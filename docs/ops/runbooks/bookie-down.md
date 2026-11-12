# Runbook：Bookie 故障

规格：[../../design/32-operations.md](../../design/32-operations.md) §8.2。设计：[03](../../design/03-autorecovery.md)、[11](../../design/11-ledger-recovery-deep.md)。

## 1. 症状

Bookie 宕机/磁盘坏；`ol_underreplicated_ledgers` > 0；写超时。

## 2. 影响面

未满足 Qa 的写失败；已写数据需 AR。

## 3. 立即隔离

- 摘故障 Bookie 出 Ensemble 候选（若未自动）。  
- **Qa 不足时禁止继续硬写**（让客户端重试/换 ledger）。

## 4. 步骤

1. 确认节点与盘。  
2. 触发/等待 AutoRecovery。  
3. 观察 underreplicated → 0。  
4. 长期下线则走缩容 Bookie runbook（护栏）。

## 5. 验证

- underreplicated=0；Produce P99 恢复；无丢已确认。

## 6. 事后

- 盘/机替换；容量与隔离组复盘。
