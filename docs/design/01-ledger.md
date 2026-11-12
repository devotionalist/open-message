# Ledger（账本）细章

主文档：§4。通则：[00-principles.md](./00-principles.md)。  
**实现仓**：[open-ledger](../../../open-ledger/)（Rust Bookie / Journal / Client）。  
**已定**：LedgerMeta 与 AutoRecovery 由 open-ledger **自管**；Broker 只维护 `LedgerIndex` 链。见 [19-open-ledger-integration.md](./19-open-ledger-integration.md)。

**深挖**：

- 写路径 / LAC / 换 ensemble → [10-ledger-write-path.md](./10-ledger-write-path.md)  
- Fence / Recover / 场景推演 → [11-ledger-recovery-deep.md](./11-ledger-recovery-deep.md)  
- 跨组件组合故障 → [12-cross-failure-scenarios.md](./12-cross-failure-scenarios.md)  
- 端到端时序图全集 → [13-flows-and-diagrams.md](./13-flows-and-diagrams.md)  

## 1. 为何合理

- 消息耐久单元与 Broker 解耦 → 计算层无状态、存储层独立扩容。  
- Append-only + 关闭不可变 → recovery 简单、可 offload。  
- E/Qw/Qa → 明确耐久与延迟权衡，避免「副本数」含糊。  
- ManagedLedger = ledger 链 → 故障后不必复活旧 writer，开新本即可。

## 2. 数据模型

### 2.1 LedgerMeta（权威，在 **open-ledger** Metadata）

形态与 CAS 由 open-ledger 定义（E/Qw/Qa、state、fragments、lastEntryId、metaVersion…）。  
**open-message Metadata 不存该记录**；Broker 经 open-ledger Client Create/Close/Recover。

### 2.2 Entry（权威字节在 Bookie）

```text
Entry { ledgerId, entryId, lac, payload }
```

Bookie 本地另有：**Journal = WAL**、ledger 长期存储；语义硬约束：

- **对 Client 返回 SendReceipt / 计入 LAC 之前，该 entry 必须已在至少 Qa 个 Bookie 的 Journal 上持久化（落盘）**。  
- 否则 Bookie 进程一挂，已「成功」的消息可能丢 —— **这正是需要 WAL 的原因**。  
- **Broker 不做消息 WAL**（否则又变成有本地消息状态、扩容要搬盘）；耐久只认 Bookie Journal。

### 2.3 ManagedLedger / LedgerIndex（key=`topic`）

```text
LedgerIndex {
  ledgerIds[]          // 有序
  openLedgerId?
  chainVersion
}
```

## 3. 正常路径

### 3.1 Create + Append

1. open-ledger Client.Create(E,Qw,Qa,hints)：**Client 发号 + 选 ensemble**，写 OL LedgerMeta + Bookie 本地结构。  
2. Cas **本仓** LedgerIndex 挂到链尾。  
3. Client.Append：并行写 Qw，等 Qa；推进内存 LAC；SendReceipt。

### 3.2 正常 Close + 滚本

**已定**：OL Close/Create → 本仓 Cas LedgerIndex（有序三步，非跨系统单 CAS）。见 [02-metadata.md §3.1](./02-metadata.md)。

### 3.3 Read

`entryId ≤ LAC`；定位 ledger → 选 sticky bookie（若 E=Qw）或任意持有副本 → Read。

## 4. 故障与自愈

| 故障 | 检测 | 隔离 | 收敛 | 续服 | 数据后果 |
|---|---|---|---|---|---|
| Writer(Broker) 崩溃 | lease 超时 / 主动 Unload | Fence ledger | Recover → CLOSED；开新 ledger | 新 owner 服务 | 未 ack 可能丢或需 Client 重试；已 ack（达 Qa）保留 |
| 单 bookie 写失败 | Append 超时/错误 | 该次 quorum 失败 | 换 ensemble 新 fragment，重试未确认 entry | 同 writer 继续 | 无已 ack 丢失 |
| 单 bookie 永久丢失 | 注册消失 / Auditor | — | AutoRecovery 补副本（[03](./03-autorecovery.md)） | 读改其它副本 | 若同时丢 ≥Qa 且未补齐，理论丢数据 |
| 旧 writer 诈尸 | Fence / epoch | Append → ErrFenced | 忽略 | 新 writer 已服务 | 无双写成功 |
| LAC 未同步到部分节点 | Recover 读修 | IN_RECOVERY | 读修满 Qw 再 CLOSED | — | 尾部收敛唯一 |

### 4.1 Recover（必须幂等；**算法权威唯一：open-ledger design §6.4**）

Broker **只**调用 `Client.Recover(ledgerId)`（及随后 Create + Cas LedgerIndex）；**禁止**在本仓复述/裁剪 fence 覆盖范围或「只 fence 末 fragment」等与 OL 冲突的步骤表。  
概念结果：OPEN/IN_RECOVERY → CLOSED + 唯一 `lastEntryId`；并发 Recover 以 OL Meta CAS 为准。细节与验收见 [11](./11-ledger-recovery-deep.md)、[20](./20-broker-openledger-contract.md)。

**LedgerFenced / 超时**：Client 必须当作不确定结果 → 查重或 Lookup 后按 MessageID/业务键去重（去重为 M2）。

## 5. 扩容

| 动作 | 行为 |
|---|---|
| 加 bookie | 进入池；**新 ledger / 新 fragment** 可用；历史 CLOSED 不动 |
| 单 ledger 写满盘 | Close + 新 ledger 选新 bookie |
| 提高耐久 | namespace 调大 Qa/Qw（影响**新** ledger；旧账本不变） |

热点：单 topic 单 writer 上限 ≈ 单 OPEN ledger 的 quorum 盘能力 → 用 **分区 topic** 水平拆逻辑流（[05](./05-routing-bundle.md)）。

## 6. 不变量

1. OPEN ledger 至多一个 writer。  
2. CLOSED ledger 内容对所有读者相同。  
3. 已向 Client 确认的 entry 已在至少 Qa 个 bookie durable。  
4. MessageID `(ledgerId,entryId)` 在集群生命周期内唯一不复用。  

## 7. 阶段

- M1：E=Qw=Qa=1，Fence/Recover 简化但 API 全。  
- M2：真 quorum + fragment 替换。  
- M3：与 AutoRecovery、offload 联动。
