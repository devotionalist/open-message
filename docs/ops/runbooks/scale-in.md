# Runbook：缩容 Broker（可照做）

## 步骤

1. 查隔离护栏：
```bash
TOKEN=dev-token
kubectl -n open-message exec broker-0 -- wget -qO- \
  --header="Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8080/admin/v1/brokers/scale-in-check?ns=default&after=1"
# allowed=false 则禁止缩到 1（当 min_brokers≥2）
```
2. 对目标节点 `POST /admin/v1/brokers/drain`。  
3. 确认 bundle 已在其他节点 `stats`。  
4. `kubectl -n open-message scale statefulset/broker --replicas=N`（N≥min）。  
5. 验证无野主、流量恢复。
