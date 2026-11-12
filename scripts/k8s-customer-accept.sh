#!/usr/bin/env bash
# Customer acceptance against local k8s Proxy (docs/user + SDK example).
set -euo pipefail

NS=open-message
TOPIC="${OM_TOPIC:-public/default/accept-$(date +%s)}"
kubectl -n "$NS" delete pod om-accept --ignore-not-found >/dev/null 2>&1 || true

echo "==> run produce-consume example via Proxy (topic=$TOPIC)"
kubectl -n "$NS" run om-accept --rm -i --restart=Never \
  --image=open-message:local --image-pull-policy=Never \
  --env=OM_SERVICE_URL=proxy.open-message.svc.cluster.local:6650 \
  --env=OM_TOPIC="$TOPIC" \
  --env=OM_AUTH_TOKEN=dev-token \
  --command -- /usr/local/bin/omexample

echo "customer-acceptance: PASS"
