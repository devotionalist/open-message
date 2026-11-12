#!/usr/bin/env bash
# Run OM+OL unit/chaos + K8s accept/fault under a meta profile: oxia | openkv
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OL_ROOT="$(cd "$ROOT/../open-ledger" && pwd)"
PROFILE="${1:?usage: $0 oxia|openkv}"
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT_DIR="${META_PROFILE_OUT:-$ROOT/tmp/ops-records}/meta-profile-${PROFILE}-${STAMP}"
mkdir -p "$OUT_DIR"
NS_OM=open-message
NS_OL=open-ledger

log() { echo "==> $*"; echo "==> $*" >>"$OUT_DIR/run.log"; }

apply_ol_oxia() {
  log "OL coord → Oxia (base kustomize)"
  kubectl apply -k "$OL_ROOT/deploy/k8s" >/dev/null
  kubectl -n "$NS_OL" set env sts/bookie -c autorecovery COORD_BACKEND- OPENKV_ADDR- || true
  kubectl -n "$NS_OL" set env deploy/openledger-admin COORD_BACKEND- OPENKV_ADDR- || true
  kubectl -n "$NS_OL" rollout restart sts/bookie deploy/openledger-admin
  kubectl -n "$NS_OL" rollout status sts/bookie --timeout=600s
  kubectl -n "$NS_OL" rollout status deploy/openledger-admin --timeout=180s
}

apply_ol_openkv() {
  log "OL coord → OpenKV overlay"
  kubectl apply -k "$OL_ROOT/deploy/overlays/openkv" >/dev/null
  kubectl -n "$NS_OL" rollout restart sts/bookie deploy/openledger-admin
  kubectl -n "$NS_OL" rollout status sts/bookie --timeout=600s
  kubectl -n "$NS_OL" rollout status deploy/openledger-admin --timeout=180s
}

apply_om_oxia() {
  log "OM Meta → Oxia"
  kubectl -n "$NS_OM" set env sts/broker \
    METADATA_BACKEND=oxia \
    OXIA_ADDR=oxia-0.oxia.open-ledger.svc.cluster.local:6648 \
    OXIA_PREFIX=/om/ \
    OPENKV_GATEWAY- \
    OPENKV_PREFIX-
  kubectl -n "$NS_OM" rollout status sts/broker --timeout=180s
}

apply_om_openkv() {
  log "OM Meta → OpenKV"
  kubectl -n "$NS_OM" set env sts/broker \
    METADATA_BACKEND=openkv \
    OPENKV_GATEWAY=gateway.openkv.svc.cluster.local:19000 \
    OPENKV_PREFIX=/om/ \
    OXIA_ADDR- \
    OXIA_PREFIX-
  kubectl -n "$NS_OM" rollout status sts/broker --timeout=180s
}

verify_profile() {
  log "verify profile=$PROFILE"
  {
    echo "### OM broker env"
    kubectl -n "$NS_OM" get sts broker -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' | rg 'META|OXIA|OPENKV|OL_META|BOOKIE' || true
    echo "### OL autorecovery args"
    kubectl -n "$NS_OL" get sts bookie -o jsonpath='{.spec.template.spec.containers[?(@.name=="autorecovery")].args}'
    echo
    echo "### broker-0 log head"
    kubectl -n "$NS_OM" logs broker-0 --tail=5
  } | tee "$OUT_DIR/topology.txt"
}

run_unit() {
  log "go test internal + chaos + opsdrill + stress"
  set +e
  (
    cd "$ROOT"
    GOTOOLCHAIN=auto go test ./internal/... ./test/chaos/... ./test/opsdrill/... ./test/stress/... -count=1
  ) >"$OUT_DIR/unit.txt" 2>&1
  local rc=$?
  set -e
  echo "unit_exit=$rc" | tee -a "$OUT_DIR/summary.txt"
  return $rc
}

run_accept() {
  log "k8s-customer-accept"
  set +e
  (
    cd "$ROOT"
    ./scripts/k8s-customer-accept.sh
  ) >"$OUT_DIR/accept.txt" 2>&1
  local rc=$?
  set -e
  echo "accept_exit=$rc" | tee -a "$OUT_DIR/summary.txt"
  return $rc
}

run_fault() {
  log "k8s-fault-drill (may take ~2–3m)"
  set +e
  (
    cd "$ROOT"
    # Preserve OM Meta backend across fault-drill's kubectl apply -k (yaml defaults to oxia).
    OM_META_PROFILE="$PROFILE" DRILL_DURATION=10m ./scripts/k8s-fault-drill.sh
  ) >"$OUT_DIR/fault.txt" 2>&1
  local rc=$?
  set -e
  echo "fault_exit=$rc" | tee -a "$OUT_DIR/summary.txt"
  if [[ "$PROFILE" == "oxia" ]]; then apply_om_oxia; else apply_om_openkv; fi
  return $rc
}

echo "profile=$PROFILE stamp=$STAMP" >"$OUT_DIR/summary.txt"
echo "note=OM LedgerMeta still OL_META_URL=metakv (HTTP); compared OM business Meta + OL coord backend" >>"$OUT_DIR/summary.txt"

case "$PROFILE" in
  oxia)
    apply_ol_oxia
    apply_om_oxia
    ;;
  openkv)
    apply_ol_openkv
    apply_om_openkv
    ;;
  *)
    echo "unknown profile $PROFILE" >&2
    exit 2
    ;;
esac

verify_profile

UNIT_RC=0
ACCEPT_RC=0
FAULT_RC=0
run_unit || UNIT_RC=$?
run_accept || ACCEPT_RC=$?
run_fault || FAULT_RC=$?

{
  echo "UNIT_RC=$UNIT_RC"
  echo "ACCEPT_RC=$ACCEPT_RC"
  echo "FAULT_RC=$FAULT_RC"
  if [[ $UNIT_RC -eq 0 && $ACCEPT_RC -eq 0 && $FAULT_RC -eq 0 ]]; then
    echo "OVERALL=PASS"
  else
    echo "OVERALL=FAIL"
  fi
} | tee -a "$OUT_DIR/summary.txt"

log "done → $OUT_DIR"
echo "$OUT_DIR"
exit 0
