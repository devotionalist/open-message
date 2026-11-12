# Runbook：热备升主（Promote）

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2。设计：[16](../../design/16-warm-standby.md)、[15](../../design/15-availability-failover-window.md)、[06](../../design/06-fencing-consistency.md)。

## 1. 症状

主故障或主动 Promote；failover 延迟告警。

## 2. 影响面

短暂不可写；**Promote ≠ 等满 TTL**。

## 3. 立即隔离

- 确认旧主已不可服务或已 Fence/Unload。  
- 勿并行冷切与 Promote 同 bundle。

## 4. 步骤

1. 确认 Standby 健康与 CursorStore 状态。  
2. 触发 Promote（自动或 Admin）。  
3. 观察新 epoch / 可写。  
4. 旧主禁止带旧 epoch 写回。

## 5. 验证

- Produce/Consume 恢复；failover 直方图落入 SLO。

## 6. 事后

- 记录是否触发自动 Promote；对照演练 O7。
