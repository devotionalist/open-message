# Go SDK（客户面）

规格：[../design/33-customer-usage.md](../design/33-customer-usage.md)。  
官方库：[open-message-client-go](../../../open-message-client-go)（另有 Java / Rust / TypeScript 兄弟库）。  
示例：`examples/produce-consume`。

## 安装

```bash
go get github.com/devotionalist/open-message-client-go@latest
```

本地开发可：

```text
replace github.com/devotionalist/open-message-client-go => ../open-message-client-go
```

## 连接

```go
import om "github.com/devotionalist/open-message-client-go"

c, err := om.Connect(ctx, om.Config{
  ServiceURL: "proxy.example:6650", // 生产默认 Proxy；勿枚举 Broker
  AuthToken:  os.Getenv("OM_AUTH_TOKEN"),
  MaxPending: 1000,
  MaxRetries: 8,
})
```

`ServiceURL` 为 `host:port`（scheme `om://` 由运维文档描述；SDK 当前拨号 TCP）。

## 生产

```go
p, err := c.CreateProducer(ctx, "tenant/ns/topic", "my-producer", true) // 末参=幂等
ledgerID, entryID, err := p.Send(ctx, []byte("payload"))
// 仅收到 Receipt（无 error）才算成功；失败见 errors-and-retries
```

同 Owner 存活期内，开启幂等后相同 `sequence_id` 不会重复落盘（M2）。**不要**宣称崩溃切主后仍精确一次（见 [delivery-semantics](./delivery-semantics.md)）。

## 消费

```go
co, err := c.Subscribe(ctx, "tenant/ns/topic", "sub", "Exclusive")
_ = co.Flow(ctx, 10)
msg, err := co.Receive(ctx)
_ = co.AckCumulative(ctx, msg.LedgerID, int64(msg.EntryID))
```

## 其他语言

| 语言 | 库 | 示例 |
|---|---|---|
| Go | `open-message-client-go` | `examples/produce-consume` |
| Java | `open-message-client-java` | `…/examples/ProduceConsume` |
| Rust | `open-message-client-rust` | `examples/produce_consume` |
| TypeScript | `open-message-client-typescript` | `examples/produce-consume.mts` |

四国对本地 Broker 一键验收（主机 port-forward `broker-0`；因无法解析集群内 Owner DNS）：

```bash
./scripts/sdk-accept-all.sh
```

## 本地 K8s 验收

```bash
./scripts/k8s-up.sh
./scripts/k8s-customer-accept.sh   # 集群内 Go 镜像 → Proxy
./scripts/sdk-accept-all.sh        # 主机侧四国 SDK → broker-0
```

期望输出含 `customer-acceptance: PASS` / `sdk-accept-all: PASS`。
