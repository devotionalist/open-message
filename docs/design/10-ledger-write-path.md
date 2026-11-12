# 深挖：Ledger 写路径、LAC、换 Ensemble

上游：[01-ledger.md](./01-ledger.md)。通则：[00-principles.md](./00-principles.md)。  
**执行体**：quorum / fragment CAS 均在 **open-ledger Client** 内；Broker ManagedLedger 只调 Client 并维护 LAC/Receipt 语义。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 「并行 Qw、等 Qa」若步骤不清 → 提前 Receipt 或分叉 |
| 选型 | Client 内 quorum；LAC 连续释放；失败换 fragment 写 OL Meta |
| 放弃 | Broker 自管 ensemble Meta；未达 Qa 先 Receipt |
| 代价 | 单 writer 吞吐上限；换 fragment 延迟 |
| 验收 | Receipt ⊆ Qa；LAC 连续 |

## 1. 目标

把「并行写 Qw、等 Qa」落到**可实现、可证错**的步骤，覆盖：

- entry 如何选 Write Quorum  
- LAC 如何推进、为何能读  
- 中途 bookie 失败如何换 fragment（**Client** 更新 OL LedgerMeta）  
- 哪些情况绝不能给 Client 成功 Receipt  

## 2. 配置与选副本（示例）

取 `E=5, Qw=3, Qa=2`，当前 fragment ensemble：

```text
index:  0    1    2    3    4
bookie: B0   B1   B2   B3   B4
```

Entry `i` 的 Write Quorum = 从 `i % E` 起连续 `Qw` 个（环状）：

| entryId | 起点 `i%5` | Write Quorum (Qw=3) |
|---|---|---|
| 0 | 0 | B0,B1,B2 |
| 1 | 1 | B1,B2,B3 |
| 2 | 2 | B2,B3,B4 |
| 3 | 3 | B3,B4,B0 |
| 4 | 4 | B4,B0,B1 |
| 5 | 0 | B0,B1,B2 |

Ack Quorum：Write Quorum 中任意 **2** 个 journal 成功即可对该 entry 向上确认（仍须遵守下面的 LAC 顺序约束）。

**耐久**：已确认 entry 在至少 Qa=2 个 bookie；可再丢 1 个仍可能读出（取决于读策略与是否已补副本）。

## 3. Writer 侧状态（Broker 内存，可丢）

```text
LedgerHandle {
  ledgerId, ensemble/fragments 镜像,
  nextEntryId,          // 分配给下一条
  lac,                  // 已向 Client 确认的最大 entryId
  inFlight: map[entryId]→{acks, deadline, payload},
  writerEpoch,
  fenced: bool
}
```

权威仍在 Bookie + **open-ledger LedgerMeta**；上述仅加速。Failover 后全部重建。

## 4. 单条 Append 时序

```mermaid
sequenceDiagram
  participant C as Client
  participant W as WriterBroker
  participant OL as open_ledger_Client
  participant Q as WriteQuorumBookies
  C->>W: Send(payload)
  W->>W: entryId=next++
  W->>OL: Append(ledgerId,entryId,lac,payload)
  OL->>Q: parallel AddEntry
  Q-->>OL: journal_ack x Qa
  OL-->>W: OK
  W->>W: if entryId==lac+1: lac=entryId; release contiguous
  W-->>C: SendReceipt(MessageID)
```

### 4.1 严格规则

1. **先分配 entryId，再写 bookie**（单 writer 本地单调即可）。  
2. **并行**发往 Qw，不等待串行双写。  
3. 仅当收到 **≥Qa** 个成功且 **journal 已持久** 才计入该 entry「可确认」。  
4. **LAC 连续推进**：只有 `entryId == lac+1` 且可确认时，才 `lac++`；若 entry 5 先凑齐 Qa 而 4 未齐，**先不给 Client 5 的成功**（或持有 Receipt 至空洞补齐）。推荐：**按 entryId 序释放 Receipt**，避免读者看到空洞。  
5. 发给 bookie 的 entry 内嵌当前 `lac`（写时已确认点），供 Recover 加速。

