# 生产幂等（已定；分里程碑承诺）

上游：[08-client-backpressure.md](./08-client-backpressure.md)、[15-availability-failover-window.md](./15-availability-failover-window.md)、[21-wire-protocol.md](./21-wire-protocol.md)。  
**客户可读摘要**（禁止超本文承诺）：[../user/delivery-semantics.md](../user/delivery-semantics.md)；规格门禁：[33-customer-usage.md](./33-customer-usage.md)。  
合理性模板：[37](./37-design-rationale.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 超时/重绑重发导致重复 Append；切主后更易重复 |
| 选型 | `(producer_name, sequence_id)` 分阶段：M2 同 Owner 内存；M3 Meta 跨切主 |
| 放弃 | 默认 exactly-once；用 entryId 跨滚本去重 |
| 代价 | M2 崩溃仍可重复；M3 Meta 写放大 |
| 验收 | 23 §6；测勿把 M2 当跨杀主精确一次 |

## 1. 目标（按阶段，禁止混写）

| 阶段 | 承诺 | 不承诺 |
|---|---|---|
| **M1** | at-least-once；无服务端去重 | 切主/超时重发不重复 |
| **M2** | **同 Owner 连接生命周期内**：同 `(producer_name, sequence_id)` 重发 → 原 MessageID，不再 Append | **跨 Owner 崩溃切主**后精确一次 |
| **M3** | 幂等表进 OM Metadata（按 topic 分片）→ **跨切主**仍返回原 MessageID | — |

M2 价值：Proxy/SDK 超时重试、连接闪断重绑到**同一仍存活 Owner** 时安全。  
切主场景的业务去重：M2 靠优雅 Unload 降低窗口 + 应用层；**跨崩溃精确一次是 M3 验收项**，不得写进 M2 退出标准。

## 2. 键与窗口

```text
IdemKey = (topic, producer_name)
sequence_id: uint64   // Client 单调
```

| 项 | 约定 |
|---|---|
| 去重粒度 | Owner 上 `lastSequenceId` + 环形缓冲 `seq → MessageID` |
| 窗口 | 最近 **N** 条（建议 N≥1024）；窗外策略可配（拒 / 当新写） |
| M2 持久化 | **仅 Owner 内存**（Unload 可尽力刷快照到接任者；崩溃则丢） |
| M3 持久化 | OM Meta `idem/{topic}/{producer_name}`：`lastSeq` + 窗口摘要；随 Receipt 异步 Cas |

## 3. 协议字段

见 [21](./21-wire-protocol.md)：

- `Producer`：`producer_name`（幂等必填）、`initial_sequence_id?`  
- `Send` / `SendReceipt`：带回 `sequence_id`；重复已确认 seq → 同 Receipt，不 Append  

## 4. Broker 状态机（M2 内存）

```text
on Send(seq):
  if seq in map:           → return cached MessageID
  if seq < base:           → ErrOutOfOrder / 忽略（可配）
  if seq > last+W:         → 拒或背压（pipeline 上限 W）
  // 允许 last+1 … last+W 并发；与 LAC 连续释放一致：Receipt 按序释放
  Append via OL Client
  on Qa: map[seq]=MessageID; 按序推进 last; SendReceipt
```

**已定 pipeline**：空洞未齐不向 Client 释放更高 seq 的成功（对齐写路径 LAC）。

## 5. 与切主

| 事件 | M2 | M3 |
|---|---|---|
| 同连接重复 Send | 去重 | 去重 |
| Owner 存活，重绑连接 | 窗口仍在则去重 | 同左 + Meta |
| Owner 崩溃 | 窗口丢 → **允许业务重复** | Meta 命中 → 不重复 |
| 优雅 Unload | 尽力把窗口交给新主（尽力而为，非硬保证） | Meta 已持久则硬保证 |

**不得**用 entryId 去重代替 producer 幂等（滚本后 entryId 重计）。

## 6. 验收

1. M2：同 Owner 重复 seq → 单 entry、双 Receipt 同 MessageID。  
2. M2：Kill Owner 后重发未确认 seq → **允许**重复（测试断言「可重复」而非「禁止重复」）。  
3. M3：Kill Owner 后重发 → 不重复。  
4. 关闭幂等（无名 producer）= M1 语义。  
