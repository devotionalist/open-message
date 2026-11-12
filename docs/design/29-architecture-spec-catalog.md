# 架构设计规格目录

本文回答：**架构设计遵循什么规格、有哪些、解决什么问题、如何解决、行业参考是什么**。  
审计如何验收这些规格见 [28](./28-design-audit-requirements.md)。

---

## 1. 规格分层（遵循什么）

不是单一 RFC，而是本仓可执行的分层规格；上层约束下层，冲突时以上层与「权威落点」章为准。

| 层级 | 文档 | 管什么 |
|---|---|---|
| **硬约束** | [00-principles.md](./00-principles.md) | 存算分离、故障自愈、在线扩容不搬历史消息体；L0–L3；故障模型；一致性默认档 |
| **架构四目标** | [34-architecture-goals.md](./34-architecture-goals.md) | 高可用 / 高并发 / 强数据一致性 / 故障·运维对用户无感；冲突优先级与不承诺 |
| **组件角色** | [35-component-roles.md](./35-component-roles.md) | 每个组件/角色的职能、做·不做、权威状态、协作与禁止边 |
| **目标与交付** | [../architecture.md](../architecture.md) §1 | 无状态 Broker、外置可分片 Meta、账本主存、Proxy、四订阅、AR、双阶段可用性；设计了就做 |
| **一致性档位** | 00 §6；34 §3 | Qa 耐久、Ownership 线性化、Recover 尾唯一、Cursor at-least-once、Lookup 可陈旧 |
| **分域细章** | `design/01`–`27` | Ledger / Meta / Cursor / Bundle / Fence / Proxy / 分发 / OL 契约 / 协议 / GC / 幂等 / Offload / Admin… |
| **主存对照** | [17](./17-storage-vs-bookkeeper.md)、[19](./19-open-ledger-integration.md)、[20](./20-broker-openledger-contract.md) | 与 BK/Pulsar 同构；OL 自管 LedgerMeta/AR；Broker 只编排 |
| **审计规格** | [28](./28-design-audit-requirements.md) | 六维审查、传播检查、结果正确性判定、冻结门禁 |
| **开发四性** | [30](./30-engineering-quality.md) | 性能/可用/鲁棒/复用；PR 检查单 |
| **测试五层** | [31](./31-test-strategy.md) | 单元/流程/集成/故障/压力；与 O* 分工 |
| **运维规格** | [32](./32-operations.md)；[../ops/README.md](../ops/README.md) | 安装/部署/扩缩/演练/观测/恢复；Runbook |
| **客户使用** | [33](./33-customer-usage.md)；[../user/README.md](../user/README.md) | 对外说明、语义诚实、SDK 默认行为 |
| **生产级标准** | [36](./36-production-grade-bar.md) | 六角产出须达生产级；骨架升正式规则；生产 HA 总门禁 |
| **合理性说明** | [37](./37-design-rationale.md) | 角色/流程/组件/功能强制五问；无说明不冻结 |

架构师日常必须遵守：**00 三条硬约束 + architecture 交付边界 + 各细章不变量**；改不变量须审计传播（28 §4）。

---

## 2. 规格清单：问题 → 解法 → 权威章

