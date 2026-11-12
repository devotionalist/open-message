# open-message 使用说明

**层属 L5（客户落地）。** 文档总入口：[../README.md](../README.md)。  
面向**应用开发者 / SDK 集成方**。规格门禁（L4）：[../design/33-customer-usage.md](../design/33-customer-usage.md)。  
生产级：对外能力退出前本文档须达可集成标准（[../design/36-production-grade-bar.md](../design/36-production-grade-bar.md) §1.6）。  
**不要**从 L3 内部设计章上手集成。

**不在这里**：集群安装、扩缩容、值班恢复 → 见 [../ops/README.md](../ops/README.md)。

## 阅读顺序

| 顺序 | 文档 | 内容 |
|---|---|---|
| 1 | [getting-started.md](./getting-started.md) | 连接、鉴权、发一条收一条 |
| 1b | [sdk-go.md](./sdk-go.md) | Go SDK（及 Java/Rust/TS 兄弟库）与本地 K8s 验收 |
| 2 | [produce.md](./produce.md) | 生产、Receipt、背压、幂等开关 |
| 3 | [consume.md](./consume.md) | 四模式选用、Flow、Ack |
| 4 | [errors-and-retries.md](./errors-and-retries.md) | 错误分类与切主表现 |
| 5 | [delivery-semantics.md](./delivery-semantics.md) | 至少一次 / 幂等阶段（诚实承诺） |

## 你需要知道的底线

1. **生产默认连 Proxy**（一个 URL），不要写死每台 Broker。  
2. **只有拿到 SendReceipt 才算发送成功**；未 Receipt = 不确定。  
3. **切主时短暂失败是正常的**；靠重试 + Lookup，不要当集群永久坏了。架构上「运维/故障对用户无感」= **你不用人工改应用去修集群**，不是进程日志永远零错误（见 [34 §4](../design/34-architecture-goals.md)）。  
4. **不要假设默认 exactly-once**；见 [delivery-semantics](./delivery-semantics.md)。

## 里程碑与文档覆盖

以发行说明为准；设计侧阶段边界见 [33 §3](../design/33-customer-usage.md) / [23](../design/23-producer-idempotency.md)。
