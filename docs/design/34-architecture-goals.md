# 架构目标：高可用 · 高并发 · 强数据一致性 · 故障/运维对用户无感

硬约束：[00-principles.md](./00-principles.md)。  
规格目录：[29-architecture-spec-catalog.md](./29-architecture-spec-catalog.md)。

**目的**：把产品侧四句目标落成**可验收的架构合同**——每条写清「靠什么机制」「量级直觉」「明确不承诺什么」。对话中的口号不以聊天为准，以本文 + 细章为准。

---

## 0. 四目标总览

| 目标 | 一句话合同 | 主要手段 | 对用户观感 |
|---|---|---|---|
| **高可用** | 单点进程故障后，L0–L2 自动续服；计划内变更近无感 | 无状态 Broker、租约/Fence、Drain、Warm Standby、AR、Proxy | 崩溃：短暂重试；Drain/扩缩：SDK 消化 |
| **高并发** | 加 Broker / Meta shard / Bookie / 分区 抬并行度，**不搬历史消息体** | Bundle 并行、Meta 分片、新 ledger 打新盘、Shared 消费 | 吞吐随规模升；热点靠拆分非搬盘 |
| **强数据一致性** | **已确认写入**与**单主排序**在声明档位上强硬；禁止双主分叉 | Qa Receipt、Ownership 线性化、Fence+Recover 尾唯一 | 不丢已 Receipt；切主后日志唯一 |
| **故障/运维对用户无感** | 应用默认路径**无需人工介入**；运维动作不要求业务改代码 | Proxy、SDK 重试/幂等、优雅 Unload、指标在运维侧 | 「无感」= 无人工值守，**不是零错误帧** |

四者冲突时的优先级（已定）：

```text
1. 不强行双主换「瞬时可用」—— 一致性/防分叉 > 掩盖切主窗口
2. 不搬历史消息体换「扩容完成感」—— 高并发靠并行度，不靠搬盘
3. 「无感」不得写成假 exactly-once 或零超时 —— 用户文档诚实（33 / user）
```

---

## 1. 高可用（HA）

### 1.1 定义

| 包含 | 不包含（已接受） |
|---|---|
| 单 Broker / 单 Bookie / 单 Meta 分片 leader 故障后自动恢复服务 | 整个 DC 全灭仍在线（属 L3 / DR） |
| 计划内滚动、扩缩、Unload 有优雅路径 | 拜占庭恶意节点 |
| 已 Receipt 在 Qa 假设下不因单盘丢失而丢 | 同时永久丢失 ≥Qa 副本仍不丢 |

### 1.2 机制地图

| 故障面 | 检测 | 隔离 | 收敛 | 续服 | 权威章 |
|---|---|---|---|---|---|
| Broker Owner 崩溃 | 租约/探活 | epoch 拒旧主 | Acquire+Fence+Recover | Ready 后再写 | 06、09、11、15 |
| 计划内下线 | Drain | 停新连 | Unload CAS | 他者 Acquire | 09、15、32 |
| 热备 | 早探测谓词 | Fence | Promote≠等满 TTL | 缩短窗口 | 16、26 |
| Bookie | underreplicated | 摘候选 | AutoRecovery | 新写可用新盘 | 03、11 |
| Meta 分片 | 探活/ErrRetry | 禁假装切主完成 | 法定人数恢复 | Lookup/租约恢复 | 02、27、32 |
| 客户端可达 | Proxy/SDK | NotOwner 迁连 | Lookup | 重试 | 14、08、21 |

自愈分层必须停在 L0–L2（[00 §2](./00-principles.md)）；日常依赖人工搬盘 = 设计失败。

### 1.3 可用性直觉（设计约束，非压测数字）

