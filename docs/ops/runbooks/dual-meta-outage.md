# Runbook：双 Meta 同时故障

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2；[27](../../design/27-dual-meta-ops.md)。演练：O1 / O13。

## 1. 症状

OM+OL Meta 同时不可用或脑裂。

## 2. 影响面

控制面与写路径大面积不可用；**最坏场景**。

## 3. 立即隔离

- **只读维护模式**（若有）：停 Produce/扩缩/GC。  
- 禁止手动改一边 Meta 导致对账更糟。  
- 明确恢复顺序：**先稳 Meta 本身，再开流量**。

## 4. 步骤

1. 判定哪一侧先可恢复（或并行但团队拆分）。  
2. 恢复 Meta 服务可用性。  
3. 跑双 Meta 对账（设计 13/22）：缺 LedgerMeta、野 Index、Orphan。  
4. 按对账结果：补 Meta / MarkOrphan / 人工审阅。  
5. 逐步放开 Lookup → Produce → Consume。

## 5. 验证

- 对账告警清；E2E 冒烟；无野主。

## 6. 事后

- 必做：备份策略复盘；演练 O1 记录。
