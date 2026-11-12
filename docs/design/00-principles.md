# 设计通则：合理 · 故障自愈 · 可扩容

本文约束所有细章。实现与评审时，任一组件若违反通则，视为设计缺陷。

**交付**：凡写入 `docs/design/*` 的能力均须落地；只按里程碑分期，不以「非目标 / 可选」裁掉。

**产品架构四目标**（高可用 · 高并发 · 强数据一致性 · 故障/运维对用户无感）的合同、机制地图与「不承诺」见 **[34-architecture-goals.md](./34-architecture-goals.md)**。本通则提供硬约束与检查清单；34 提供目标层验收口径。冲突时：**一致性/防分叉 > 掩盖切主窗口**；**不搬历史消息体 > 扩容完成感**；**诚实语义 > 无感口号**。

**合理性强制**：角色 / 流程 / 组件 / 功能设计均须有合理性说明（问题、选型、放弃方案、代价、验收），见 **[37-design-rationale.md](./37-design-rationale.md)**。无说明不得冻结。

## 1. 三条硬约束

| 约束 | 含义 | 反例（禁止） |
|---|---|---|
| **存算分离** | 消息体只在 LedgerStore；Broker 可丢可迁 | Broker 本地消息 WAL 当权威 |
| **故障可自愈** | 单点进程宕机后，集群在有限步骤内自动恢复服务与一致性，无需人工搬数据 | 宕机后必须手工拷贝 ledger 目录 |
| **在线可扩容** | 加 Broker / Meta shard / Bookie 不中断；**不把搬历史消息体当必经路径** | 扩容强制 rebalance 全量副本 |

## 2. 自愈分层

```text
L0 进程内    重试、超时、背压、连接重建
L1 组件内    Fence/Recover、CAS 冲突重读、租约过期抢主
L2 集群内    Ownership 迁移、Meta 分片迁移重试、AutoRecovery 补副本
L3 人工     仅：数据中心级灾难、Bug 导致的元数据腐坏修复工具
```

设计要求：常见故障停在 **L0–L2**；L3 必须有 runbook，但不能是日常依赖。

## 3. 故障模型（设计时默认）

假设并必须处理：

1. Broker 进程崩溃 / OOM / 网络隔离  
2. Bookie 进程崩溃、磁盘只读、慢盘  
3. Metadata 单分片 leader 切换、短暂不可用  
4. 网络：丢包、延迟、半开连接  
5. 时钟：允许漂移，**租约与超时用逻辑时间/服务端时间**，不依赖客户端墙钟强同步  
6. 旧主「诈尸」：分区恢复后旧 owner / 旧 writer 继续发请求 → 必须被 epoch/fence 拒绝  

暂不要求（可后续章）：拜占庭恶意节点、跨洲强一致同步复制。

## 4. 自愈设计检查清单（每组件必答）

1. **故障检测**：谁发现？超时 / 心跳 / 审计？  
2. **隔离**：如何阻止旧角色继续写成功？  
3. **收敛**：如何得到唯一权威状态（CAS / fence / recover）？  
4. **续服**：新角色如何恢复服务？RTO 量级预期？  
5. **数据**：是否丢已 ack 消息？是否可能重复？（at-least-once / exactly 边界）  
6. **风暴**：恢复是否会造成重试/补副本雪崩？有无 rate limit / delay？  

## 5. 扩容设计检查清单（每组件必答）

1. **加节点后并行度如何升**？（异 key / 新 ledger / 新 bundle）  
2. **历史数据搬不搬**？默认不搬消息体。  
3. **迁移窗口客户端行为**？`ErrRetry` / redirect / 阻塞上限。  
4. **热点**：单 key / 单 ledger / 单 bundle 过热怎么办？（拆分逻辑分区，非搬盘）  
5. **元数据放大**：分片/bundle 数量上限与治理。  

## 6. 一致性默认档位

| 对象 | 一致性 |
|---|---|
| 已 ack 的 Produce | 在 `Qa` 副本持久；容忍 `Qa-1` 个 bookie 永久丢失不丢**已确认** |
| Ownership | 线性化 CAS；至多一主 |
| Ledger 尾部 | Recovery 后全局同一 `lastEntryId` |
| Cursor | 持久位点；崩溃窗口允许重复投递 |
| Lookup 缓存 | 允许短暂陈旧 + NotOwner 纠正 |

## 7. 文档映射（分层；勿按编号当阅读序）

**总导航**：[../README.md](../README.md) · **组织规则**：[../STRUCTURE.md](../STRUCTURE.md) · **design 分层目录**：[README.md](./README.md)。

| 层 | 本目录要点 |
|---|---|
| **L1** | 本文 · [34](./34-architecture-goals.md) · [37](./37-design-rationale.md) |
| **L2** | [../architecture.md](../architecture.md) · [35](./35-component-roles.md) · [13](./13-flows-and-diagrams.md) · [29](./29-architecture-spec-catalog.md) |
| **L3-A 存储** | [01](./01-ledger.md) [03](./03-autorecovery.md) [10](./10-ledger-write-path.md) [11](./11-ledger-recovery-deep.md) [17](./17-storage-vs-bookkeeper.md) [19](./19-open-ledger-integration.md) [20](./20-broker-openledger-contract.md) [24](./24-offload.md) |
| **L3-B 元数据/所有权** | [02](./02-metadata.md) [05](./05-routing-bundle.md) [06](./06-fencing-consistency.md) [09](./09-broker-lifecycle.md) [22](./22-orphan-gc.md) [27](./27-dual-meta-ops.md) |
| **L3-C 订阅分发** | [04](./04-cursor-subscription.md) [07](./07-multitenancy.md) [18](./18-dispatch-modes.md) |
| **L3-D 客户端入口** | [08](./08-client-backpressure.md) [14](./14-proxy.md) [21](./21-wire-protocol.md) [23](./23-producer-idempotency.md) |
| **L3-E 可用性进阶** | [12](./12-cross-failure-scenarios.md) [15](./15-availability-failover-window.md) [16](./16-warm-standby.md) [26](./26-standby-wire.md) |
| **L3-F 控制面** | [25](./25-admin-api.md) |
| **L4 门禁** | [28](./28-design-audit-requirements.md) [30](./30-engineering-quality.md) [31](./31-test-strategy.md) [32](./32-operations.md) [33](./33-customer-usage.md) [36](./36-production-grade-bar.md) |
| **L5 说明** | [../ops/](../ops/README.md) · [../user/](../user/README.md) · [../deploy/local-k8s.md](../deploy/local-k8s.md) |
