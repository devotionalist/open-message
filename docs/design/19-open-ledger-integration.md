# open-message ↔ open-ledger 集成与权责（已定）

通则：[00-principles.md](./00-principles.md)。对照 Pulsar/BK：[17-storage-vs-bookkeeper.md](./17-storage-vs-bookkeeper.md)。  
open-ledger 设计：仓库内 `docs/design.md` / `docs/architecture.md`。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 若 Broker 再管一份 LedgerMeta → 双写与 AR/Recover 分叉 |
| 选型 | OL 自管账本 Meta/AR；OM 只管业务 Meta；Broker 经 Client 编排 |
| 放弃 | 本仓实现 Bookie/AR；跨 OM+OL 单 CAS |
| 代价 | 滚本三步非原子，需 MarkOrphan/对账 |
| 验收 | 19 禁止事项；本仓无 `led/`、无 `cmd/bookie` |

## 1. 已定原则

**open-ledger 的元数据自己管理，Broker 不参与。**  
**Broker 只管理 open-message 自己的元数据**（Ownership / 逻辑日志链 / Cursor / 策略等）。

与 Pulsar 同构：

| Pulsar | 本项目 |
|---|---|
| BookKeeper Metadata + AutoRecovery | **open-ledger**（Oxia 上等存储元数据 + AR） |
| Pulsar Metadata + ManagedLedger | **open-message** Metadata + Broker ManagedLedger |
| Broker 内 BK Client | Broker 内 **open-ledger client** |

```text
open-message Broker
  ├── open-message Metadata（Oxia / OpenKV）
  │     Ownership / LedgerIndex(链) / Cursor / Broker Node / 策略 / Schema…
  └── open-ledger Client
        ├── gRPC → Bookie（entry 字节）
        └── 读写 open-ledger 自管 Metadata（LedgerMeta / registry / AR 队列）
              ▲
              │ Broker 不直接写这些 key
```

组件角色总册（Broker 内外全部角色）：[35-component-roles.md](./35-component-roles.md)。

## 2. 权责表

| 数据 / 能力 | 权威位置 | 谁写 | Broker 做什么 |
|---|---|---|---|
| Entry 字节、Journal | open-ledger Bookie | Bookie | 经 Client Add/Read |
| 单本 **LedgerMeta**（E/Qw/Qa、state、fragments、lastEntryId、masterKey、**OffloadLocation?**） | **open-ledger Metadata** | **open-ledger Client**（Create/Close/Recover） | **不**往 open-message Meta 写 LedgerMeta |
| Bookie 注册 / 心跳 / underreplicated / AR 选举 | **open-ledger Metadata** | Bookie / AR | 不参与 |
| **AutoRecovery** | open-ledger（Auditor + Worker） | AR sidecar | 不实现、不调度 |
| Topic→ledger **链**（`ledgerIds[]`、`openLedgerId`） | **open-message Metadata · LedgerIndex** | **ManagedLedger** | Cas 维护链 |
| Ownership / Cursor / 订阅 / NS 策略 | open-message Metadata | Broker | 本职 |
| Broker 节点注册 | open-message Metadata · NodeStore | Broker | 本职 |
| Fence + Recover 算法 | open-ledger Client + 其 Metadata | Client 执行 | Owner 切换时 **调用** Client Recover/Close/Create，再更新 LedgerIndex |
| **MarkOrphan** | open-ledger LedgerMeta | **OL Client** | 挂链永久失败时调用；见 [20](./20-broker-openledger-contract.md)、[22](./22-orphan-gc.md)；禁止 Broker 自扫盘删 |
| OffloadLocation | open-ledger LedgerMeta | OL / offload worker | Broker 不写；读经 Client |
| ledgerId 发号 / ensemble | open-ledger Client | Client | 只传 E/Qw/Qa 等策略；见 [20](./20-broker-openledger-contract.md) |

## 3. Broker 禁止事项

1. 在 open-message Metadata 中持久化 ensemble / Qw/Qa / fragment / CLOSED.lastEntryId（那是 open-ledger LedgerMeta）。  
2. 实现或部署与 open-ledger 并行的第二套 AutoRecovery。  
3. 用本地盘做消息 WAL 或权威 ledger 状态。  
4. 绕过 Client 直接猜「某 Bookie 本地 max entryId = 账本尾」。

## 4. ManagedLedger 交互（概念）

```text
Produce:
  Client.AddEntry(openLedgerId, …)     // open-ledger：写 Bookie + 必要时触及其 Meta
  成功 → Receipt；失败按错误重试 / 换本

滚本 / 切主:  **有序三步**（已定，见 design/02 §3.1）
  1. Client.Fence+Recover 或 Close(旧)   // open-ledger Meta
  2. Client.Create(新)                  // open-ledger Meta
  3. Cas LedgerIndex 挂新 id            // 仅此步写 open-message Meta
  禁止幻想「跨 OL Meta + OM Index 的单次原子 CasCloseAndAppend」

消费:
  读 LedgerIndex 定位 (ledgerId, entryId) → Client.Read
```

`LedgerIndex.openStateHint` 仅缓存提示；**权威 OPEN/CLOSED 以 open-ledger LedgerMeta 为准**。

## 5. Metadata 后端可分离

| 用途 | 建议 |
|---|---|
| open-message 业务 Meta | `METADATA_BACKEND=oxia\|openkv`（本仓配置） |
| open-ledger 账本 Meta | open-ledger 自配 Oxia（可同集群**不同 namespace/前缀**，避免 key 冲突） |

允许同 Oxia 集群、**必须** key 前缀隔离（例如 `/open-message/...` vs `/open-ledger/...`）。

## 6. 相关细章

- 业务 Meta：[02](./02-metadata.md)；AR 与同主滚本：[03](./03-autorecovery.md)  
- 契约：[20](./20-broker-openledger-contract.md)；Orphan：[22](./22-orphan-gc.md)  
- 双 Meta 对账 / 分面故障：[27](./27-dual-meta-ops.md)  

## 7. 验收

1. 杀 Broker：open-ledger LedgerMeta / entry 不丢；新 Owner 只调 Client Recover + 读本仓 Cursor/Index。  
2. 杀 Bookie：由 open-ledger AR 补副本；Broker 无 AR 代码路径。  
3. 滚本成功后：open-message Index 与 open-ledger LedgerMeta 的 ledgerId 集合一致（Index ⊆ 已创建账本）；无本仓 `led/` 键。  
