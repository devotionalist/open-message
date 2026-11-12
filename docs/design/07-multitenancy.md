# 多租户 / 配额 / 隔离 细章

主文档：§18。通则：[00-principles.md](./00-principles.md)。

## 1. 为何合理

- 策略落在 Namespace → 避免按 topic 人工配置爆炸。  
- 配额在 Broker 强制 → 坏邻居不能拖死共享 Meta/Bookie。  
- 隔离组 → 强租户可独占计算/存储池，同时仍享账本模型扩容红利。

## 2. 模型

```text
Tenant
  policies: adminRoles, allowedClusters, resourceQuota?
  Namespace
    policies: retention, backlogQuota, persistence(E,Qw,Qa),
              publishRate, dispatchRate, ttl,
              auth(produce/consume roles),
              brokerIsolationGroup?, bookieAffinityGroup?
    Bundle* → Topics / Partitions
```

策略解析：Topic 未覆盖字段 → 继承 Namespace → 集群默认。

## 3. 配额与行为

| 策略 | 度量 | 超额 |
|---|---|---|
| backlog quota (size/time) | 订阅积压 | hold / exception / 丢最旧 backlog（谨慎） |
| publish rate | msg/s, byte/s | 拒 Send 或延迟 |
| dispatch rate | 分发速率 | 减慢 Dispatcher |
| retention | 已确认保留 | 到期可 trim ledger |
| persistence | 新 ledger 的 E/Qw/Qa | 创建时生效 |

**自愈**：超配额不能卡死 Meta；拒绝在 Broker 数据面完成，并打度量。

## 4. 隔离

| 类型 | 机制 | 故障域 |
|---|---|---|
| Broker 组 | Acquire 仅允许组内 broker | 计算邻干扰 |
| Bookie 亲和 | 新 ensemble 只从组内选 | 存储邻干扰 |

扩容：向组内加 broker/bookie 即可；**不跨组搬历史 ledger**（组扩容同样走新 ledger）。

### 4.1 缩容 / 容量护栏（已定）

配置键（Namespace 或集群默认；实现名可变，**语义不可缺**）：

| 键 | 含义 | 默认（设计） |
|---|---|---|
| `min_brokers_in_isolation_group` | 组内允许服务的最少 Broker | `2`（单组演示可配 `1`，**不得**标生产 HA） |
| `min_bookies_in_isolation_group` | 组内可选 ensemble 的最少 Bookie | `max(Qa, 2)` |

**执法**：

1. Admin/LoadManager **缩容**或摘节点前：若操作后存活 &lt; min → **拒绝**并告警。  
2. Acquire：组内无足够 Ready Broker 时不得把 owner 派到组外（除非显式 `failover-to-default`，默认关）。  
3. 新 ledger：Client 选 ensemble 时组内候选 &lt; `min_bookies` 或 &lt; Qa → `Unavailable`，不降级偷用组外盘（默认）。  

与 [32](./32-operations.md) 缩容护栏、ops `scale-in` runbook 对齐。

## 5. 故障与自愈

| 故障 | 自愈 |
|---|---|
| 租户打满 backlog | 按策略拒写/驱赶；告警；不拖垮他租户（需隔离或限流） |
| 隔离组内 broker 全挂 | 若允许 failover-to-default 则降级（**默认否**，避免穿透）；否则该 NS 只读/不可用直至组内恢复 |
| 策略 Meta 不可读 | Broker 使用上次缓存策略 + 保守默认限流；禁止「无限制放行」 |

## 6. 扩容

- 租户增多：Namespace/Bundle 元数据走 Meta 分片。  
- 单租户流量升：加 partition + 加 bundle + 组内加节点。  
- ResourceGroup（可选，M3）：跨 namespace 聚合配额账本。

## 7. 不变量

1. 鉴权失败不得 Produce/Consume。  
2. 隔离组外节点不得成为该 NS bundle owner（Acquire 校验）。  
3. 配额拒绝不破坏已确认消息耐久。  

## 8. 阶段

- M1：tenant/ns 字符串 + 默认 persistence/retention 结构。  
- M2：backlog + rate + 基础 auth。  
- M3：isolation 组 + resource group。
