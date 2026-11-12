# 深挖：Fence、Recover、尾部收敛

上游：[01-ledger.md](./01-ledger.md)、[06-fencing-consistency.md](./06-fencing-consistency.md)、[10-ledger-write-path.md](./10-ledger-write-path.md)。  
**算法权威**：[open-ledger `docs/design.md` §6.4 Recover](../../../open-ledger/docs/design.md)（含全 fragment fence、`maxLac+1` 前向扫描、fence-on-read / BK #2614）。  
**执行体**：Fence / Recover / CAS LedgerMeta **全部在 open-ledger Client**；Broker 只编排 `Client.Recover` 后 Cas 本仓 LedgerIndex。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | Writer 消失后副本不一致 → 双主/尾分叉 |
| 选型 | Fence 后 Recover；算法唯一在 OL §6.4；OM 只挂链 |
| 放弃 | Broker 猜 Bookie 本地 max；本仓复述裁剪版 Recover |
| 代价 | 切主窗口含 Recover 耗时 |
| 验收 | 全局同一 lastEntryId；IN_RECOVERY 禁止假 CLOSED |

## 1. 要解决的问题

Writer 消失时，集群里可能存在：

- 部分 bookie 有 entry K，部分没有；  
- Client 可能已收到或未收到 K 的 Receipt；  
- 旧 Writer 稍后「诈尸」继续写。

必须同时做到：

1. **旧 Writer 再也无法达到 Qa**（Fence）。  
2. **新读者/新 Writer 看到同一最终尾部**（Recover → CLOSED）。  
3. **再写必须开新 ledger**（不复活 CLOSED）。

## 2. Fence 法定人数

对 Write Quorum（大小 Qw），至少 `F = (Qw - Qa) + 1` 个成员已 fence ⇒ 旧 Writer 最多 `Qa - 1` 个有效 ack。  
**覆盖范围**：OL 要求对 **每个 fragment 中出现过的 bookie** 做 fence，且每个 write-set 窗口满足 coverage——**不要**只 fence「最后一个 fragment」就当完成（那是过简近似，已废止为权威）。

## 3. Broker 编排（非算法副本）

```text
// Broker / ManagedLedger — 切主或同主遇 Fenced（见 03）
last = OL.Client.Recover(ledgerId)   // 内部实现以 OL §6.4 为准
L_new = OL.Client.Create(...)
Cas LedgerIndex: append L_new, open=L_new
// 然后才对外 Ready / 继续 Produce
```

本章 **不再**维护一份与 OL 可能漂移的逐步伪代码。实现与单测以 open-ledger Client / §6.4 为准。

### 3.1 卡住 `IN_RECOVERY`（已定策略）

若 `Recover` 因 NotEnoughBookies 返回失败 / Meta 停在 `IN_RECOVERY`：

| 允许 | 禁止 |
|---|---|
| 指数退避重试 Recover | 未钉尾就 Create 新本并当作连续日志 |
| 可选：只读服务到**上次已知 durable LAC hint**（不推进新 Receipt） | 向 Client 对开本尾假装 CLOSED |
| 告警 `ledger_in_recovery_age` | 静默丢弃 IN_RECOVERY 账本 |

## 4. 端到端场景推演

设定：`E=3, Qw=3, Qa=2`，ensemble `[B0,B1,B2]`。

### 场景 A：已确认 entry 不丢

1. entry 0,1 已 Receipt（各至少 2 副本）。  
2. Writer 崩溃。  
3. Recover（OL §6.4）→ CLOSED `last=1`。  
4. 新 ledger 从 entry 0 再计（新 ledgerId）。  

**Client**：已持有 MessageID 的不必重发；未 Receipt 的从下一业务消息重发。

### 场景 B：未确认 entry「可能在」

1. entry 2 只到 1 副本，无 Receipt。  
2. Writer 崩溃。  
3. Recover 可能 `last=1`（截断）或 `last=2`（读修进尾）。  
4. Client 重发 → 可能业务重复（M2 同 Owner 去重无济；**M3** Meta 幂等）。

### 场景 C：诈尸

旧 Writer 在 Fence coverage 达成后无法再达 Qa；新主只在新 ledger 写。

### 场景 D：双 Recover

两 Broker 同时 Recover：OL Meta CAS 一胜一败；败者见 CLOSED 对齐 `lastEntryId`。

## 5. 短路径 hint（Warm Standby）

本地 LAC / TailHint **只**缩小扫描起点；仍须完整 Fence coverage + 前向 fenced read + CAS CLOSED。见 [16](./16-warm-standby.md)。

## 6. 不变量

1. Fence coverage 达成前，不得 CAS CLOSED。  
2. CLOSED.lastEntryId 只定一次（同值幂等）。  
3. 已 Receipt ⊆ 达 Qa ⊆ Recover 后 CLOSED 尾（在 Qa 耐久假设下）。  
4. Broker 不实现第二套 Recover 算法。  

## 7. 验收

1. 杀 Writer：新主 Recover 后尾唯一；诈尸 Append 失败。  
2. 实现代码路径调用 OL Client，无本仓复制 §6.4 分叉逻辑。  
3. bookie 不足时保持 IN_RECOVERY，指标可见，不假 CLOSED。  
