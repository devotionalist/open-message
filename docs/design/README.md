# 设计文档目录（L1–L4）

**总入口**：[../README.md](../README.md)（分层导航）。  
**组织规则**：[../STRUCTURE.md](../STRUCTURE.md)。  
本页只排 `docs/design/*`：**编号 ≠ 阅读顺序**；按层与域递进。

评审以本目录细章 + [../architecture.md](../architecture.md) 为准。聊天结论须落入正文。

---

## 阅读递进（design 内）

```text
L1  00 → 34 → 37
L2  （回总览 architecture）→ 35 → 13 → 29
L3  按域 A→F 选读（下表）
L4  28 →（开发 30 / 测试 31 / 运维 32 / 客户 33）→ 36
```

---

## L1 · 硬约束与目标

| 编号 | 文档 | 内容 |
|---|---|---|
| 00 | [00-principles.md](./00-principles.md) | 三条硬约束；自愈/扩容清单；一致性档 |
| 34 | [34-architecture-goals.md](./34-architecture-goals.md) | 高可用 / 高并发 / 强一致 / 用户无感 |
| 37 | [37-design-rationale.md](./37-design-rationale.md) | 合理性五问强制；角色/流程/组件/功能总表 |

---

## L2 · 地图类（在 design 内）

| 编号 | 文档 | 内容 |
|---|---|---|
| 35 | [35-component-roles.md](./35-component-roles.md) | 组件角色职能 |
| 13 | [13-flows-and-diagrams.md](./13-flows-and-diagrams.md) | 流程与设计图总册 |
| 29 | [29-architecture-spec-catalog.md](./29-architecture-spec-catalog.md) | 规格目录：问题→解法 |

系统叙事总览在仓库 `[../architecture.md](../architecture.md)`（L2，不在本目录）。

---

## L3 · 领域细章（按域）

### A · 存储与账本

| 编号 | 文档 |
|---|---|
| 01 | [01-ledger.md](./01-ledger.md) |
| 03 | [03-autorecovery.md](./03-autorecovery.md) |
| 10 | [10-ledger-write-path.md](./10-ledger-write-path.md) |
| 11 | [11-ledger-recovery-deep.md](./11-ledger-recovery-deep.md) |
| 17 | [17-storage-vs-bookkeeper.md](./17-storage-vs-bookkeeper.md) |
| 19 | [19-open-ledger-integration.md](./19-open-ledger-integration.md) |
| 20 | [20-broker-openledger-contract.md](./20-broker-openledger-contract.md) |
| 24 | [24-offload.md](./24-offload.md) |

### B · 元数据与所有权

| 编号 | 文档 |
|---|---|
| 02 | [02-metadata.md](./02-metadata.md) |
| 05 | [05-routing-bundle.md](./05-routing-bundle.md) |
| 06 | [06-fencing-consistency.md](./06-fencing-consistency.md) |
| 09 | [09-broker-lifecycle.md](./09-broker-lifecycle.md) |
| 22 | [22-orphan-gc.md](./22-orphan-gc.md) |
| 27 | [27-dual-meta-ops.md](./27-dual-meta-ops.md) |

### C · 订阅与分发

| 编号 | 文档 |
|---|---|
| 04 | [04-cursor-subscription.md](./04-cursor-subscription.md) |
| 07 | [07-multitenancy.md](./07-multitenancy.md) |
| 18 | [18-dispatch-modes.md](./18-dispatch-modes.md) |

### D · 客户端与入口

| 编号 | 文档 |
|---|---|
| 08 | [08-client-backpressure.md](./08-client-backpressure.md) |
| 14 | [14-proxy.md](./14-proxy.md) |
| 21 | [21-wire-protocol.md](./21-wire-protocol.md) |
| 23 | [23-producer-idempotency.md](./23-producer-idempotency.md) |

### E · 可用性进阶

| 编号 | 文档 |
|---|---|
| 12 | [12-cross-failure-scenarios.md](./12-cross-failure-scenarios.md) |
| 15 | [15-availability-failover-window.md](./15-availability-failover-window.md) |
| 16 | [16-warm-standby.md](./16-warm-standby.md) |
| 26 | [26-standby-wire.md](./26-standby-wire.md) |

### F · 控制面

| 编号 | 文档 |
|---|---|
| 25 | [25-admin-api.md](./25-admin-api.md) |

---

## L4 · 工程门禁

| 编号 | 文档 | 角 |
|---|---|---|
| 28 | [28-design-audit-requirements.md](./28-design-audit-requirements.md) | 审计 |
| 30 | [30-engineering-quality.md](./30-engineering-quality.md) | 开发 |
| 31 | [31-test-strategy.md](./31-test-strategy.md) | 测试 |
| 32 | [32-operations.md](./32-operations.md) | 运维 |
| 33 | [33-customer-usage.md](./33-customer-usage.md) | 客户 |
| 36 | [36-production-grade-bar.md](./36-production-grade-bar.md) | 全角生产级 |

---

## L5 指针（正文不在 design/）

| 路径 | 内容 |
|---|---|
| [31-test-strategy.md](./31-test-strategy.md) | 测试五层与故障策略 |
| [../ops/](../ops/README.md) | Runbook |
| [../user/](../user/README.md) | 使用说明 |
| [../deploy/local-k8s.md](../deploy/local-k8s.md) | 本地 K8s |

---

## 编号速查（扁平，仅供跳转）

需要按文件名查找时用；**日常请走上方分层表**。

| 00–09 | 10–19 | 20–29 | 30–37 |
|---|---|---|---|
| 原则·Ledger·Meta·AR·Cursor·Bundle·Fence·租户·Client·生命周期 | 写路径·Recover·组合故障·**流程图**·Proxy·可用性·Standby·vsBK·分发·OL集成 | 契约·协议·Orphan·幂等·Offload·Admin·Standby线·双Meta·**审计**·**规格目录** | 开发四性·测试·运维·客户·**四目标**·**组件角色**·**生产级**·**合理性** |
