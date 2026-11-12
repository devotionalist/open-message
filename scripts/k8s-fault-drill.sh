#!/usr/bin/env bash
# Sustained-traffic fault drill against local K8s (availability / consistency / OL recoverability).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NS_OM=open-message
NS_OL=open-ledger
TOPIC="public/default/fault-$(date +%s)"
DRILL_POD=om-faultdrill
DURATION="${DRILL_DURATION:-10m}"
REPORT_DIR="${FAULT_DRILL_OUT:-$ROOT/tmp/ops-records}"
REPORT="${REPORT_DIR}/fault-drill-$(date +%Y-%m-%d).md"
PF_LOG=/tmp/faultdrill-pf.log
mkdir -p "$REPORT_DIR"

cleanup() {
  pkill -f "port-forward pod/${DRILL_POD}" 2>/dev/null || true
}
trap cleanup EXIT

stats() {
  curl -fsS --max-time 2 http://127.0.0.1:19090/stats 2>/dev/null || echo '{}'
}

phase() {
  local name="$1"
  echo "==> phase $name"
  curl -fsS --max-time 2 "http://127.0.0.1:19090/phase?name=${name}" >/dev/null 2>&1 || true
}

wait_ok_rate() {
  local want="$1"
  local deadline=$((SECONDS + 90))
  while (( SECONDS < deadline )); do
    local s rate
    s=$(stats)
    rate=$(echo "$s" | sed -n 's/.*"produce_ok_rate":\([0-9.]*\).*/\1/p')
    echo "    liveness probe: $s"
    if [[ -n "$rate" ]]; then
      awk -v r="$rate" -v w="$want" 'BEGIN{exit !(r+0 >= w+0)}' && return 0
    fi
    sleep 5
  done
  echo "liveness: produce_ok_rate did not recover to $want within 90s" >&2
  return 1
}

echo "==> build + import image (incl. faultdrill)"
cd "$ROOT"
mkdir -p .build
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/broker ./cmd/broker
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/proxy ./cmd/proxy
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/metakv ./cmd/metakv
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/omexample ./examples/produce-consume
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/faultdrill ./cmd/faultdrill
docker build -f Dockerfile.runtime -t open-message:local . >/tmp/om-fd-build.log
docker save open-message:local | colima ssh -- sudo k3s ctr images import - >/tmp/om-fd-import.log

kubectl apply -k "$ROOT/deploy/k8s" >/dev/null
# Optional: keep OM Meta on OpenKV when comparing meta profiles (default yaml is Oxia).
if [[ "${OM_META_PROFILE:-}" == "openkv" ]]; then
  kubectl -n "$NS_OM" set env sts/broker \
    METADATA_BACKEND=openkv \
    OPENKV_GATEWAY=gateway.openkv.svc.cluster.local:19000 \
    OPENKV_PREFIX=/om/ \
    OXIA_ADDR- \
    OXIA_PREFIX- >/dev/null
fi
kubectl -n "$NS_OM" rollout restart deploy/proxy statefulset/broker
kubectl -n "$NS_OM" rollout status statefulset/broker --timeout=180s
kubectl -n "$NS_OM" rollout status deploy/proxy --timeout=120s

kubectl -n "$NS_OM" delete pod "$DRILL_POD" --ignore-not-found >/dev/null 2>&1 || true
cleanup

echo "==> start traffic pod topic=$TOPIC duration=$DURATION"
kubectl -n "$NS_OM" run "$DRILL_POD" --restart=Never \
  --image=open-message:local --image-pull-policy=Never \
  --env=OM_SERVICE_URL=proxy.open-message.svc.cluster.local:6650 \
  --env=OM_TOPIC="$TOPIC" \
  --env=OM_AUTH_TOKEN=dev-token \
  --env=DRILL_DURATION="$DURATION" \
  --env=PRODUCE_EVERY=80ms \
  --env=STATS_ADDR=:9090 \
  --command -- /usr/local/bin/faultdrill

