#!/usr/bin/env bash
# Port-forward Oxia + OpenKV, run metabench both backends, write comparison record.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT_DIR="${META_BENCH_OUT:-$ROOT/tmp/ops-records}/meta-bench-${STAMP}"
mkdir -p "$OUT_DIR"
OXIA_LOCAL=16648
# Gateway exposes 19000=KV gRPC, 19001=admin — always forward KV.
OKV_LOCAL=19000
OKV_REMOTE=19000
# Keep serial ops modest: OpenKV p50 Get can be tens of ms on local k3s.
OPS="${OPS:-300}"
DURATION="${DURATION:-5s}"
CONC="${CONC:-1,8,32}"

cleanup() {
  pkill -f "port-forward.*oxia-0.*${OXIA_LOCAL}" 2>/dev/null || true
  pkill -f "port-forward.*svc/oxia.*${OXIA_LOCAL}" 2>/dev/null || true
  pkill -f "port-forward.*svc/gateway.*${OKV_LOCAL}" 2>/dev/null || true
  pkill -f "port-forward.*gateway.*${OKV_LOCAL}" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> build metabench"
cd "$ROOT"
GOTOOLCHAIN=auto go build -o .build/metabench ./cmd/metabench

echo "==> port-forward Oxia :${OXIA_LOCAL} OpenKV :${OKV_LOCAL}->${OKV_REMOTE}"
cleanup
sleep 1
# Oxia Service is headless — forward the pod directly.
kubectl -n open-ledger port-forward pod/oxia-0 "${OXIA_LOCAL}:6648" >/tmp/mb-oxia-pf.log 2>&1 &
kubectl -n openkv port-forward svc/gateway "${OKV_LOCAL}:${OKV_REMOTE}" >/tmp/mb-okv-pf.log 2>&1 &
for i in 1 2 3 4 5 6 7 8 9 10; do
  if nc -z 127.0.0.1 "$OXIA_LOCAL" && nc -z 127.0.0.1 "$OKV_LOCAL"; then
    break
  fi
  sleep 0.5
done
nc -z 127.0.0.1 "$OXIA_LOCAL" || { echo "oxia pf failed"; cat /tmp/mb-oxia-pf.log; exit 1; }
nc -z 127.0.0.1 "$OKV_LOCAL" || { echo "openkv pf failed"; cat /tmp/mb-okv-pf.log; exit 1; }

echo "==> oxia"
./.build/metabench -backend oxia -oxia "127.0.0.1:${OXIA_LOCAL}" \
  -prefix "/om-bench/${STAMP}/oxia/" \
  -ops "$OPS" -duration "$DURATION" -conc "$CONC" -json \
  | tee "$OUT_DIR/oxia.jsonl"

echo "==> openkv"
./.build/metabench -backend openkv -openkv "127.0.0.1:${OKV_LOCAL}" \
  -prefix "/om-bench/${STAMP}/openkv/" \
  -ops "$OPS" -duration "$DURATION" -conc "$CONC" -json \
  | tee "$OUT_DIR/openkv.jsonl"

python3 - <<'PY' "$OUT_DIR"
import json, sys, collections
from pathlib import Path
out = Path(sys.argv[1])
rows = {}
for backend in ("oxia", "openkv"):
    for line in (out / f"{backend}.jsonl").read_text().splitlines():
        if not line.strip():
            continue
        r = json.loads(line)
        rows[(r["scenario"], r["concurrency"], backend)] = r

scenarios = sorted({(s, c) for s, c, _ in rows})
md = []
md.append("# Meta microbench：Oxia vs OpenKV（OM/OL 访问模式）")
md.append("")
md.append(f"目录：`{out.name}`")
md.append("")
md.append("口径：走 OM `internal/metadata` 适配层（与 Broker 相同）；OL 侧用同构 Get/PutCAS/ListPrefix（bookie 心跳、ledger meta、list）。")
md.append("")
md.append("| scenario | c | Oxia ops/s | OpenKV ops/s | 比值 Oxia/OKV | Oxia p99µs | OpenKV p99µs | Oxia err | OKV err |")
md.append("|---|---:|---:|---:|---:|---:|---:|---:|---:|")
for s, c in scenarios:
    a = rows.get((s, c, "oxia"))
    b = rows.get((s, c, "openkv"))
    if not a or not b:
        continue
    ratio = (a["ops_per_sec"] / b["ops_per_sec"]) if b["ops_per_sec"] else float("inf")
    md.append(
        f"| {s} | {c} | {a['ops_per_sec']:.1f} | {b['ops_per_sec']:.1f} | **{ratio:.2f}×** | "
        f"{a['p99_us']:.0f} | {b['p99_us']:.0f} | {a['errors']} | {b['errors']} |"
    )
md.append("")
md.append("## 场景说明")
md.append("")
md.append("| scenario | 模拟 |")
md.append("|---|---|")
md.append("| get_lookup | OM Ownership Lookup `Get` |")
md.append("| om_renew | OM Ownership Renew `Get+Cas` |")
md.append("| om_cursor | OM Cursor CasAdvance `Get+Cas` |")
md.append("| om_list_own | OM LoadMgr `ListPrefix own/` |")
md.append("| ol_ledger_get | OL LedgerMeta `Get` |")
md.append("| ol_ledger_cas | OL LedgerMeta `Get+PutCAS` |")
md.append("| ol_list_bookie | OL bookie 发现 `ListPrefix` |")
md.append("| mixed_om | 并发混合：Lookup 45% / Cursor 30% / Renew 15% / List 10% |")
md.append("| mixed_ol | 并发混合：Ledger Get 40% / Ledger Cas 40% / Bookie Cas 15% / List 5% |")
md.append("")
(out / "compare.md").write_text("\n".join(md) + "\n")
print((out / "compare.md").read_text())
PY

echo "done → $OUT_DIR"
echo "$OUT_DIR"
