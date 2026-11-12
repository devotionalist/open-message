# Orphan ledger GC（已定）

上游：[02-metadata.md](./02-metadata.md) §3.1、[12-cross-failure-scenarios.md](./12-cross-failure-scenarios.md)、[19-open-ledger-integration.md](./19-open-ledger-integration.md)。  
执行仓：**open-ledger**（见其 `docs/design.md` §10）；本仓只定义**判定与协作**。  
合理性模板：[37](./37-design-rationale.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 滚本三步非原子 → Create 成功、Index 未挂链 → 盘泄漏 |
| 选型 | 显式 `MarkOrphan` + 全量对账 + grace；删权在 OL |
| 放弃 | 抽样 Index 差集即删；Broker 自扫 Bookie 目录 |
| 代价 | 短暂不一致窗口占盘；需对账任务 |
| 验收 | 22 验收段；禁抽样注入不得删挂链本 |

## 1. 何谓 orphan

| 状态 | 含义 |
|---|---|
| **Orphan** | open-ledger 已有 `LedgerMeta`，且已被显式 `MarkOrphan`，或经**完整** Index 引用校验确认无引用 |
| **正常** | Index 含该 id；权威 state 在 OL LedgerMeta |
| **短暂不一致** | Create 成功、Index Cas 尚未成功 —— **不是立刻删**，先重试 Cas |

根因几乎总是**跨系统非原子**：滚本三步里 OL Create 成功、OM Index Cas 失败 / Broker 崩溃。

## 2. 权责

| 角色 | 做什么 |
|---|---|
| **ManagedLedger** | Index Cas 失败且**永久放弃**挂链时，必须 `Client.MarkOrphan(ledgerId)`；**不得**自己扫删 Bookie |
| **open-ledger GC** | 仅删除 `orphanMarked=true`（或等价）且超过 grace 的本；真正 Delete Meta + bookie 目录 |
| **OM 控制面** | 提供 **完整** Index 引用导出（对账用）；**禁止抽样**当删除依据 |
| **Broker Admin** | 列出 MarkOrphan / 强制删（危险二次确认） |

## 3. 安全规则（已定）

1. **禁止**以「抽样 / 部分 shard 导出」的引用集做差集删除。  
2. **默认禁止**把 orphan **挂回** Index；新主发现旧 OPEN 已 CLOSED 时 **一律 Create L_new** 再 Cas Index。  
3. 删除路径优先认 **`MarkOrphan`**（写路径产生的确定性信号）。  
4. 对账删除（无 MarkOrphan 的残留）仅允许：`R = 全量 Index 快照（带 routingEpoch / 导出世代）`，`orphan = 候选 \ R`，且 **二次全量确认** 同一世代；任一代不一致则中止。  
5. GC 与任何「挂回」若保留为运维例外：须先 CAS OL Meta `claimedByTopic`，GC 只删 `orphanMarked && !claimed`（默认实现可不提供挂回）。

## 4. 判定流程

```text
A. 主路径（推荐）
   MarkOrphan(L) → age > grace → Fence(若 OPEN) → Close/Delete → 拭 bookie

B. 对账路径（收 MarkOrphan 遗漏）
   1. grace = max(rollRetryWindow, leaseTTL * N)   // ≥ 5–15min
   2. 候选 = OL 中 age>grace 且 (OPEN 久无写 | 显式可疑)
   3. R = 完整合集(各 topic LedgerIndex.ledgerIds)  // 同世代全量；禁止抽样
   4. 二次同世代确认仍无引用
   5. CAS OL Meta orphanMarked=true（若尚未）→ 再走 A
```

**禁止**：仅因「某 Broker 本地不知该 id」就删。

## 5. 与滚本补偿

| 步骤失败 | 优先动作 | GC |
|---|---|---|
| Close 失败 | 重试 Close/Recover | 不涉及 |
| Create 失败 | 重试 Create | 无新本 |
| Index Cas 失败 | **重试 Cas**（已含 L_new 则成功） | 永久放弃 → **MarkOrphan(L_new)** |
| Index 指向已 CLOSED 的 open 提示 | Create **新**本挂链 | 旧本正常；**不**挂回其它 orphan |

## 6. LedgerMeta 辅助字段（open-ledger）

对账 aging 需要时间戳：OL `LedgerMeta` 应具备 `createdAt`（或等价）与可选 `orphanMarked` / `claimedByTopic`。见 open-ledger `docs/design.md`。

## 7. 阶段

| 阶段 | 交付 |
|---|---|
| M1 | Admin MarkOrphan / 按 id Delete；演练 |
| M2 | 自动：MarkOrphan 主路径 + 全量对账路径；指标 `orphan_pending` / `orphan_deleted` |
| M3 | 与 Offload / trim 联动 |

## 8. 验收

1. 打断滚本步骤 3 → MarkOrphan → grace 后删除；已挂链本不被删。  
2. **注入「抽样漏 shard」不得触发删除**（对账实现拒绝非全量快照）。  
3. 杀 GC 进程不丢已确认消息。  
