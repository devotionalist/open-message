# 客户端 / Proxy 二进制协议（已定骨架）

细章配合：[08-client-backpressure.md](./08-client-backpressure.md)、[14-proxy.md](./14-proxy.md)。  
目标：解锁 M2 真实 SDK + Proxy；语义与 architecture §19 一致。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 无稳定帧格式则 SDK/Proxy 无法并行；错误不可重试分类 |
| 选型 | 自研 `OM` 帧 + 多路 requestId；错误码表可重试分类 |
| 放弃 | 直接复用 Pulsar 二进制（绑定生态）；Admin 走热路径帧 |
| 代价 | 自研协议成本；M2 起骨架需填编码细节 |
| 验收 | 21 §10；抓包 magic OM |

## 1. 传输

| 项 | 约定 |
|---|---|
| 承载 | TCP；生产可 TLS（Proxy 终结或透传） |
| 字节序 | 网络字节序（big-endian）多数字段；Protobuf 体除外 |
| 多路复用 | **单连接多 requestId**；响应带同一 requestId |
| 心跳 | `Ping` / `Pong`（空闲超时可配，默认 30s） |

Proxy 与 Broker 使用 **同一帧格式**；Proxy 转发时保留 requestId，可改写 Broker 地址类字段。

## 2. 帧布局

```text
+--------+--------+-----------+------------+----------------+
| magic  | ver    | total_len | header_len | header | payload |
| 2B     | 1B     | 4B        | 2B         | 变长   | 变长     |
+--------+--------+-----------+------------+----------------+

magic     = 0x4F4D  ('O''M')
ver       = 1
total_len = 从 header 起至帧尾的字节数（不含 magic/ver/total_len 自身 7 字节）
header    = Protobuf `CommandHeader`（见下）
payload   = 可选；Send/Message 为原始消息字节（或再包一层 MessageMetadata）
```

```text
CommandHeader {
  type:        enum      // Connect, Send, ...
  request_id:  uint64    // 0 表示无关联（如心跳）
  correlation: uint64    // 可选，Client 侧追踪
  broker_err:  ErrorCode // 响应时
}
```

`type` 决定后续是否还有 **command body Protobuf**（紧接 header，长度含在 header 或用 oneof 嵌在 Header 扩展）。  
实现约定：**Header Protobuf 自描述**；`header_len` 覆盖整个 command 元数据；`payload` 仅消息体。

## 3. 命令与方向

| type | 方向 | body 要点 | payload |
|---|---|---|---|
| Connect / Connected | C↔B | protocol_version, client_version, auth_token? | — |
| Ping / Pong | 双向 | — | — |
| Lookup / LookupResponse | C↔B | topic → broker_url, error | — |
| Producer / ProducerSuccess | C↔B | topic, producer_name, epoch?, enable_idempotency? | — |
| Send / SendReceipt / SendError | C↔B | producer_id, sequence_id；Receipt: ledger_id, entry_id, sequence_id | 消息体 |
| Subscribe / SubscribeSuccess | C↔B | topic, sub, mode(Exclusive\|Failover\|Shared\|Key_Shared), earliest/latest | — |
| Flow | C→B | consumer_id, permits | — |
| Message | B→C | consumer_id, ledger_id, entry_id, redelivery, batch_index? | 消息体 |
| Ack / AckResponse | C↔B | cumulative 或 individual 位点 | — |
| CloseProducer / CloseConsumer | 双向 | id | — |
| GetLastMessageId | C↔B | topic / producer 范围 | — |

Admin / Standby **不**走本帧：见 [25](./25-admin-api.md)、[26](./26-standby-wire.md)。

## 4. 命令体要点（加深）

### 4.1 Connect

```text
Connect {
  protocol_version: uint32
  client_version:   string
  auth_token?:      bytes
  proxy_to_broker?: string   // Proxy 透传时可选
}
Connected {
  protocol_version: uint32
  max_message_size: uint32
  server_version:   string
}
```

