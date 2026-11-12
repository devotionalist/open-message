# 双 Meta 运维与对账（已定）

合理性模板：[37](./37-design-rationale.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | OM 与 OL Meta 无法原子 → 分面故障与引用空洞不可避免 |
| 选型 | 全量同世代对账 + 分面 runbook + 耦合发布纪律 |
| 放弃 | 假装单 Meta；抽样导出当删除依据 |
| 代价 | 对账任务与运维复杂度 |
| 验收 | C13–C15；指标 `index_ledger_missing_ol` |


上游：[19-open-ledger-integration.md](./19-open-ledger-integration.md)、[02-metadata.md](./02-metadata.md)、[22-orphan-gc.md](./22-orphan-gc.md)。

## 1. 为何单开一章

OM Metadata（Ownership / Index / Cursor / Node）与 OL Metadata（LedgerMeta / registry / AR）**必然**两套。跨系统无原子；生产 HA 依赖**对账、分面故障演练、耦合发布**，不能只靠滚本文字。

## 2. 对账任务（M2 必做）

| 检查 | 指标 / 动作 |
|---|---|
| Index 中每个 `ledgerId` 在 OL 有 Meta | `index_ledger_missing_ol`；告警；禁止 trim 误伤 |
| OL 有 Meta 但 Index 无引用且超 grace | 走 [22](./22-orphan-gc.md) MarkOrphan/对账删 |
| Index.`openLedgerId` 在 OL 非 OPEN/IN_RECOVERY | Owner 触发 Create+Cas 或 Recover 补偿 |
| `openStateHint` ≠ OL state | 仅指标；权威永远是 OL |

导出 Index 引用集必须 **全量同世代**（禁止抽样当删除依据）。

## 3. 分面故障（runbook + chaos）

| 故障 | 现象 | 期望 |
|---|---|---|
| **OM Meta 挂** | Lookup/Acquire/Cursor/Index Cas 失败；续约失败 → Owner 停写 | Bookie 已 ack 数据仍在；恢复后 Cas/Acquire 自愈 |
| **OL Meta 挂** | Create/Close/Recover/Fence Meta 失败；滚本/切主钉尾堵 | 已缓存 OPEN 句柄或可短暂继续 Append；**不得**假装完成 Recover |
| **双挂** | 控制面停 | 数据面已加载 Owner 服务至租约/探测失效 |

Chaos 场景建议：OM-only / OL-only / 双挂（落 `test/chaos`，对照 [31](./31-test-strategy.md)）。

## 4. 部署策略

- 允许同 Oxia 集群、**必须** key 前缀隔离（`/open-message/...` vs `/open-ledger/...`）。  
- 版本与扩容：**耦合变更窗口**（先扩容 Meta 后端，再滚 Broker / openledgerd）。  
- 禁止「只备份其中一套 Meta」当完整灾难恢复。

## 5. 验收

1. 故意制造 Index 有、OL 无 → 指标响；不静默。  
2. C13/C14：对应面恢复后集群可写可读。  
3. 对账任务与 Orphan GC 共用全量导出，无第二套抽样逻辑。  