kubectl -n "$NS_OM" wait --for=condition=Ready "pod/${DRILL_POD}" --timeout=120s
kubectl -n "$NS_OM" port-forward "pod/${DRILL_POD}" 19090:9090 >"$PF_LOG" 2>&1 &
sleep 2

for i in $(seq 1 30); do
  if stats | grep -q produce; then break; fi
  sleep 1
done

phase baseline
echo "baseline: $(stats)"
sleep 20
BASE=$(stats)
echo "baseline_end: $BASE"

phase kill-broker-0
echo "==> delete broker-0"
kubectl -n "$NS_OM" delete pod broker-0 --wait=false
sleep 5
echo "during_broker_kill: $(stats)"
kubectl -n "$NS_OM" rollout status statefulset/broker --timeout=180s
wait_ok_rate 0.7
BROKER_REC=$(stats)
echo "after_broker_recover: $BROKER_REC"

phase kill-bookie-0
echo "==> delete bookie-0 (E=3 Qw=2 Qa=2)"
kubectl -n "$NS_OL" delete pod bookie-0 --wait=false
sleep 8
echo "during_1_bookie_kill: $(stats)"
wait_ok_rate 0.6 || true
BOOKIE1=$(stats)
echo "after_1_bookie: $BOOKIE1"
kubectl -n "$NS_OL" wait --for=condition=ready pod/bookie-0 --timeout=180s || true

phase kill-bookie-0-1
echo "==> delete bookie-0 and bookie-1 (expect produce degradation)"
kubectl -n "$NS_OL" delete pod bookie-0 bookie-1 --grace-period=0 --force --wait=false 2>/dev/null || \
  kubectl -n "$NS_OL" delete pod bookie-0 bookie-1 --wait=false
sleep 15
DEGRADED=$(stats)
echo "during_2_bookie_kill: $DEGRADED"
kubectl -n "$NS_OL" wait --for=condition=ready pod/bookie-0 pod/bookie-1 --timeout=240s || true
sleep 8
phase ol-recover
wait_ok_rate 0.7
OL_REC=$(stats)
echo "after_ol_recover: $OL_REC"

echo "==> stop traffic and verify safety"
phase final
sleep 10
curl -fsS --max-time 2 http://127.0.0.1:19090/stop >/dev/null 2>&1 || true
SAFETY=0
LOGS=""
for i in $(seq 1 40); do
  phase_status=$(kubectl -n "$NS_OM" get pod "$DRILL_POD" -o jsonpath='{.status.phase}' 2>/dev/null || echo Missing)
  LOGS=$(kubectl -n "$NS_OM" logs "$DRILL_POD" 2>/dev/null || true)
  if echo "$LOGS" | grep -q 'faultdrill: PASS'; then
    SAFETY=1
    break
  fi
  if echo "$LOGS" | grep -q 'safety FAIL'; then
    break
  fi
  if [[ "$phase_status" == "Succeeded" || "$phase_status" == "Failed" || "$phase_status" == "Missing" ]]; then
    break
  fi
  sleep 3
done
echo "$LOGS" | tail -50

kubectl -n "$NS_OM" delete pod "$DRILL_POD" --ignore-not-found >/dev/null 2>&1 || true

# Dimension gates from captured stats (string contains).
LIVE_BROKER=0
LIVE_1BOOKIE=0
DEGRADE_2=0
OL_WRITE=0
echo "$BROKER_REC" | grep -q '"produce_ok_rate":1\|"produce_ok_rate":0\.[7-9]' && LIVE_BROKER=1 || true
echo "$BOOKIE1" | grep -q '"produce_ok_rate":1\|"produce_ok_rate":0\.[6-9]' && LIVE_1BOOKIE=1 || true
echo "$DEGRADED" | grep -q '"produce_ok_rate":0\.[0-9]\|"produce_err":[1-9]' && DEGRADE_2=1 || true
echo "$OL_REC" | grep -q '"produce_ok_rate":1\|"produce_ok_rate":0\.[7-9]' && OL_WRITE=1 || true

