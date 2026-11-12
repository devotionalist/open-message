#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "==> cross-compile linux/amd64"
mkdir -p .build
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/broker ./cmd/broker
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/proxy ./cmd/proxy
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/metakv ./cmd/metakv
GOTOOLCHAIN=auto GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .build/omexample ./examples/produce-consume

echo "==> docker image open-message:local"
docker build -f Dockerfile.runtime -t open-message:local .
echo "==> import into k3s (colima)"
docker save open-message:local | colima ssh -- sudo k3s ctr images import -

echo "==> apply manifests"
kubectl apply -k "$ROOT/deploy/k8s"
kubectl -n open-message rollout status deploy/metakv --timeout=120s
kubectl -n open-message rollout status statefulset/broker --timeout=180s
kubectl -n open-message rollout status deploy/proxy --timeout=120s
kubectl -n open-message get pods -o wide
echo "ready"