### 4.2 何时禁止 SendReceipt

- `fenced == true` 或 `writerEpoch` 失效  
- 未达 Qa  
- **OL** LedgerMeta 已非 OPEN（竞态读到 IN_RECOVERY/CLOSED）  
- 本地 bundleEpoch 续约失败  

## 5. Bookie 侧（单节点）

```text
on AddEntry:
  if ledger fenced locally -> ErrFenced
  if ledger sealed -> ErrSealed
  append to Journal (fsync/group commit)
  ack writer
  async dump to ledger storage
```

- **Ack 含义**：journal 耐久，不是「已对所有副本可见」。  
- Fence：持久化 fenced 标记；之后普通 AddEntry 失败；Recover 写修可走特权路径（或单独 API）。

## 6. 写中 Bookie 失败：换 Fragment

场景：写 entry 10，Qw={B1,B2,B3}，B2 超时。

### 6.1 决策

- 若已达 Qa（例如 B1、B3 已 ack）：**该 entry 仍可确认**；稍后 Auditor 可修 B2。  
- 若未达 Qa：该 entry **不得 Receipt**；触发 ensemble change。

### 6.2 Ensemble change 步骤（**open-ledger Client**；同 writer 会话）

1. Client 选替换 bookie B2'（活、同亲和组、尽量不在当前 ensemble）。  
2. Client **CAS OL LedgerMeta**：追加 fragment `{ firstEntryId: firstUnacked, ensemble: 新列表 }`。  
3. 对所有 **未确认** entry（含失败的 10）用**新** Write Quorum 重写（幂等：同 entryId 覆盖/拒绝冲突由协议保证同内容）。  
4. 达 Qa 后 Broker 按序推进 LAC / Receipt。  
Broker **不**直接写 LedgerMeta。

### 6.3 反例（禁止）

| 错误做法 | 后果 |
|---|---|
| 未达 Qa 就 Receipt | 丢确认数据 |
| 改写历史 fragment 的 ensemble | 旧 entry 定位混乱 |
| 换 ensemble 后 entryId 重绕从 0 | MessageID 语义崩坏 |
| 多 writer 同时换 fragment | 分叉；必须单 writer |

## 7. 批量与乱序

- Broker 可 pipeline 多条 in-flight（提高吞吐）。  
- **Receipt 仍按 entryId 连续释放**（或 Client 协议允许乱序 Receipt 但消费侧只暴露到 LAC——二选一，**已定：Receipt 连续释放**，实现简单）。  
- 批量打包（多消息一 entry）：属 entry 内格式；失败整 entry 重试；MessageID 带 batchIndex（M2）。

## 8. 与扩容

- 新 bookie 只出现在**新 fragment / 新 ledger**。  
- 长生命周期 OPEN ledger 可通过**主动 Close 滚本**把后续写入打到含新 bookie 的 ensemble。  
- Sticky read（E=Qw）：读固定一副本，利于 cache；E>Qw 时读需找持有该 entry 的 bookie。

## 9. 故障小结（写路径）

| 故障 | 自愈 |
|---|---|
| 少数 bookie 慢 | 等 Qa；慢节点可后续换下 |
| 未达 Qa | 超时失败；换 fragment 重试或回 Client 可重试错误 |
| Writer 崩溃 | 见 [11-ledger-recovery-deep.md](./11-ledger-recovery-deep.md) |
| 重复 Send | M2 幂等；M1 at-least-once 可能重复 entry（业务去重） |

## 10. 验收用例（设计级）

1. `Qa=2` 时杀 1 个 quorum 成员，仍能 Receipt。  
2. 只 ack 1 个时杀 writer，Recover 后该 entry **可有可无**；Client 未收到 Receipt 必须可重试。  
3. Pipeline 10 条，人为延迟 entry 3：Receipt 不得先发 4..9。  
4. 换 fragment 后旧 entryId 可读且内容不变。