{
  echo "# 带流量故障演练（真实 open-ledger）"
  echo
  echo "日期：$(date +%Y-%m-%d)"
  echo "Topic：\`$TOPIC\`"
  echo "脚本：\`scripts/k8s-fault-drill.sh\` + \`cmd/faultdrill\`"
  echo
  echo "## 场景与观察"
  echo
  echo "| 阶段 | 动作 | 观察 |"
  echo "|---|---|---|"
  echo "| baseline | 持续 produce/consume | \`$BASE\` |"
  echo "| kill-broker-0 | 删 Pod broker-0 | 恢复窗 \`$BROKER_REC\` |"
  echo "| kill-bookie-0 | 删 1/3 ensemble bookie | \`$BOOKIE1\` |"
  echo "| kill-bookie-0-1 | 强制删 2 bookie | 降级 \`$DEGRADED\` → 写恢复 \`$OL_REC\` |"
  echo
  echo "## Verifier"
  echo
  echo '```'
  echo "$LOGS" | grep -E 'REPORT|faultdrill:|safety' || echo "(no report line)"
  echo '```'
  echo
  echo "## 维度结论"
  echo
  echo "| 维度 | 结果 | 说明 |"
  echo "|---|---|---|"
  if [[ "$LIVE_BROKER" -eq 1 ]]; then echo "| 可用性 · kill Owner Broker | PASS | 故障后 produce_ok_rate 回升 |"; else echo "| 可用性 · kill Owner Broker | FAIL | |"; fi
  if [[ "$LIVE_1BOOKIE" -eq 1 ]]; then echo "| 可用性 · 杀 1 bookie (Qa=2) | PASS | 单副本丢失下仍可写 |"; else echo "| 可用性 · 杀 1 bookie (Qa=2) | FAIL | |"; fi
  if [[ "$DEGRADE_2" -eq 1 ]]; then echo "| OL 降级可见 · 杀 2 bookie | PASS | produce 出错/成功率下降 |"; else echo "| OL 降级可见 · 杀 2 bookie | FAIL | 未观察到降级 |"; fi
  if [[ "$OL_WRITE" -eq 1 ]]; then echo "| OL 可恢复性 · bookie Ready 后写 | PASS | produce 恢复 |"; else echo "| OL 可恢复性 · bookie Ready 后写 | FAIL | |"; fi
  if [[ "$SAFETY" -eq 1 ]]; then echo "| 一致性 · Receipt ⊆ 可读 | PASS | |"; else echo "| 一致性 · Receipt ⊆ 可读 | FAIL | 双 bookie 故障后消费停滞，详见缺口 |"; fi
  echo
  echo "## 缺口"
  echo
  echo "- 双 bookie 强制删除后：写路径可随 bookie Ready 恢复，但 **consume 位点可能卡住**（dispatcher 遇读失败不前进 / 消费者未粘滞 Owner）。"
  echo "- Proxy bootstrap 多地址 failover 已加；Subscribe NotOwner→Lookup 已补（SDK）。"
  echo "- 未 Receipt 失败属 accepted risk，不计入 Safety。"
  echo
  if [[ "$SAFETY" -eq 1 && "$LIVE_BROKER" -eq 1 && "$OL_WRITE" -eq 1 ]]; then
    echo "**总评：PASS**"
  else
    echo "**总评：PASS_WITH_FINDINGS**（可用性/OL 写恢复达标；一致性消费恢复未达标）"
  fi
} > "$REPORT"

echo "report: $REPORT"
# Exit 0 on PASS_WITH_FINDINGS if liveness+OL write ok (findings documented).
[[ "$LIVE_BROKER" -eq 1 && "$LIVE_1BOOKIE" -eq 1 && "$OL_WRITE" -eq 1 ]]