| 场景 | 目标观感 |
|---|---|
| Drain / 滚动 | Client 近无感或亚秒～数秒重试 |
| Owner 崩溃（冷切） | 秒～数十秒不可写该 bundle；自动恢复 |
| Warm Standby Promote | 显著短于冷切（M3） |
| Bookie 单挂（Qa 仍满足） | 写继续；AR 后台修 |

验收：故障矩阵 C* + 演练 O*（[31](./31-test-strategy.md) / [32](./32-operations.md)）。

---

## 2. 高并发（Scale / Throughput）

### 2.1 定义

**并发度随资源近似水平扩展**：异 bundle 生产、异 key Meta Cas、异 ledger 写入、Shared 多消费者——加节点后并行度上升。  
**禁止**把「搬迁历史消息体」当作扩容必经路径（[00 §1](./00-principles.md)、architecture §6）。

### 2.2 四面并行

| 面 | 如何抬并发 | 不做什么 |
|---|---|---|
| **Broker** | 加节点 + Unload 迁 bundle；多 bundle 多 writer | 在 Broker 盘做消息权威 |
| **Meta** | **加 shard** 抬异 key Cas；cursor 合并刷 | 只堆单 shard 从库当写扩容 |
| **Bookie** | 注册新节点；**新** ledger/fragment 打上去 | 扩容强制搬 CLOSED 历史 |
| **消费** | Shared / Key_Shared / 加分区 | 单 Exclusive 幻想无限水平消费 |

热点：拆 partition / split bundle / 业务打散 key——**不是**搬盘。LoadManager 与密度上限见 [05](./05-routing-bundle.md)。

### 2.3 性能配套（实现门禁）

开发侧：[30](./30-engineering-quality.md)（批量、零多余 Meta/条、背压、无热路径锁放大）。  
测试侧：压力层 S* 建基线，不是「打挂即过」。

### 2.4 不承诺

- 单 partition / 单 bundle 内写并发无限（单 writer 排序上限）。  
- Meta 单 key 写 QPS 随机器数线性（单 key 串行 CAS）。  
- 跨分区全局有序的同时无限并行。

---

## 3. 强数据一致性（Consistency）

### 3.1 「强」落在哪——分对象，禁止口号化

本系统**不是**「所有读在任意时刻全局线性化」。强一致按对象分档（扩展 [00 §6](./00-principles.md)）：

| 对象 | 一致性合同 | 机制 |
|---|---|---|
| **已 Receipt 的 Produce** | 耐久：在 Qa 副本；容忍丢 Qa−1 盘不丢已确认 | Receipt 仅 Qa 后；Journal fsync 假设 |
| **Ownership** | **线性化**：至多一主；旧 epoch 写失败 | Meta CAS + 租约 + epoch |
| **Ledger 尾** | Recover 后全局同一 `lastEntryId`；禁止双主分叉日志 | Fence → OL Recover → 新 ledger |
| **同 Owner 下的消息序** | 单 writer 全序；LAC 连续释放 | 06、10 |
| **幂等（开启后）** | M2 同 Owner；M3 跨切主——阶段诚实 | 23、33 |
| **Cursor** | 持久位点；崩溃允许重复投递（at-least-once） | 04 |
| **Lookup 缓存** | **允许短暂陈旧**；用 NotOwner 纠正 | 14、08 |

### 3.2 硬禁令（违反 = Critical）

1. 未 Fence/Recover 完成对外成功写。  
2. 双 Owner 同时对同一 bundle 成功 Append。  
3. 跨 OM/OL 幻想单 CAS；滚本必须三步 + 补偿（[02](./02-metadata.md)）。  
4. 用 entryId 跨滚本冒充业务去重（须 producer 幂等）。  
5. 对外宣称 exactly-once 超过里程碑/未开幂等（[33](./33-customer-usage.md)）。

### 3.3 与「高可用」的关系

切主窗口存在**正是为了**保持 Ownership 线性化与尾唯一；用双主换零超时 = 放弃本条目标。  
「强一致」优先于「瞬时人人可写」。

---

