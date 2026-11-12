# 设计审计要求

通则：[00-principles.md](./00-principles.md)。  
范围：`docs/architecture.md`、`docs/design/*`、`docs/deploy/*`，以及依赖仓 [open-ledger](../../../open-ledger/) 中与 Broker 契约相关的章节（至少 `docs/design.md` §3/§6.4/§9/§10）。

**目的**：在实现与里程碑冻结前，用统一标准审查设计是否合理、有无架构/一致性/恢复/HA·并发/运维缺陷，并强制做「修复传播」与回归。

---

## 1. 何时必须审计

| 触发 | 要求 |
|---|---|
| 重大权责变更（OM/OL Meta、滚本、Ownership、Recover） | 全量六维审计 |
| 关闭一批 Critical 设计缺陷后 | **复审**（至少回归表 + 传播检查） |
| M1 / M2 / M3 退出评审 | 按 §7 门禁；未清 Critical 不得标「设计冻结」 |
| 新增细章或改不变量 | 对该章 + 交叉引用章做局部审计 |

产出物：审计结论（可 Markdown / Canvas），含严重度、证据路径、失败场景、改法；**不得**只给印象分。

---

## 2. 严重度

| 级 | 定义 | 门禁 |
|---|---|---|
| **Critical** | 按文实现会导致丢已确认数据、双写分叉、误删活账本、或与权威章直接矛盾且会被照抄 | 设计冻结 / 对应里程碑退出前 **必须改文** |
| **Important** | 实现路径未钉死、契约缺口、运维不可执行；不立刻丢数但上线会踩坑 | 进入该路径编码前必须改文 |
| **Polish** | 措辞、交叉引用、索引过期、非规范性残留 | 下一次文档整理清掉 |

**已接受风险**须单独成表（如崩溃窗口、M2 不跨切主精确一次、Qa 二次故障），不得与缺陷混报。

---

## 3. 六维检查清单

每维至少回答下列问题；任一「否」须记入发现项（或明确列为已接受风险并指向章节）。

### 3.1 合理性

权威模板与总表：[37-design-rationale.md](./37-design-rationale.md)。

1. 范围内**角色 / 流程 / 组件 / 功能**是否均有合理性说明（37 五问：问题、选型、放弃方案、代价、验收）？无说明 → 本维发现项。  
2. 是否符合 [00](./00-principles.md) 三条硬约束（存算分离、故障自愈、在线扩容不搬历史消息体）？  
3. 是否对照 [34](./34-architecture-goals.md) 四目标（HA/并发/强一致/用户无感）且未用双主换无感、未用搬盘换扩容、未超里程碑宣称 exactly-once？  
4. 关键路径是否在 [13](./13-flows-and-diagrams.md) 目录表挂号且有设计图/时序图（或明确指向细章图）？  
5. 组件边界是否与 Pulsar/BK 同构叙事一致（Broker 软状态；OL 管 LedgerMeta/AR；OM 管 Ownership/Index/Cursor）？  
6. 是否存在「跨 OM Meta + OL Meta 单次原子 CAS」类幻想？滚本是否仍是有序三步 + 补偿？  
7. 里程碑承诺是否诚实（例如 M2 幂等 ≠ 跨崩溃精确一次）？

### 3.2 架构

1. 权威数据落点唯一吗？（LedgerMeta / OffloadLocation / ledgerId 发号 / Bookie 注册 → OL；Index 链 / Ownership / Cursor → OM）  
2. Client API / Admin / 线协议 / Standby 流是否分面清晰、无职责重叠？  
3. 控制面是否过薄（LoadManager、对账、失败探测）以至于 HA 叙事无法落地？  
4. 是否与 open-ledger 文档冲突（Recover、AR OPEN fence、orphan 字段）？

### 3.3 数据一致性

1. 单 bundle 至多一主；Produce Ready 前是否完成 Fence/Recover/挂链？  
2. 已 Receipt ⊆ 达 Qa；LAC 是否连续释放？  
3. Cursor：权威是否只在 CursorStore？Standby 是否禁止 `max(本地, Meta)`？  
4. Shared：`CasAdvance` 是否禁止跳过 `individualAcks` 空洞？Unload 是否刷整份 CursorValue？  
5. Orphan：是否禁抽样删除、默认禁挂回、主路径 MarkOrphan？  
6. 幂等：同 Owner / 跨切主承诺是否与 [23](./23-producer-idempotency.md) 一致且全文无回退措辞？

### 3.4 故障恢复

