# 开发工程品质：高性能 · 高可用 · 高鲁棒 · 高复用

设计规格：[29-architecture-spec-catalog.md](./29-architecture-spec-catalog.md)。  
本文件规定：**实现时如何保证四性**；违反视为实现缺陷或设计缺口（不得静默牺牲正确性换「快」）。

---

## 0. 总原则

| 原则 | 含义 |
|---|---|
| **正确性优先于微优化** | 禁止跳过 Fence / 未达 Qa 就 Receipt / 双写 Meta |
| **四性来自架构，代码兑现** | 性能靠无锁热路径与分片；可用靠 Ownership+重试；鲁棒靠状态机与补偿；复用靠边界清晰的包 |
| **可测才算交付** | 无单测/无故障断言的路径不算完成 |
| **可观测才算可运维** | 关键路径必须有指标与错误分类 |

---

## 1. 高性能（High Performance）

### 1.1 目标（设计已定的天花板）

- Meta 写 QPS ≪ 消息 QPS（owner 缓存 + cursor **合并刷盘**）。  
- 单 OPEN ledger：**单 writer**；吞吐受 1 Broker 核 + Qw journal 限制；水平扩展靠 **partition × bundle**。  
- Append：并行写 Qw、等 Qa；pipeline 允许多 in-flight，**LAC/Receipt 连续释放**。  
- 读：EntryCache 优先；禁止热路径全表扫 Meta。

### 1.2 实现必须做

| 做法 | 禁止 |
|---|---|
| 热路径零 Meta Cas（Produce 不写 Ownership） | 每条消息 Cas Cursor / Ownership |
| cursor 合并刷：时间/条数/字节阈值 | 每条 Ack 同步 Cas |
| 连接池复用 Bookie/Meta；有界队列背压 | 无限 goroutine / 无界 channel |
| 对象池谨慎用于热路径（payload 缓冲）；度量后再加 | 过早微优化、模糊所有权 |
| Proxy Lookup **singleflight** + 缓存 | 切主时惊群打爆 Meta |
| 批量/pipeline 与设计 LAC 语义一致 | 乱序提前 Receipt |

### 1.3 验收（开发自证）

- 基准：单 partition Produce 延迟/吞吐；cursor 刷盘 QPS vs 消息 QPS 比值有记录。  
- 回归：加负载时 Meta Cas QPS 不随消息线性涨（合并刷生效）。  
- 单测：未达 Qa 不推进 LAC；pipeline 空洞不提前释放更高 seq。

---

## 2. 高可用（High Availability）

### 2.1 目标

- 优雅 Unload/Drain：业务近无感（配合 Proxy/SDK 重试）。  
- 崩溃：窗口可观测；Ready 前拒写；已 Receipt 不丢（Qa 假设下）。  
- 非 Owner 宕机不影响该 bundle。  
- 控制面：LoadManager 密度、双 Meta 对账（按里程碑）。

### 2.2 实现必须做

| 做法 | 禁止 |
|---|---|
| 状态机：`Loading → Recovering → Ready → Unloading`；非 Ready 拒成功写 | 半初始化就对外 Receipt |
| Renew 失败 / epoch 不匹配 → 本地立即停写 | 墙钟未过仍硬写 |
| 错误分类：可重试 vs 不可重试（对齐 [21](./21-wire-protocol.md)） | 一律 Internal |
| Fence+Recover 完成前不开新写（切主）；同主遇 Fenced → 迷你滚本（[03](./03-autorecovery.md)） | 忽略 Fenced 盲重试同本 |
| Drain：拒新连 → 排空 → Close/flush → Unload CAS | 滚动升级默认 kill -9 |
| 指标：`failover_duration`、`not_owner`、`lease_renew_fail` | 无指标宣称 HA |

### 2.3 验收

- 单测/集成：Unload 后旧节点 NotOwner；切主后已 Receipt 可读。  
- 演练：对照 [31](./31-test-strategy.md) 故障层与 [32](./32-operations.md) 演练要求。  
- 不把 M1 `Qa=1` 标成生产 HA。

---

## 3. 高鲁棒性（High Robustness）

### 3.1 目标

- 常见故障停在 L0–L2（[00](./00-principles.md)）。  
- 跨系统步骤可补偿、可幂等重试。  
- 无静默丢已确认；不确定写可重试（幂等按 [23](./23-producer-idempotency.md) 阶段）。

