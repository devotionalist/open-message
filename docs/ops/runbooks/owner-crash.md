# Runbook：Owner 崩溃（冷切）

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2。设计：[15](../../design/15-availability-failover-window.md)、[09](../../design/09-broker-lifecycle.md)、[11](../../design/11-ledger-recovery-deep.md)。演练：O7。

## 1. 症状

Owner 进程没了；租约过期；客户端 NotOwner/超时。

## 2. 影响面

该 bundle 写停顿至新主；可能无 Standby 则冷切。

## 3. 立即隔离

- 确认不是网络闪断（避免误杀双主）。  
- 观察 lease / epoch。

## 4. 步骤

1. 等待租约失效或 Admin 强制 Unload（按策略）。  
2. 新 Broker 抢主 + Recover（OL 权威）。  
3. 若有 Standby 优先 Promote 路径。  
4. 对账 Index 与 open ledger。

## 5. 验证

- 新 owner；已确认消息可读；无双写。

## 6. 事后

- failover 时长是否超 SLO；是否需加 Standby。