1. 对照 [00 §4](./00-principles.md)：检测 / 隔离 / 收敛 / 续服 / 数据语义 / 风暴控制是否齐全？  
2. 旧主诈尸：epoch + Fence 是否保证达不到 Qa？  
3. Owner 仍持有合法 epoch 时遇 `ErrFenced`（AR）→ 是否规定同主滚本（[03](./03-autorecovery.md)）？  
4. Recover 算法权威是否唯一指向 OL §6.4？OM 是否仍残留「只 fence 末 fragment / 修满 Qw」？  
5. `IN_RECOVERY` 卡住是否禁止假 CLOSED？  
6. 组合故障是否覆盖：[12](./12-cross-failure-scenarios.md) + OM-only / OL-only / 双 Meta？

### 3.5 高可用 / 高并发

1. 崩溃窗口是否承认且有阶段一缓解（Drain、Proxy/SDK 重试、探测、滚本）？  
2. Promote 是否可早于满 TTL，且与 TryAcquire 同一 Ownership CAS？相关章（06/09/15）是否已同步？  
3. LoadManager 半自动 + `max_bundles_per_broker` 是否为 M2 退出条件？  
4. 并发天花板是否写清：单 OPEN ledger / 单 Dispatcher / 热 key 不随加 Broker 线性升？  
5. Proxy Lookup 惊群、NodeStore/RoutingTable 热点是否有对策？  
6. 隔离组是否有 min capacity，避免缩成单点？

### 3.6 运维操作

1. 是否有可执行 **Runbook**（检测→隔离→恢复→验收）：OM Meta 挂、OL Meta 挂、双挂、Drain/Unload、Promote、Orphan GC、Trim、Offload？  
2. Admin API 是否覆盖 day-2（与 runbook 一一对应）？危险操作是否二次确认？  
3. 指标 / 告警 / SLO 是否成目录并链到 runbook？  
4. Backup/Restore：双 Meta + Bookie/冷层对象；联合恢复序；禁止单面当完整 DR？  
5. 滚动升级组件序与版本 skew / 回滚条件？  
6. Chaos：C13/C14/C15（及关键路径）是否进入矩阵与 Nightly，而非仅功能 ID？  
7. 本地/测试拓扑与生产运维是否区分标明？

---

## 4. 修复传播检查（强制）

改「主章节」后，必须扫下列卫星章，禁止只改一处：

| 主结论 | 必须同步 |
|---|---|
| Promote 可早于 TTL | `16` → `06`、`09`、`15`、`26` |
| Recover = OL §6.4 | `11` → `01`、`13`；删冲突伪代码 |
| Cursor = CursorStore | `16` → `26`、验收项 |
| M2/M3 幂等边界 | `23` → `08`、`12`、`15`、`architecture`、`13`、`10` |
| MarkOrphan / 禁挂回 | `22` → `02`、`12`、`20`、architecture Client API、OL schema |
| Ownership 含 standby/lastRenewAt | `16` → **`02` 权威 schema** |
| LoadManager M2 | `05` → `09`、`25` |
| 双 Meta 对账 | `27` → chaos 表、runbook、指标 |

审计报告须含 **「传播检查」** 小节：列出已扫路径与仍漂移项。

---

## 5. 与 open-ledger 对齐检查

| 主题 | OM 侧 | OL 侧须一致 |
|---|---|---|
| Recover | 只调 Client；无分叉算法 | `design.md` §6.4 |
| AR 对 OPEN fence | Broker 同主滚本 | §9；与 HOT_DELETED/offload 交互 |
| Orphan | MarkOrphan + 全量对账 | §10；`createdAtMs` / `orphanMarked` |
| OffloadLocation | Index 不存 | LedgerMeta 字段 |
| ledgerId | Broker 不发号 | Client 发号 |

发现冲突时：**算法/存储 Meta 以 OL 为准**；业务编排以 OM 为准；OM 细章改为指针，不复制易漂的逐步伪代码。

---

## 6. 报告格式（最低字段）

```text
标题 / 审计时点 / 范围
总判（≤5 句）
上次 Critical 回归表：Fixed | Partial | Open
本轮发现：
  id | 严重度 | 维度 | 标题 | 证据(路径+原文要点) | 失败场景 | 建议改法
已接受风险表
文档矛盾表（A 章 vs B 章）
运维成熟度记分卡（已设计 / 薄 / 缺失）
传播检查（已扫路径 + 仍漂移项）
建议修复顺序
```

复审时必须重跑 §4 传播检查，不得假设「主章已改 = 全集群一致」。

---

## 6.1 如何判定「审计结果正确」（已定）

审计结论的正确性**不靠审计员自信**，靠可复核证据 + 对抗验证 + 回归。

