# Runbook：OM Meta 故障

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2；双 Meta：[27](../../design/27-dual-meta-ops.md)。

## 1. 症状

Lookup/租约/游标 CAS 失败；`om_meta_*` 探活失败；大量 Unavailable。

## 2. 影响面

新派发/切主/确认游标可能停；已持有 owner 的热路径可能短暂续命（视实现）。

## 3. 立即隔离

- 停止会加重 Meta 写的运维动作（大规模 Unload、扩缩）。  
- 勿同时动 OL Meta。

## 4. 步骤

1. 确认是客户端超时还是 Meta 真宕（探活/日志）。  
2. 按 Meta 产品手册恢复（Oxia/OpenKV）。  
3. Broker 侧观察租约恢复与 ErrRetry 消退。  
4. 必要时触发受控 Unload 清理半开状态（仅在 Meta 已稳）。

## 5. 验证

- Lookup/Produce 恢复；lease 续约成功；无野主。

## 6. 事后

- 根因；是否触及备份恢复阈值。