### 4.2 Producer / Send（幂等字段）

```text
Producer {
  topic, producer_name
  producer_id:     uint64      // 连接内分配，Success 回显或 Client 自带
  enable_idempotency: bool     // true 则要求 producer_name 非空
  initial_sequence_id?: uint64
}
Send {
  producer_id, sequence_id
  num_messages?: uint32        // batch；M3
}
SendReceipt {
  producer_id, sequence_id
  ledger_id, entry_id
}
```

重复 `sequence_id` 行为见 [23-producer-idempotency.md](./23-producer-idempotency.md)。

### 4.3 Subscribe / Flow / Ack

```text
Subscribe {
  topic, subscription
  mode: Exclusive | Failover | Shared | Key_Shared
  initial_position: Earliest | Latest
  consumer_id, consumer_name?
  key_shared_policy?: { sticky | auto_split, hash_ranges? }
}
Flow { consumer_id, permits }          // 增量许可；见 08
Ack {
  consumer_id
  cumulative?: Position
  individual?: Position[]              // M3 Shared
}
Position { ledger_id, entry_id, batch_index? }
```

分发语义权威在 [18-dispatch-modes.md](./18-dispatch-modes.md)。

## 5. 错误码（ErrorCode）

| 码 | 可重试 | 说明 |
|---|---|---|
| OK | — | |
| NotOwner | 是 | 先 Lookup |
| Fenced | 是 | 同 NotOwner 处理写 |
| Timeout / Unavailable | 是 | 退避；Send 不确定 → 依赖幂等 |
| QuotaExceeded | 视策略 | 常退避 |
| AuthFailed | 否 | |
| TopicNotFound | 否 | |
| InvalidFrame / UnsupportedVersion | 否 | 升级或断开 |
| ProducerBusy / ConsumerBusy | 视模式 | Exclusive 冲突等 |
| ProducerFenced | 否/重连 | 同名幂等 Producer 被顶替（若启用） |
| ChecksumError | 否 | payload 校验失败 |

## 6. 版本协商

1. Client `Connect.protocol_version` = 所支持最大版本。  
2. Server `Connected.protocol_version` = min(client, server)。  
3. 若 Client 最小需求 > Server → 断开并报 UnsupportedVersion。  
4. 帧 `ver` 与 Connect 协商版本一致；未知 type → UnsupportedVersion。

## 7. 大小与限制

| 项 | 默认（可配） |
|---|---|
| 单帧 `total_len` 上限 | 与 `max_message_size` + header 一致（如 5MiB） |
| 单连接最大 pending Send | 与 Client pending 队列对齐 |
| 空闲超时 | 30s 无 Ping/业务 → 断开 |

超限：`InvalidFrame` 或断开，防爆。

## 8. Proxy 行为

- 入站帧校验 magic/ver/长度上限（防爆）。  
- Lookup 可由 Proxy 代劳或透传；缓存 owner，见 [14](./14-proxy.md)。  
- 转发 Send/Message **不解析业务 payload**（除限流统计可选）。  
- 上游 `NotOwner` → Proxy 可清缓存并重 Lookup 再试（次数有限），或把错误回 Client。  
- **不**改写 `sequence_id` / `request_id`。

## 9. 阶段

| 阶段 | 交付 |
|---|---|
| M1 | 进程内 API 等价语义；可不落帧 |
| M2 | 本帧格式 + 单语言 SDK + Proxy 转发 + 幂等字段 |
| M3 | 压缩、batch、chunk、鉴权扩展；Key_Shared 策略体 |

## 10. 验收

1. 抓包可见 magic `OM`、requestId 匹配。  
2. 旧 Client 连新 Server：协商失败则干净断开。  
3. Proxy 杀连接后 Client 重连 Lookup 可恢复。  
4. 同 sequence_id 重发（幂等开）→ 相同 MessageID Receipt。  