### 3.2 实现必须做

| 做法 | 禁止 |
|---|---|
| 显式状态机 + 穷尽错误分支（表驱动测试） | 深嵌套「差不多对」 |
| 滚本三步每步可重入；Cas 冲突重读；MarkOrphan 永久放弃时调用 | 假装跨 OM/OL 事务成功 |
| 超时、上下文取消、有界重试 + jitter | 热路径 tight loop 无退避 |
| 时钟：租约用 Meta 服务端时间 | 用 Broker 墙钟判 lease |
| 资源：连接/文件/锁 defer 释放；panic 隔离在边界 | 热路径 panic 拖垮进程无恢复策略 |
| 输入校验：帧长、topic 名、seq 窗口 | 信任客户端任意长度 |

### 3.3 验收

- 单测必覆盖：CAS 冲突、Fence 后 Append、滚本 2/3 失败、租约过期、双 Acquire。  
- 故障注入：Bookie 超时、Meta ErrRetry、杀 Owner（按阶段）。  
- 静态：race detector（Go）、关键路径 lint。

---

## 4. 高复用性（High Reusability）

### 4.1 目标

- 包边界对齐设计权责：换 Meta 后端、换 OL Client、换 Proxy 传输不重写业务。  
- 无「复制粘贴」第二套 Recover/AR/LedgerMeta。  
- SDK 与 Broker 共享错误语义与协议常量。

### 4.2 实现必须做

| 分层 | 职责 | 禁止依赖 |
|---|---|---|
| `internal/metadata` | Store 接口；Oxia/OpenKV 适配 | Bookie、业务 Dispatcher |
| `internal/ledger` | **仅** OL Client 适配 + 错误映射 | 自实现 quorum/AR |
| `internal/topic` | ManagedLedger 编排（滚本三步、Index Cas） | 直连拼装 ensemble |
| `internal/broker` | Ownership、Lookup、Dispatcher | 持久消息 WAL |
| `internal/client` / SDK | 重试、pending、permits、协议编解码 | 集群内部 Admin 键 |
| `cmd/*` | 组装与配置 | 业务逻辑 |

接口优先：`LedgerStore`、`MetaStore`、`Clock`（可注入）、`IdGenerator`（测双主）。  
配置与策略（E/Qw/Qa、刷盘间隔）外置；默认值安全（偏正确）。

### 4.3 验收

- 内存 Fake Meta / Fake OL Client 可跑通 Produce-Consume 单测。  
- 切换 `METADATA_BACKEND` 不改 topic 包。  
- 无本仓 `led/` LedgerMeta 键、无 `cmd/bookie`。

---

## 5. PR / 功能完成检查单（开发必填）

```text
功能 ID / 设计章：
[ ] 硬禁令（developer.md §4）未违反
[ ] 性能：热路径无每消息 Meta Cas；背压有界
[ ] 可用：非 Ready 拒写；错误可分类；关键指标已加
[ ] 鲁棒：失败补偿/重试有单测；无裸无限重试
[ ] 复用：依赖方向符合 §4.2；可注入 Fake 测
[ ] 幂等/语义承诺未超过里程碑文档
[ ] 偏离已开单或已改设计
```

---

## 6. 与测试、运维、客户

| 角色 | 如何卡住「假四性」 |
|---|---|
| 测试 | 无断言的性能/HA 声称不通过；C* 场景缺失 |
| 运维 | 无 Drain/指标/Admin 的 HA 叙事降级；对照 [32](./32-operations.md) |
| 客户 | 对外文档与错误不可操作 → 体验单 |
| 审计 | 实现违反设计不变量 → 实现违规或改设计再审 |

---

## 7. 相关文档

| 文档 | 用途 |
|---|---|
| [10](./10-ledger-write-path.md) | 写路径性能与 LAC |
| [08](./08-client-backpressure.md) | 背压 |
| [15](./15-availability-failover-window.md) | 可用性 |
| [00](./00-principles.md) | 自愈分层 |
| [20](./20-broker-openledger-contract.md) | 复用边界（OL Client） |
| [31-test-strategy.md](./31-test-strategy.md) | 单测与故障策略 |
| [32](./32-operations.md) | 运维规格；指标/Drain 门禁 |
