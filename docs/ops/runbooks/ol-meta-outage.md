# Runbook：OL Meta 故障

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2；双 Meta：[27](../../design/27-dual-meta-ops.md)。

## 1. 症状

Create/Fence/Recover/AR 失败；`ol_meta_*` 探活失败；写入 Unavailable。

## 2. 影响面

新 ledger / Recover / 副本修复可能停；读已 seal 数据视路径仍可用。

## 3. 立即隔离

- 暂停 Offload/Orphan 大规模作业。  
- 勿同时动 OM Meta。

## 4. 步骤

1. 确认 OL Meta 探活与 openledgerd 错误码。  
2. 按 Meta 产品手册恢复。  
3. 确认 underreplicated 与 Recover 队列消化。  
4. 与 OM Index 对账（`index_ledger_missing_ol`）。

## 5. 验证

- Produce 恢复；无新增耐久告警；对账干净。

## 6. 事后

- 根因；是否需 MarkOrphan 人工介入。
