# 快速开始

规格：[../design/33-customer-usage.md](../design/33-customer-usage.md)。

## 1. 你需要什么

| 项 | 说明 |
|---|---|
| 服务入口 | **Proxy** 地址（推荐），形如 `proxy.example:6650`（Go SDK `ServiceURL`；文档亦写 `om://` 语义入口） |
| 鉴权 | 运维发放的 token（Connect / `OM_AUTH_TOKEN`），或 mTLS（若集群开启） |
| Topic | `tenant/namespace/topic`（如 `public/default/demo`） |
| SDK | Go：[sdk-go.md](./sdk-go.md)；示例 `examples/produce-consume` |

本地试玩拓扑（开发/测试，非生产）：[../deploy/local-k8s.md](../deploy/local-k8s.md)。

## 2. 连接

```text
1. 配置 serviceUrl = Proxy（不要枚举全部 Broker）
2. Connect（协议版本由 SDK 协商）
3. 可选：带 auth_token / 启用 TLS
```

直连某一 Broker 仅适合平坦内网调试；**公网 / K8s / 多故障域生产默认走 Proxy**。

## 3. 最小收发

```text
# 生产
Producer(topic) → Send(payload) → 等待 SendReceipt(messageId)
# 未收到 Receipt：不要当成功

# 消费
Subscribe(topic, subscription, mode) → Flow(permits)
← Message → 处理业务 → Ack(position)
# permits 用尽后需再 Flow，否则不再推送
```

Go 可运行示例见 [sdk-go.md](./sdk-go.md)。本地 K8s：`./scripts/k8s-customer-accept.sh`。

端到端验收：Lookup（由 SDK/Proxy 完成）→ Produce → Consume → Ack。

## 4. 鉴权（概念）

| 方式 | 客户侧 |
|---|---|
| Token | Connect / SDK 配置传入；失败 → `AuthFailed`（不可靠重试刷 token） |
| mTLS | 配置客户端证书；握手失败则无法 Connect |

具体字段名以 SDK 文档为准。

## 5. 下一步

- 生产细节与幂等 → [produce.md](./produce.md)  
- 选订阅模式 → [consume.md](./consume.md)  
- 失败怎么重试 → [errors-and-retries.md](./errors-and-retries.md)  
- 会不会丢 / 会不会重复 → [delivery-semantics.md](./delivery-semantics.md)
