# 客户端协议与背压 细章

主文档：§19。通则：[00-principles.md](./00-principles.md)。

## 1. 为何合理

- 推送 + permits：低延迟 tailing，同时防止慢消费者拖垮 Broker 内存。  
- 生产 pending 窗口：把压强挡在 Client，避免无限 in-flight 打爆 Broker/Bookie。  
- 错误可重试分类：支撑 failover 自愈，而不双写成功。

## 2. 连接与命令

**帧格式 / 版本协商 / 错误码表** → [21-wire-protocol.md](./21-wire-protocol.md)（已定）。

### 2.1 连接

1. Lookup → owner 地址（或经 Proxy）。  
2. TCP/TLS 长连接 `Connect`（协商 protocol_version）。  
3. 在该连接上打开 Producer/Consumer（多路 `request_id`）。  
4. 收到 `NotOwner`/`Fenced` → 关闭写路径、重新 Lookup、迁连。

### 2.2 最小命令

见 [21 §3](./21-wire-protocol.md)：Connect、Lookup、Producer、Send/Receipt/Error、Subscribe、Flow、Message、Ack、Close*、Ping/Pong。

## 3. 生产路径与背压

```text
inflight < maxPending ? Send : block/fail
Receipt → inflight--
SendError(retryable) → 可重发策略
SendError(NotOwner|Fenced) → 禁止盲目重试同连接；先 Lookup
```

| 背压来源 | Client 行为 |
|---|---|
| maxPending 满 | 阻塞或 `ErrBackpressure` |
| Broker QuotaExceeded | 退避 |
| Bookie 慢 | Receipt 变慢 → pending 自然堵住 |
| Meta 抖动 | 不应每条消息打 Meta |

**幂等**：M2 = 同 Owner 去重；M3 = 跨切主 Meta 表。细则 [23-producer-idempotency.md](./23-producer-idempotency.md)。

## 4. 消费路径与背压

```text
Subscribe → Flow(initialPermits)
Broker 仅在 permits>0 时 Message 推送
Client 处理完 → Ack；队列低于水位 → Flow(增量)
```

| 背压来源 | 行为 |
|---|---|
| permits=0 | Broker 停推 |
| Broker in-flight read 限额 | 停从 Bookie 预读 |
| 慢消费 | 积压涨 → 触发 namespace backlog 策略 |

## 5. 故障与自愈

| 故障 | Client 自愈 |
|---|---|
| 连接断 | 重连 Lookup；未确认序列重发（同 Owner 时 M2 安全；跨切主 M3 才精确） |
| 超时无 Receipt | 视为不确定；重发按上表阶段承诺 |
| NotOwner | Lookup 迁主 |
| 半开连接 | 心跳 / 请求超时探测 |

**禁止**：对 NotOwner 在旧连接上疯狂重试 Send（雪崩 + 可能干扰）。

## 6. 扩容观感

- 加 Broker：短暂 NotOwner/超时，重 Lookup 后恢复。  
- 加分区：Client 感知分区元数据变更（M2 admin API）。  
- 协议本身无状态，Client 水平扩消费者即可升消费并发（Shared M3）。

## 7. 不变量

1. 无 permits 不得推送。  
2. 未 Receipt 的消息不得视为成功。  
3. 成功 Receipt ⇒ 已达 Qa（Broker 保证）。  

## 8. 阶段

- M1：进程内 API 模拟 pending + permits + NotOwner。  
- M2：二进制协议 + 单语言 Client + 基础幂等。  
- M3：batch/compress/chunk/auth/多语言。