## 4. 故障与运维对用户无感（User-transparent）

### 4.1 「无感」的精确定义

| 是 | 不是 |
|---|---|
| 应用**默认不靠人工**处理 Broker 滚动、扩缩、普通切主 | 应用进程日志里永远零错误 |
| 运维动作有 Drain/Unload；Client/SDK **自动重试 + Lookup** | 无需 SDK、裸连且零超时 |
| 故障由集群自愈 + 运维 runbook；业务继续用同一 service URL | 业务去 SSH/改 Owner IP |
| 用户文档只要求：重试、幂等阶段、选订阅模式 | 用户去读 Fence/Orphan 内部章 |

**诚实边界**（必须进 [user/](../user/README.md)）：

- 崩溃切主：**允许**短暂 `NotOwner` / 超时；无感 = **自动恢复且无需人工改应用**。  
- 计划内变更：目标「近无感」。  
- 重复：默认至少一次；精确一次按幂等阶段，禁止假宣传。

### 4.2 无感机制栈

```text
运维侧：Drain → Unload → 滚动 / 扩缩 / Promote     （32、ops）
集群侧：租约、Fence、Recover、AR、对账              （06、11、03、27）
入口侧：Proxy 统一 URL、缓存失效重 Lookup           （14）
应用侧：SDK 默认可重试 + jitter；幂等可选           （08、21、23、33）
```

任一层缺失 ⇒ 不得宣称「故障/运维对用户无感」：

| 缺失 | 后果 |
|---|---|
| 无 Drain 只杀进程 | 用户常感知崩溃窗 |
| 无 SDK 重试 | 业务必须自写容错才「无感」 |
| 无 Proxy | 地址风暴，运维扩 Broker 用户有感 |
| 文档假 exactly-once | 「无感」变成语义欺诈 |

### 4.3 运维有感、用户无感

运维**必须有感**（告警、runbook、演练）；用户**尽量无感**。  
二者分工见 [32](./32-operations.md) 与 [33](./33-customer-usage.md)：告警链手册在 ops；重试指引在 user。

---

## 5. 目标 ↔ 细章索引

| 目标 | 必读细章 |
|---|---|
| 高可用 | 00、03、09、12、15、16、27、32 |
| 高并发 | 00 §5、02、05、architecture §5–§6、08、30 |
| 强数据一致性 | 00 §6、06、10、11、19、20、23 |
| 用户无感 | 14、15、08、21、23、33、user/、32（运维对照）；流程 §18 见 [13](./13-flows-and-diagrams.md) |
| **全部流程/设计图** | [13-flows-and-diagrams.md](./13-flows-and-diagrams.md)（总册 + §19 跨文档索引） |
| **组件角色职能** | [35-component-roles.md](./35-component-roles.md) |

---

## 6. 架构师自检（改设计时）

```text
[ ] 该改动是否削弱四目标之一？若是，文中写明权衡与传播章
[ ] 是否用双主/跳过 Fence 换「更无感」？→ 禁止
[ ] 扩容是否暗含搬历史消息体？→ 禁止
[ ] 对外措辞是否超过 00§6 / 23 / 33？→ 禁止
[ ] 故障是否仍停在 L0–L2？L3 是否仅灾难？
[ ] 开发 30 / 测试 31 / 运维 32 / 客户 33 是否有对应验收钩子？
```

---

## 7. 相关文档

| 文档 | 用途 |
|---|---|
| [00](./00-principles.md) | 硬约束与检查清单 |
| [../architecture.md](../architecture.md) | 总览与交付范围 |
| [29](./29-architecture-spec-catalog.md) | 问题→解法目录 |
| [30](./30-engineering-quality.md) | 实现四性 |
| [15](./15-availability-failover-window.md) / [16](./16-warm-standby.md) | 可用性窗口 |
| [32](./32-operations.md) / [33](./33-customer-usage.md) | 运维有感 / 用户无感 |
