# Runbook：Broker Drain / Unload（可照做）

规格：[../../design/32-operations.md](../../design/32-operations.md)。设计：[09](../../design/09-broker-lifecycle.md)、[25](../../design/25-admin-api.md)。

**级别**：生产级（Admin 已落地）。

## 1. 前置

```bash
NS=open-message
TOKEN=dev-token   # 或集群真实 token
BROKER=broker-0   # StatefulSet pod
kubectl -n $NS get pods -l app=broker
```

## 2. 步骤

```bash
# Drain（拒绝新 Acquire，Unload 全部 bundle）
kubectl -n $NS exec deploy/proxy -- true  # 确认入口仍在
kubectl -n $NS exec $BROKER -- wget -qO- \
  --header="Authorization: Bearer $TOKEN" \
  --post-data='' http://127.0.0.1:8080/admin/v1/brokers/drain

# 或单 bundle Unload
kubectl -n $NS exec $BROKER -- wget -qO- \
  --header="Authorization: Bearer $TOKEN" \
  --post-data='' \
  http://127.0.0.1:8080/admin/v1/namespaces/default/bundles/00000000-ffffffff/unload

# 观察
kubectl -n $NS exec broker-1 -- wget -qO- \
  --header="Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/admin/v1/stats
```

## 3. 验证

- 新 Owner `stats.owned` 上升；旧节点 `status=draining` 或 owned=0。  
- 客户端短暂 `NotOwner` 后重 Lookup 可恢复（SDK 默认可重试）。  
- `/metrics` 中 `failover` 可观测。

## 4. 回滚

对已 Drain 节点：重启 Pod 会重新 Register；勿在 Meta 不可用时批量 Unload。
