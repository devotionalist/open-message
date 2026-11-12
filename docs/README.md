# open-message 文档中心

**唯一总入口。** 组织规则：[STRUCTURE.md](./STRUCTURE.md)。  
不以聊天为权威；结论落入对应层正文。

**产品定位**：超大规模 · 高并发 · 高可用 · 高吞吐消息系统；Broker/Proxy/Bookie/Meta **均可在线扩容**（不搬历史消息体）。仓库总览与核心能力见 [../README.md](../README.md)。

```text
L0 入口 → L1 硬约束/目标 → L2 架构总览 → L3 领域细章 → L4 工程门禁 → L5 说明
```

本树只保留 **设计** 与 **说明**（运维手册、用户文档、本地部署说明）。  
过程产物（实现计划、角色手册、握手副本、压测/演练原始输出）**不入库**；本地脚本默认写到 `tmp/ops-records/`（已 gitignore）。

---

## L0 · 入口与规则

| 文档 | 用途 |
|---|---|
| **本页** | 分层导航 |
| [STRUCTURE.md](./STRUCTURE.md) | 如何分层、改文、编号含义 |

**你该怎么读**

| 身份 | 路径 |
|---|---|
| 新人 / 架构 | L0 → L1 → L2 → 按域进 L3 |
| 开发 | L1–L2 扫过 → 相关 L3 → L4·30 |
| 测试 | L2 验收直觉 → L3 场景 → L4·31 |
| 运维 | L2 HA 摘要 → L4·32 → L5 ops |
| 客户 / SDK | L4·33 → L5 user（勿从 L3 内部章上手） |
| 审计 | L1 + L4·28 + 范围内 L3；对照 L2·13/35/37 |

---

## L1 · 硬约束与目标（先于机制）

| 文档 | 一句话 |
|---|---|
| [design/00-principles.md](./design/00-principles.md) | 存算分离 · 故障自愈 · 在线扩容不搬历史；自愈/扩容清单；一致性档 |
| [design/34-architecture-goals.md](./design/34-architecture-goals.md) | 高可用 · 高并发 · 强一致 · 用户无感 |
| [design/37-design-rationale.md](./design/37-design-rationale.md) | 角色/流程/组件/功能 **必须** 有合理性五问 |

冲突时：L1 优先于一切细章口号。

---

## L2 · 架构总览（地图）

| 文档 | 一句话 |
|---|---|
| [architecture.md](./architecture.md) | 系统总览、组件摘要、路径与验收；细节指针进 L3 |
| [design/35-component-roles.md](./design/35-component-roles.md) | 每个组件角色：做 / 不做 / 权威 / 协作 |
| [design/13-flows-and-diagrams.md](./design/13-flows-and-diagrams.md) | 全流程与设计图总册 |
| [design/29-architecture-spec-catalog.md](./design/29-architecture-spec-catalog.md) | 规格分层目录：问题 → 解法 → 权威章 |

读完 L2 应能回答：系统分几面、权威数据在哪、主路径怎么走。

---

## L3 · 领域细章（按域；编号≠阅读序）

完整表与域内顺序：[design/README.md](./design/README.md)。

| 域 | 章 | 管什么 |
|---|---|---|
| **A 存储与账本** | 01, 03, 10, 11, 17, 19, 20, 24 | Ledger、写/Recover、vs BK、OL 权责与契约、Offload、AR |
| **B 元数据与所有权** | 02, 05, 06, 09, 22, 27 | Meta 分片、Bundle、Fence、Broker 生命周期、Orphan、双 Meta |
| **C 订阅与分发** | 04, 07, 18 | Cursor、多租户、四模式 |
| **D 客户端与入口** | 08, 14, 21, 23 | 背压、Proxy、线协议、生产幂等 |
| **E 可用性进阶** | 12, 15, 16, 26 | 组合故障、切主窗口、Warm Standby、Standby 线 |
| **F 控制面** | 25 | Admin API |

依赖提示：做 Produce 先 A+B+D；做消费加 C；做 HA 加 E；接 OL 必读 19/20。

---

## L4 · 工程门禁（各角规格）

| 文档 | 角 |
|---|---|
| [design/28-design-audit-requirements.md](./design/28-design-audit-requirements.md) | 审计：六维、传播、门禁 |
| [design/30-engineering-quality.md](./design/30-engineering-quality.md) | 开发：四性 |
| [design/31-test-strategy.md](./design/31-test-strategy.md) | 测试：五层 |
| [design/32-operations.md](./design/32-operations.md) | 运维：安装～恢复 |
| [design/33-customer-usage.md](./design/33-customer-usage.md) | 客户：对外说明规格 |
| [design/36-production-grade-bar.md](./design/36-production-grade-bar.md) | 全角：生产级标准 |

L4 **不发明** L1/L2 不变量；只规定如何验收与否决。

---

## L5 · 说明（运维 / 用户 / 部署）

| 文档 | 用途 |
|---|---|
| [ops/README.md](./ops/README.md) | Runbook / 可观测性 |
| [user/README.md](./user/README.md) | 客户使用说明 |
| [deploy/local-k8s.md](./deploy/local-k8s.md) | 本地测试拓扑（非生产等价） |

依赖仓：**open-ledger**（存储实现与 OL 设计）。

---

## 快速链（按任务）

| 任务 | 打开 |
|---|---|
| 我要理解整体 | L1 → [architecture](./architecture.md) → [13](./design/13-flows-and-diagrams.md) |
| 我要改一条不变量 | L1 → 相关 L3 → [28 传播](./design/28-design-audit-requirements.md) → [37](./design/37-design-rationale.md) |
| 我要实现能力 | 相关 L3 → [30](./design/30-engineering-quality.md) → [35](./design/35-component-roles.md) |
| 我要写测试 | [31](./design/31-test-strategy.md) → `test/chaos` / `test/opsdrill` / `test/stress` |
| 我要值班 | [32](./design/32-operations.md) → [ops](./ops/README.md) |
| 我要写应用 | [user](./user/README.md) |
| 生产级宣称 | [36](./design/36-production-grade-bar.md) |
