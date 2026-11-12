# Bookie 复制与 AutoRecovery

主文档：architecture §14。通则：[00-principles.md](./00-principles.md)。  
**已定**：AutoRecovery **由 open-ledger 实现与运维**，open-message Broker **不实现 Auditor/Worker**。详见 [19-open-ledger-integration.md](./19-open-ledger-integration.md) 与 open-ledger `docs/design.md` §9。

## 1. 为何合理

写入路径的 Qw/Qa 只保证「写入当时」的副本数。Bookie 随后宕机会使实际副本 &lt; Qw。没有补副本，二次故障可能丢已确认数据。故 AR 是耐久闭环。

与扩容区别：扩容默认不搬历史；AR 按需复制缺失 entry。

## 2. 权责（对齐 Pulsar/BK）

| 角色 | 归属 |
|---|---|
| Auditor / RecoveryQueue / ReplicationWorker | **open-ledger**（sidecar 或等价进程） |
| LedgerMeta / bookie registry / underreplicated | **open-ledger Metadata** |
| Broker / open-message Metadata | **不**实现 AR，不写上述 key |

## 3. Broker 对 AR 副作用的契约（已定）

OL 可对 **OPEN** 尾段在 grace 后 **fence** 再补副本（见 OL design §9）。Broker 仍是 Owner 时可能突然收到 `ErrFenced` / 见 `IN_RECOVERY`。

**ManagedLedger 规范（同 Owner，不换 Ownership）**：

```text
on Append/Close → ErrFenced 或 Meta.state ∈ {IN_RECOVERY, CLOSED}:
  1. 停止向该 openLedgerId 赋新 entryId
  2. Client.Recover(openLedgerId)     // 或已 CLOSED 则取 lastEntryId
  3. L_new = Client.Create(...)
  4. Cas LedgerIndex 挂 L_new         // 三步滚本；失败 → MarkOrphan
  5. 仅在新本上继续 Produce
```

禁止：忽略 Fenced 继续盲重试同一 OPEN 本；禁止未 Recover 钉尾就开写新本。  
Client 侧：对进行中的 Send 返回可重试错误；依赖 [23](./23-producer-idempotency.md) 阶段承诺。

## 4. Broker 侧其它只需知道

1. Produce/Recover 走 open-ledger Client；under-replicated 由存储侧修。  
2. 演练 O9 验证的是 **open-ledger AR**。  
3. 实现计划 F3.1 = **集成验证**，本仓不重写 Auditor。

## 5. 扩容边界

加 Bookie → open-ledger 注册；新 ledger/fragment 可用新节点。历史 CLOSED 默认不搬。

## 6. 验收（集成）

1. 掉 1 个 Bookie（Qw≥2）：已确认可读；AR → underreplicated=0。  
2. AR fence OPEN 本时：Owner **自动同主滚本**，topic 恢复可写；无双写。  
3. Broker 进程内无 AR 调度线程；OM Meta 无 `led/` / underreplicated 键。  