| 问题 | 规格如何定 | 如何解决 | 权威章 |
|---|---|---|---|
| Broker 扩容/宕机要搬消息盘 | 存算分离 | 消息只在 Bookie；Broker 只迁 Ownership | 00、architecture |
| 双主写乱序/分叉 | 单 writer + epoch + Fence | 租约/Promote CAS → Fence → Recover → 新 ledger → Ready 才写 | 06、09、11、16 |
| 已确认消息丢失 | Qa 耐久 + Journal fsync | Receipt 仅 Qa 后；AR 补副本；二次丢 ≥Qa 为已接受风险 | 00、10、03、12 |
| Topic 要逻辑日志又可滚本 | ManagedLedger = **id 链** | OM `LedgerIndex` 只管链；单本详情在 OL | 01、02、19 |
| OM Meta 与 OL Meta 无法原子 | 禁止跨系统单 CAS | **三步滚本** + MarkOrphan + GC | 02 §3.1、22 |
| Meta 成为全局瓶颈 | 分片 KV + 每 key CAS | Oxia/OpenKV；异 key 并行；cursor 合并刷 | 02、architecture §5 |
| 客户端怕 Broker 地址风暴 | Proxy 默认入口 | Lookup/转发；重试消化切主 | 14、21、08 |
| 崩溃有不可用窗口 | 承认 + 压缩；「无感≠零错误」 | Drain/重试/幂等/租约/滚本 → Warm Standby；用户文档诚实 | 15、16、34、33 |
| 四目标口号无验收 | 34 合同分对象 | HA/并发/一致/无感各有机制地图与禁令 | 34 |
| 流程只在聊天里 | 13 总册挂号 | 新流程必须目录表 + 图；跨文档图见 13 §19 | 13 |
| 组件职责口头约定 | 35 角色合同 | 做/不做/权威/协作挂号；OM↔OL 禁边 | 35、19 |
| 多消费者语义 | 四模式必做 | Exclusive / Failover / Shared / Key_Shared | 18、04 |
| 热/冷数据 | 热路径禁直写对象存储 | CLOSED Offload；location 在 OL | 24、20 |
| 切主后业务重复 | 分阶段幂等 | M2 同 Owner；M3 Meta 跨切主；用户文档诚实承诺 | 23、33、user/ |
| 未挂链 ledger 泄漏 | Orphan 安全规则 | MarkOrphan；禁抽样删；默认禁挂回 | 22 |
| 双 Meta 运维盲区 | 对账 + 分面故障 | 全量引用导出；C13–C15；runbook | 27、32 |
| 爆炸半径过大 | LoadManager M2 门槛 | 半自动 Unload + 密度上限 | 05 |
| 只有 HA 叙事不可值班 | 运维规格 + Runbook 门禁 | 检测→隔离→恢复；告警必链手册；无手册不宣称生产 HA | 32、ops/ |
| 集成必须读内部设计 | 客户使用规格 + user 文档 | 仅凭 docs/user+SDK 可收发；禁止假 exactly-once | 33、user/ |

---

## 3. 行业参考

| 参考 | 对齐什么 | 本项目刻意不同 |
|---|---|---|
| **Apache Pulsar** | 存算分离、Bundle Ownership、ManagedLedger、四订阅、Proxy、Cursor 外置 | 控制面自研；主存不绑官方 BK |
| **Apache BookKeeper** | Ledger、E/Qw/Qa、LAC、Fence/Recover、AutoRecovery、存储侧 Meta | **open-ledger**（Rust）；**不要求 BK 协议兼容** |
| **Kafka**（对照） | 分区单 leader、耐久直觉 | **不**以 broker 本地 log 为权威；扩容默认不搬历史消息体 |
| **Oxia / 分片 Meta** | 替代 ZK 单 ensemble 扛全量元数据 | 业务 Meta 默 Oxia，可切 OpenKV |
| **Pulsar Tiered Storage** | seal 后 offload | 字段与路径在 OL；状态机见 24 |

**一句话**：语义规格 ≈ Pulsar 计算面 + BookKeeper 存储面；工程规格 = 双仓（open-message + open-ledger）+ 自研线协议 `OM`。

术语对照表见 [architecture §1.3](../architecture.md)。

---

## 4. 架构师如何使用本目录

1. 先定**权威落点**（谁写哪类 Meta）— §2 表 + [19](./19-open-ledger-integration.md)  
2. 再写状态机/时序；禁止跨系统原子幻想  
3. 用 [00 §4/§5](./00-principles.md) 自检自愈与扩容  
4. 改结论做 [28 §4](./28-design-audit-requirements.md) 传播检查  
5. 交审计；开发/测试/运维/客户用可编码/可测/可运维/可集成再否决  

阅读顺序（与 [../README.md](../README.md) 一致）：**L1** `00`→`34`→`37` → **L2** `architecture`→`35`→`13`→`29` → **L3** 按域（见 [README](./README.md)）→ **L4** `28`… → **L5** `roles`/plan/ops/user。

---

## 5. 与审计、角色的关系

| 文档 | 关系 |
|---|---|
| [28](./28-design-audit-requirements.md) | 审计「这些规格有没有写对、有没有自相矛盾」 |
| [28 §6.1](./28-design-audit-requirements.md) | 如何判定审计结论本身正确 |
| [36-production-grade-bar.md](./36-production-grade-bar.md) | 生产级退出门禁 |
