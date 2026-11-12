# Runbook：Orphan GC（可照做）

## 原则

- **禁止**抽样 Index 导出后直接删盘。  
- 主路径：`MarkOrphan` → 全量对账 → OL GC。

## 步骤

```bash
TOKEN=dev-token
# 对疑似未挂链 ledger 候选做对账标记
kubectl -n open-message exec broker-0 -- wget -qO- \
  --header="Authorization: Bearer $TOKEN" \
  --header="Content-Type: application/json" \
  --post-data='{"candidates":[999001,999002]}' \
  http://127.0.0.1:8080/admin/v1/topics/public/default/demo/orphans/reconcile
```

验证：响应 `marked`；`/metrics` 中 `orphan_marked` 增加；挂链 ledger 不会出现在 orphans。
