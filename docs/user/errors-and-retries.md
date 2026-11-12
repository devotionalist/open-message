# 错误与重试

切主与失败窗口说明（设计侧）：[../design/15-availability-failover-window.md](../design/15-availability-failover-window.md)。  
错误码权威：[../design/21-wire-protocol.md](../design/21-wire-protocol.md) §5。

## 1. 原则

1. **可重试**：有限次 + 指数退避 + jitter；耗尽后上抛给业务。  
2. **不可重试**：修配置/权限/参数；盲重试只会刷屏。  
3. **`NotOwner` / `Fenced`**：不要在旧连接上死磕；先 Lookup（或让 SDK/Proxy 做），再继续。  
4. **未 Receipt 的 Send**：按「不确定」；开幂等后安全重发同 `sequence_id`。

官方 SDK 应对上表可重试码提供**默认重试**；关闭默认重试须在文档标明。

## 2. 错误分类（客户向）

| 错误 | 可重试？ | 建议动作 |
|---|---|---|
| `NotOwner` | 是 | Lookup → 换后端 → 重试 |
| `Fenced` | 是（写路径） | 同 NotOwner |
| `Timeout` / `Unavailable` | 是 | 退避重试；Send 视为不确定 |
| 连接断开 | 是 | 重连 + Lookup；未确认 Send 按语义重发 |
| `QuotaExceeded` | 视策略 | 降速 / 退避；持续满则扩容或降流量 |
| `AuthFailed` | 否 | 换凭证；勿高频重试 |
| `TopicNotFound` | 否 | 建 topic 或改名 |
| `InvalidFrame` / `UnsupportedVersion` | 否 | 升级 Client/Server |
| `ProducerBusy` / `ConsumerBusy` | 视模式 | Exclusive 已有消费者等；换名或模式 |
| `ProducerFenced` | 通常否 | 同名幂等 Producer 被顶；换名或协调单写者 |
| `ChecksumError` | 否 | 查客户端损坏/中间盒 |

## 3. 切主时业务侧观感

| 场景 | 你可能看到 | 期望行为 |
|---|---|---|
| 计划内滚动（Drain） | 极短重试或几乎无感 | SDK 自动消化 |
| Owner 崩溃 | 超时、断连、`NotOwner` | 自动重试；窗口秒～数十秒量级（视集群） |
| Meta 短暂抖动 | `Unavailable` | 退避；勿雪崩重连 |

**不要**：把切主窗口当成「必须人工重启应用」的默认运维手段。

## 4. 生产重试与幂等

```text
Send 超时
  → 若未开幂等：重发可能造成重复消息（至少一次）
  → 若已开幂等：用同一 sequence_id 重发
       M2：Owner 仍在 → 安全
       M3：跨切主 → 仍可对已确认 seq 安全
```

详见 [delivery-semantics.md](./delivery-semantics.md)、[produce.md](./produce.md)。

## 5. 消费侧重试

- 处理失败：可 Nack / 不 Ack 等超时重投（以 SDK 为准）。  
- 业务须幂等，或去重键入库。  
- Shared 下重投可能到另一 Consumer。
