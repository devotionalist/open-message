# 客户使用规格：对外说明必须写清什么

用户可读落盘：[../user/README.md](../user/README.md)。  
内部设计（客户**不应**被要求先读）：08 / 14 / 15 / 18 / 21 / 23。

**目的**：保证业务侧只凭用户文档 + SDK 能集成；切主/重试/幂等承诺诚实；禁止把内部章当使用说明。

---

## 1. 文档分层

| 层 | 给谁 | 落点 | 禁止 |
|---|---|---|---|
| **使用说明** | 应用开发者 | `docs/user/*` | 依赖 Bundle epoch、OL Meta、Bookie 列表、Orphan、Standby 流 |
| **SDK / 示例** | 同左 | 代码仓 `examples/`（实现后） | 示例依赖未文档行为 |
| **协议参考** | SDK 作者 | [21](./21-wire-protocol.md) | 把帧格式当业务入门唯一材料 |
| **集群运维** | 运维 | [32](./32-operations.md) / ops | 与应用集成说明混写 |

客户角色验收：**仅读 `docs/user` + SDK README 能否完成范围内收发**。

---

## 2. 使用说明最低目录（必须成文）

| 章 | 回答 | 用户文档 |
|---|---|---|
| 快速开始 | 连哪、发一条、收一条、Ack | [getting-started](../user/getting-started.md) |
| 生产 | Receipt、pending、幂等如何开 | [produce](../user/produce.md) |
| 消费 | 四模式怎么选、Ack/Flow | [consume](../user/consume.md) |
| 错误与重试 | 哪些重试、切主长什么样 | [errors-and-retries](../user/errors-and-retries.md) |
| 投递语义 | at-least-once / 幂等阶段 / 不是什么 | [delivery-semantics](../user/delivery-semantics.md) |
| 连接与鉴权 | Proxy 默认、token/mTLS | getting-started §鉴权 |
| 限制与配额 | 消息大小、背压、配额满 | produce / errors |

缺章 ⇒ 对应能力不得标「客户可用」。

---

## 3. 对外语义承诺（诚实表）

必须原样出现在用户文档（措辞可口语化，**边界不得放宽**）：

| 阶段 | 可对客户说 | 禁止说 |
|---|---|---|
| **M1** | 至少一次；未 Receipt 视为不确定 | 精确一次 / exactly-once |
| **M2** | 开启幂等后：同 Owner 存活期内同 `(producer_name, sequence_id)` 不重复落盘 | 崩溃切主后仍精确一次 |
| **M3** | 开启幂等后：跨 Owner 切主仍可对已确认 seq 去重 | 「默认 exactly-once 无需配置」 |

| 事实 | 用户文档必须承认 |
|---|---|
| Broker 切主（崩溃） | 短暂失败/超时；应用应重试；计划内 Drain 近无感 |
| 已 Receipt | 达 Qa，可读；不等于业务侧只处理一次（除非幂等到位） |
| 未 Receipt | 不确定；重发可能重复（按阶段） |
| 多订阅 | 同一 topic 多 subscription = 各组各收全量（pub-sub） |
| Shared | 无分区内全局顺序；须 individual ack |

权威细节：[23](./23-producer-idempotency.md)、[15](./15-availability-failover-window.md)、[18](./18-dispatch-modes.md)。

---

## 4. 客户可见契约（稳定面）

应用只应依赖：

| 项 | 约定摘要 |
|---|---|
| 入口 | **生产默认经 Proxy**（单一 service URL）；直连 Broker 仅内网/开发 |
| Lookup | 地址变化靠 Lookup / SDK 自动刷新；勿写死 Owner IP |
| MessageID | `(ledgerId, entryId[, batchIndex])`；Receipt 携带 |
| 生产背压 | `maxPending`；满则阻塞或失败（SDK 可配） |
| 消费背压 | `Flow(permits)`；无许可不推送 |
| 订阅模式 | Exclusive / Failover / Shared / Key_Shared |
| 错误分类 | 可重试 vs 不可重试；见用户文档错误表（源自 [21 §5](./21-wire-protocol.md)） |
| 鉴权 | token 或 mTLS；失败码明确 |

**不可见 / 不可依赖**：ownership epoch、LedgerMeta、ensemble、orphan、Standby 内部协议、Admin 强制删 ledger。

---

## 5. SDK 默认行为（规格要求）

实现后 SDK 须满足（写入用户文档「默认行为」）：

```text
1. 连接：默认 Proxy URL；Connect 带 protocol_version
2. 可重试错误：有限次退避 + jitter（NotOwner / Timeout / Unavailable / 断连）
3. NotOwner / Fenced：禁止同连接死磕 Send；先 Lookup 再迁
4. 未 Receipt：不向业务回调「成功」
5. 幂等：显式 enable；要求稳定 producer_name + 单调 sequence_id
6. 消费：Subscribe 后发 Flow；处理完再 Ack；Shared 用 individual
```

无默认重试的「裸协议客户端」须在文档标明「高级 / 自建重试」。

---

## 6. 里程碑与「客户可用」

| 阶段 | 用户文档最低 |
|---|---|
| M1 | 进程内/示例收发；at-least-once；错误分类草案 |
| M2 | Proxy 连接说明；幂等 M2 边界；Failover；错误表与重试；示例 Produce→Consume→Ack |
| M3 | Shared/Key_Shared 选用指南；跨切主幂等；鉴权；batch/限制说明 |

对外承诺与 [user/](../user/README.md) 一致；生产级见 [36](./36-production-grade-bar.md)。

---

## 7. 体验缺陷与缺口

客户开单模板见 customer.md §8。常见打回：

- 必须读 design 内部章才能集成  
- 文档写 exactly-once 但里程碑未到  
- 切主无重试指引  
- 错误码不可操作  
- 示例与行为不符  

→ 体验单 → 补 `docs/user` / SDK / 必要时设计变更（走审计）。

---

## 8. 相关文档

| 文档 | 用途 |
|---|---|
| [../user/README.md](../user/README.md) | 使用说明入口 |
| [08](./08-client-backpressure.md) / [21](./21-wire-protocol.md) | 协议与背压（SDK 作者） |
| [18](./18-dispatch-modes.md) | 分发语义权威 |
| [23](./23-producer-idempotency.md) | 幂等阶段 |
| [14](./14-proxy.md) / [15](./15-availability-failover-window.md) | Proxy / 切主窗口 |
| [32](./32-operations.md) | 运维（非客户主路径） |
| [34](./34-architecture-goals.md) | 「用户无感」的架构定义与边界 |
