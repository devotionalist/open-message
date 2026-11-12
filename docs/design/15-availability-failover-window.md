# 可用性：Broker 宕机/重启时的不可用窗口

关联：[06-fencing-consistency.md](./06-fencing-consistency.md)、[09-broker-lifecycle.md](./09-broker-lifecycle.md)、[14-proxy.md](./14-proxy.md)、[08-client-backpressure.md](./08-client-backpressure.md)。  
合理性模板：[37](./37-design-rationale.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 单 writer 模型下 Owner 崩溃必有不可用窗；「无状态」≠ 零错误 |
| 选型 | 承认窗口 + Drain/重试/幂等压缩；M3 Standby 再缩 |
| 放弃 | 双主换瞬时可用；对外假称零超时 |
| 代价 | 崩溃时秒～数十秒失败；应用须可重试 |
| 验收 | 15 场景表；用户文档诚实（33/user） |

## 1. 问题陈述（承认）

在 **单 bundle 单 writer（owner）** 模型下：

> **Owner Broker 崩溃或非优雅重启时，该 bundle 上的 Produce/Consume 会有一段时间失败**（超时、`NotOwner`、连接断开），直到新 Owner 完成 Acquire + Fence/Recover + Ready。

这不是实现疏忽，而是与「单写者排序 + fencing 防双主」绑定的代价。Kafka partition leader 切换、Pulsar bundle unload 同样有类似窗口。

**不可接受的误解**：以为「Broker 无状态」=「任意节点瞬时接管且 Client 零报错」。无状态消除的是**搬数据**，不消除**切主收敛时间**。

## 2. 窗口从哪来

```text
T0  Owner 死亡/重启杀进程
T1  Client 连接断 / 飞中请求失败          ← 已开始「不可用」
T2  leaseExpire（未续约）                ← 可被其它节点抢主
T3  新 Owner Acquire
T4  Fence + Recover 旧 OPEN ledger       ← 禁止此时对 Client 成功写
T5  开新 ledger + 加载 cursor
T6  Ready                                ← 不可用窗口结束
```

粗算：`窗口 ≈ 探测/租约剩余 + Recover 耗时 + 加载`。  
租约越长，崩溃后等待越久；租约越短，越容易误切主（网络抖动）。

## 3. 分类：计划内 vs 计划外

| 类型 | 表现 | 设计目标 |
|---|---|---|
| **计划内重启** | 先 Drain/Unload 再停进程 | **Client 几乎无感知或仅极短重试**（见 §4） |
| **崩溃 / kill -9** | 必有失败窗口 | **可重试错误 + 自动恢复**；压缩窗口；业务不崩 |

把「滚动发布」做成计划内路径，是运维上把问题从「经常报错」降成「偶发故障」。

## 4. 缓解手段（已定进架构）

### 4.1 计划内：优雅移交（必须支持）

```mermaid
sequenceDiagram
  participant Ops
  participant Old as OldOwner
  participant New as NewOwner
  participant C as Client_or_Proxy

  Ops->>Old: Drain
  Old->>Old: reject new conn; finish inflight
  Old->>Old: Close ledger + flush cursor
  Old->>New: Unload then New Acquire (or New acquires after Unload)
  Note over C: Proxy retries same request to new owner
  Ops->>Old: process exit
```

- **重启 Broker 禁止**「直接杀进程当常态」。  
- 滚动升级：`Drain → Unload → 停进程 → 起新进程`。  
- 目标：计划内切主 **无长窗口 Recover**（正常 Close，不必走 crash Recover）。

### 4.2 计划外：缩短与消化窗口

| 手段 | 作用 | 代价 |
|---|---|---|
| **合理租约**（如 TTL 10–15s，renew 2–3s） | 平衡误切与故障发现 | 过短易抖动 |
| **Client/Proxy 自动重试** | 报错不传到业务致命层；幂等后可安全重发 | 要区分可重试错误 |
| **Proxy 消化切主** | Client 只见短暂延迟/重试，不见 Broker 地址风暴 | 多一跳 |
| **Ready 前不接写** | 避免双写；窗口更「干净」 | 窗口内明确失败 |
| **生产幂等 M2** | **同 Owner** 超时/重绑重发不重复；跨崩溃切主精确一次见 **M3**（[23](./23-producer-idempotency.md)） | M2 实现成本；M3 Meta 表 |
| **限制单 Broker 上 bundle 过密**（**M2 LoadManager**） | 一台挂掉影响面可控 | 要足够 Broker/bundle |
| **故障探测早于满 TTL**（C / Standby Promote） | 缩短 T0→T3 | 需抖动抑制；见 [16](./16-warm-standby.md) §3.1 |

### 4.3 架构禁止（约束，不是交付裁剪）

| 禁止项 | 原因 |
|---|---|
| 多活同时写同一 partition | 破坏 single-writer 顺序，或变成另一套冲突解决 |
| 无条件任意 Broker 接写 | 等价取消 owner，账本 fencing 模型要重做 |
| 宣称零探测成本的「零停机崩溃切换」 | 物理上需要探测 + 隔离旧主；目标是压缩窗口 + 可重试，不是假零窗口 |

## 5. 对 Client 的错误契约（避免「业务不可用」）

应用层应区分：

| 错误 | 含义 | 期望行为 |
|---|---|---|
| 可重试：`Timeout` / `NotOwner` / `Unavailable` / 连接断开 | 切主窗口 | 退避重试；成功则业务继续 |
| 不可重试：鉴权失败、Topic 不存在、配额策略拒绝 | 配置/权限问题 | 失败上抛 |

**SDK 默认**：对可重试错误做有限次重试（带 jitter），使**短暂 Broker 故障表现为延迟尖刺，而不是业务进程崩溃**。  
「客户端报错」若指 SDK 日志里的重试，可接受；若指业务 API 同步失败且无重试，则是 **Client 用法/SDK 未达标**，要在 [08](./08-client-backpressure.md) 补强。

## 6. 可用性目标（设计指标，非承诺 SLA 数字）

| 场景 | 目标 |
|---|---|
| 优雅 Unload/滚动重启 | 成功请求 P99 短暂上升；业务错误率 ≈ 0（靠重试） |
| 单 Owner 崩溃 | 该 bundle 自动恢复；窗口可观测（指标：`owner_failover_seconds`） |
| 非 Owner Broker 挂 | **该 bundle 无影响**（无状态 + 分片归属） |

## 7. 监控与告警

- `bundle_owner_failover_total` / `failover_duration_seconds`  
- `produce_error_not_owner` / `client_retries`  
- `lease_expire_takeover`  
- 滚动发布期间 failover 次数应来自 **Unload** 而非 **crash**

## 8. 有哪些设计能「解决」——分层方案

先定预期：**单 writer 模型做不到「进程已死、客户端同步调用仍 100% 成功且零重试」**。  
业界（Pulsar / Kafka）通常做的是：**压缩窗口 + 把失败变成可自动重试的延迟**，让业务表现为「抖一下」而不是「挂一会」。

### 8.1 方案对比

| 方案 | 能解决到什么程度 | 代价 | 是否适配本架构 |
|---|---|---|---|
| **A. 优雅 Drain/Unload** | 计划内重启 ≈ 业务无感 | 发布流程要规范 | **必做** |
| **B. Proxy + SDK 自动重试 + 幂等** | 窗口变延迟；M2 保同 Owner 重试；跨崩溃去重 M3 | SDK/幂等 | **必做（生产）** |
| **C. 更快故障探测**（φ/心跳/Renew 滞后，**不必等满 TTL**） | 缩短 T0→T3；热备 Promote 依赖此项 | 误切风险，需抖动抑制 | **必做（与热备一并验收）** |
| **D. 限制 Recover 成本**（定期滚本、限制 OPEN 尾部） | 缩短 T4 | 略增滚本 Meta 写 | **应做** |
| **E. Warm Standby（热备 Broker）** | 大幅缩短 T4–T5（状态已预热） | 多一倍内存/连接；要同步软状态 | **必做（M3）** |
| **F. 读写分离：Follower 只读** | 崩溃时消费可继续，生产仍窗口 | 读可能略旧；实现复杂 | **纳入演进（M3+）** |
| **G. 多活写 / Leaderless（如 S3 主存）** | 更接近任意节点接写 | 延迟↑或顺序/冲突模型大变 | **与当前账本单 writer 模型互斥，不采用** |

### 8.2 交付组合（本项目已定）

```text
【阶段一 · M1–M2 · 对齐 Pulsar】
  A 优雅 Drain/Unload（计划内重启）
  B Proxy + SDK 自动重试 + 生产幂等
  C 合理租约 / 故障探测
  D 定期滚本，限制 Recover 尾部

【阶段二 · M3 · 必做】
  E Warm Standby（热点 bundle）→ [16-warm-standby.md](./16-warm-standby.md)
  F Follower 只读（可与 Standby 同轨演进）

【模型边界】
  G 多活写 / Leaderless —— 不采用（与账本方案互斥）
```

**原则：阶段一未达标前不开启阶段二；阶段二仍在总交付内，不是砍项。**

### 8.3 方案 E：Warm Standby

完整设计见 **[16-warm-standby.md](./16-warm-standby.md)**（同步白名单、SoftStateStream、Promote+Fence、Cursor 校准、阶段与验收）。

要点：Primary 推送软状态 → Standby 预热；崩溃时 Promote + **仍必须 Fence** + 短路径 Recover；无 Standby 时退回冷切主。

### 8.4 对业务怎么讲

| 话术 | 是否诚实 |
|---|---|
| 「Broker 挂了客户端一段时间报错」 | 对，若 SDK 不做重试 |
| 「我们保证崩溃切换业务零失败」 | 单写主下不诚实 |
| 「滚动升级业务无感；崩溃靠重试自动恢复，窗口可观测可压缩」 | **应对齐的目标** |

## 9. 结论

1. **有问题感是对的**：崩溃切主期间该 bundle **确实会短暂失败/重试**。  
2. **架构仍成立**：代价换的是顺序写、防双主、扩容不搬数据。  
3. **好的设计不是装零窗口，而是**：A+B+C+D 做扎实，再用 **E 热备**把崩溃窗口压到很小。  
4. 真要「任意节点写、崩溃客户端无失败」，走 **G 类模型**，与当前 BK 账本 + owner 方案分开立项。