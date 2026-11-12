# 订阅与 Cursor 细章

主文档：§15。通则：[00-principles.md](./00-principles.md)。  
**分发模式 / Dispatcher**（Exclusive·Failover·Shared·Key_Shared）：[18-dispatch-modes.md](./18-dispatch-modes.md)。

## 1. 为何合理

- Cursor 外置 → Broker 挂了消费位点不丢（自愈基础）。  
- M1 只做 Exclusive + Cumulative → 状态机简单、易证正确。  
- 预留 `individualAcks` → Shared 时不必改存储模型。  
- 刷盘与消息 QPS 解耦 → Meta 可扩容、不被 ack 打爆。

## 2. 数据模型

```text
Subscription {
  topic, name, type: Exclusive|Failover|Shared|KeyShared,
  createdAt
}

Cursor {
  topic, subscription,
  markDelete: Position{ledgerId, entryId},  // 前缀已确认
  individualAcks: AckSet,                  // M1 空；稀疏位图/区间
  version,
  lastPersistAt
}

Position 全序: (ledgerId, entryId) 按 LedgerIndex 链序 + entryId
```

AckSet 演进（M3）：按 **user ledger** 分条持久化（防单 value 膨胀），定期把连续前缀合并进 markDelete。

## 3. 正常路径

### 3.1 Exclusive + Cumulative（M1）

1. Subscribe → 若无 cursor，初始 `markDelete = latest LAC`（或 earliest）。  
2. Dispatcher 从 markDelete 下一位置读。  
3. AckCumulative(pos)：CAS 仅允许位置前移。  
4. 持久化可合并：内存先推进，周期/阈值刷 Meta。

### 3.2 Failover（M2）

- 多 Consumer 挂同一 sub；Broker 选 active（连接序/优先级）。  
- Active 挂 → 另一实例接管；cursor 仍是一份。

### 3.3 Shared / Key_Shared（M3）

- 多消费者分发；individual ack；同 key 有序（Key_Shared）。  
- 生产端 Key_Shared 必须 key-based batch 或关 batch。  
- 选路、重投、key 迁移封锁见 [18-dispatch-modes.md](./18-dispatch-modes.md)。

## 4. 故障与自愈

| 故障 | 自愈 |
|---|---|
| Broker 崩溃 | 新 owner 读 CursorStore；从 markDelete 后至少一次投递 |
| 刷盘前崩溃 | 最多重复已「内存 ack 未刷盘」区间 → at-least-once |
| CAS 冲突 | 重读；取较大位点（cumulative 单调） |
| Consumer 挂 | Exclusive：等待重连；Failover：切 active；未 ack 重投 |
| Cursor 空洞膨胀（M3） | 按 ledger 拆分；超阈值拒绝 individual / 触发告警 |
| 全订阅越过 ledger | Trim 任务 Delete CLOSED ledger + 更新 Index |

**禁止**：仅内存 cursor 无持久 → 无法自愈。

## 5. 扩容

- Cursor 按 `topic` 分片；加 Meta shard → 不同 topic 的 ack 并发 ↑。  
- 同 topic 多 sub 同片：便于 trim 扫描该 topic 全部 cursor。  
- 分区 topic：每 partition 独立 cursor 命名空间 → 消费并行随分区数升。

## 6. 与积压 / 删除

- Backlog ≈ 从各 cursor 到 LAC 的最大差距。  
- 删除 ledger 条件：所有 cursor.markDelete 越过 **且** retention 到期。  
- TTL：到期消息可强制推进逻辑确认（实现为 dispatcher 跳过或写跳过标记，细则 M2）。

## 7. 不变量

1. markDelete 单调不回退。  
2. 投递位点 ≥ markDelete 之后。  
3. 持久 cursor 丢失窗口只导致重复，不导致「已确认被删后又对别的 sub 不可见」以外的跨订阅异常。  
4. **Shared（M3）**：`CasAdvance` 的 markDelete **不得跳过**仍存在于 `individualAcks` 的空洞；Meta 持久化须 **原子**包含 `{markDelete, individualAcks}`。Unload **必须**刷整份 CursorValue。违反则 trim 可永久丢未 ack 消息。  

## 8. 阶段

- M1：Exclusive + Cumulative + 合并刷盘。  
- M2：Failover + ack timeout。  
- M3：Shared/Key_Shared + individual + 按 ledger 拆 ack 存储。