### 单条发现可证伪

每条 Critical/Important 必须同时具备：证据（路径+原文或明确缺失）、失败场景、建议改法。缺任一项 → 该条无效，不得进门禁依据。

### 严重度校准

严格按 §2：已接受风险（崩溃窗口、M2 不跨切主精确一次、Qa 二次故障等）不得标 Critical。误报/漏报用 §2 定义对质。

### 对抗验证（其它角色验审计）

| 角色 | 如何证明审计对/错 |
|---|---|
| 架构 | 证据是否断章取义；豁免是否过宽 |
| 开发 | 改法是否仍不可编码 |
| 测试 | 要求的场景能否落到用例/Chaos ID |
| 运维 | 运维维结论能否落到 runbook/Admin |
| 客户 | 是否把内部细节误当成对外承诺缺陷 |

审计 `PASS` 但被开发/测试/运维以不可实现/不可测/不可运维否决 → 本轮结论不得单独作为生产级退出依据。

### 传播与复审

- 「主章已修」须经 §4；卫星章仍有反向表述 → 回归表标 **Partial**，不得标 Fixed。  
- 每次复审必跑 §8；仍在 = 漏检或假修复。  
- OM 与 OL 冲突：算法以 OL 为准；OM 仅编排指针算对齐。

### 操作定义（同时满足才认本轮审计可靠）

1. 报告字段完整（§6）；每条 Critical/Important 可复现判断。  
2. §8 高敏项均有 Fixed/Open/Partial，无「未扫」。  
3. §4 传播检查列出已扫路径。  
4. 架构对误报有书面接受或驳回理由。  
5. 开发对改法「可编码」、测试/运维对相关维「可测/可运维」有确认（可用握手单）。  
6. 下一轮复审：无静默复活的 Critical。

细则见本文；生产级见 [36](./36-production-grade-bar.md)。

---

## 7. 门禁

| 门禁 | 条件 |
|---|---|
| **设计冻结（编码主路径前）** | 无开放 Critical；Important 有明确延期里程碑 |
| **M2 设计退出** | C6 LoadManager、C5 对账任务、Orphan 主路径、同 Owner 幂等承诺、传播检查通过；运维：至少有 Drain/Unload/orphan 的 runbook 草案 |
| **M3 设计退出** | Standby Promote 早探测全文一致、跨切主幂等、Offload 状态机、C13–C15 入演练表；运维：backup/restore 薄章 + 双 Meta runbook + 指标目录 |
| **宣称「生产 HA」** | Qa≥2 + AR SLO；隔离组 min capacity；Backup/Restore 可演练；非仅 M1 Qa=1 沙箱；**六角产出达 [36](./36-production-grade-bar.md) 生产级** |
| **里程碑退出** | 握手单含 36 生产级栏；无「仅骨架/仅聊天」项进入完成清单 |

---

## 8. 已知高敏矛盾（审计必扫）

以下为历次审计反复出现的漂移点，**每次复审必 grep / 必读**：

1. `06`「租约未过不应有新主」vs `16` 早 Promote  
2. `01`/`13` Recover 文案 vs OL §6.4  
3. `02` OwnershipRecord 字段 vs `16` standby/lastRenewAt  
4. architecture `CasAdvance` vs `04`/`18` Shared 洞安全  
5. Client API 列表是否含 `MarkOrphan`  
6. 「M2 幂等」是否被写成跨切主保证  
7. Chaos 矩阵是否含 C13–C15  
8. Orphan「抽样」「挂回」禁令是否被写回  

---

## 9. 相关文档

| 文档 | 用途 |
|---|---|
| [00-principles.md](./00-principles.md) | 自愈/扩容检查清单 |
| [27-dual-meta-ops.md](./27-dual-meta-ops.md) | 双 Meta 对账与分面故障 |
| [22-orphan-gc.md](./22-orphan-gc.md) | Orphan 安全规则 |
| [23-producer-idempotency.md](./23-producer-idempotency.md) | 幂等阶段承诺 |
| 审计产物示例（历史） | 会话内 Canvas `design-audit-v2` / `v3`（非规范正文） |
| 生产级标准 | [36-production-grade-bar.md](./36-production-grade-bar.md) |
| 架构规格目录 | [29-architecture-spec-catalog.md](./29-architecture-spec-catalog.md) |
| 生产级全角色标准 | [36-production-grade-bar.md](./36-production-grade-bar.md) |
| 设计合理性说明 | [37-design-rationale.md](./37-design-rationale.md) |
| 设计索引 | [README.md](./README.md) |
