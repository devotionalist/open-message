# Runbook：Trim / Offload

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2。设计：[01](../../design/01-ledger.md) Trim、[24](../../design/24-offload.md)。

## 1. 症状

盘满、保留策略到期、冷数据待卸。

## 2. 影响面

Trim 过猛导致读不到历史；Offload 失败占盘。

## 3. 立即隔离

- 盘紧急时：**先扩盘/加 Bookie**，再 Trim。  
- Offload 目标不可用则暂停任务。

## 4. 步骤

1. 确认 Cursor/保留水位允许 Trim。  
2. 执行 Trim（Admin/自动）。  
3. Offload：写 OffloadLocation → 校验可读 → 再删热副本（按设计顺序）。  
4. 观察盘与读延迟。

## 5. 验证

- 订阅可读性；Offload 对象存在且 Meta 一致。

## 6. 事后

- 策略是否过激；容量规划。
