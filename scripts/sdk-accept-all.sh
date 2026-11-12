#!/usr/bin/env bash
# Run Go/Java/Rust/TypeScript SDK produce→consume→ack against broker-0.
#
# Hosts cannot follow Proxy→Owner redirects to in-cluster DNS (broker-*.svc),
# so this script port-forwards broker-0 and dials it directly. Cluster-side
# Proxy acceptance remains: ./scripts/k8s-customer-accept.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WS="$(cd "$ROOT/.." && pwd)"
NS=open-message
LOCAL_PORT="${OM_LOCAL_PORT:-16650}"
TOKEN="${OM_AUTH_TOKEN:-dev-token}"

cleanup() {
  if [[ -n "${PF_PID:-}" ]]; then
    kill "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# Drop stale listeners on the local port.
if command -v lsof >/dev/null 2>&1; then
  lsof -tiTCP:"$LOCAL_PORT" -sTCP:LISTEN | xargs kill 2>/dev/null || true
fi
pkill -f "port-forward.*${LOCAL_PORT}:6650" 2>/dev/null || true
sleep 1

echo "==> port-forward broker-0 -> 127.0.0.1:${LOCAL_PORT}"
kubectl -n "$NS" port-forward pod/broker-0 "${LOCAL_PORT}:6650" >/tmp/om-sdk-pf.log 2>&1 &
PF_PID=$!
for i in 1 2 3 4 5 6 7 8 9 10; do
  if nc -z 127.0.0.1 "$LOCAL_PORT" 2>/dev/null; then
    break
  fi
  sleep 0.5
done
if ! nc -z 127.0.0.1 "$LOCAL_PORT" 2>/dev/null; then
  echo "port-forward failed:" >&2
  cat /tmp/om-sdk-pf.log >&2 || true
  exit 1
fi

export OM_SERVICE_URL="127.0.0.1:${LOCAL_PORT}"
export OM_AUTH_TOKEN="$TOKEN"
RUN_ID="${OM_RUN_ID:-$(date +%s)}"

run_one() {
  local lang="$1"
  local topic="public/default/accept-${lang}-${RUN_ID}"
  shift
  echo "==> ${lang} accept (topic=${topic})"
  OM_TOPIC="$topic" OM_SUB="sub-${lang}-${RUN_ID}" "$@"
  echo "ok ${lang}"
}

run_one go bash -c "
  cd '$WS/open-message-client-go'
  GOTOOLCHAIN=auto go run ./examples/produce-consume
"

run_one java bash -c "
  cd '$WS/open-message-client-java'
  mkdir -p target/classes
  javac -d target/classes src/main/java/io/openmessage/client/*.java \
    src/main/java/io/openmessage/client/examples/*.java
  java -cp target/classes io.openmessage.client.examples.ProduceConsume
"

run_one rust bash -c "
  cd '$WS/open-message-client-rust'
  cargo run --example produce_consume --quiet
"

run_one typescript bash -c "
  cd '$WS/open-message-client-typescript'
  npx --yes tsx examples/produce-consume.mts
"

echo "sdk-accept-all: PASS"
