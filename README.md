# open-message

面向**超大规模、高并发、高可用、高吞吐**的存算分离消息系统。  
Broker **无消息权威盘**；消息体在 **open-ledger**（Bookie）；业务元数据在 Oxia / OpenKV（与 OL Meta 隔离）。

**目标合同**（高可用 · 高并发 · 强一致 · 运维/故障对用户无感）：[docs/design/34-architecture-goals.md](./docs/design/34-architecture-goals.md)  
**硬约束**（存算分离 · 故障自愈 · **在线扩容不搬历史**）：[docs/design/00-principles.md](./docs/design/00-principles.md)

---

## 解决什么问题

| 诉求 | 做法 |
|---|---|
| 超大规模 / 高吞吐 | Bundle / 分区并行；加 Broker、Meta 分片、Bookie **抬并行度**，历史消息体不搬盘 |
| 高并发 | Owner 按 Bundle 分片；Shared / Key_Shared 消费；Proxy 汇聚入口 |
| 高可用 | 租约 + Fence/Recover；Drain/Unload；Bookie Qa + open-ledger AutoRecovery；Proxy/SDK 重试 |
| 在线扩容 | **所有节点类型均可在线扩**：Broker / Proxy / Bookie / Meta——新流量上新节点，**不把搬历史副本当必经路径** |

---

## 核心能力

| 能力 | 说明 |
|---|---|
| **Produce / Consume** | Lookup → Owner；Qa 后 Receipt；四订阅模式（Exclusive / Failover / Shared / Key_Shared） |
| **存算分离** | Broker 可丢可迁；耐久在 open-ledger ensemble（E/Qw/Qa） |
| **单主排序 + 切主** | Bundle Ownership；崩溃/Drain 后 Fence+Recover 钉尾，再开新 ledger |
| **写路径自愈** | 同主遇 Fenced / bookie 不足：ChangeEnsemble 或滚本，换健康 bookie 续写 |
| **在线扩缩** | 加 Broker/Bookie/Meta/Proxy 不中断；缩容有 min_* 与 Drain 护栏 |
| **元数据** | 默认 Oxia（`/om/`）；一等可选 OpenKV；与 OL Meta 分家 |
| **入口与 SDK** | Proxy；官方 Go / Java / Rust / TypeScript 客户端 |
| **可运维** | Admin、指标、Chaos / 带流量故障演练、Runbook |

**不超文档承诺**：默认 **at-least-once**；跨切主精确一次仅在幂等阶段（见 [23](./docs/design/23-producer-idempotency.md) / 用户文档）达到时对外说明。

---

## 文档与角色

| 入口 | 用途 |
|---|---|
| [docs/README.md](./docs/README.md) | L0–L5 文档中心（设计 + 说明） |
| [docs/user/](./docs/user/README.md) | 应用 / SDK 使用说明 |
| [docs/ops/](./docs/ops/README.md) | 运维 Runbook / 可观测性 |

---

## 快速跑通（本地 K8s / Colima k3s）

默认 **Metadata = Oxia**（复用 `open-ledger` 命名空间；key 前缀 `/om/`）。

```bash
./scripts/k8s-up.sh
./scripts/k8s-customer-accept.sh
./scripts/k8s-fault-drill.sh   # 可选：带流量杀 broker/bookie
```

| 变量 | 默认 |
|---|---|
| `METADATA_BACKEND` | `oxia`（可切 `openkv`） |
| `OXIA_ADDR` | `oxia-0.oxia.open-ledger.svc.cluster.local:6648` |
| `OXIA_PREFIX` | `/om/` |
| `BOOKIE_ADDRS` / `OL_META_URL` | 接真实 open-ledger；未设则进程内 memory quorum |

---

## 官方客户端

| 语言 | 路径 |
|---|---|
| Go | [`../open-message-client-go`](../open-message-client-go) |
| Java | [`../open-message-client-java`](../open-message-client-java) |
| Rust | [`../open-message-client-rust`](../open-message-client-rust) |
| TypeScript | [`../open-message-client-typescript`](../open-message-client-typescript) |

---

## 单测 / Chaos

```bash
go test ./internal/... ./test/chaos/... ./test/opsdrill/... ./test/stress/...
```

---

## 组件

| 二进制 | 作用 |
|---|---|
| `cmd/broker` | Broker（Meta + OM 协议 + Admin + OL 客户端） |
| `cmd/proxy` | 生产入口；多 Broker bootstrap |
| `cmd/metakv` | 可选 HTTP Meta / LedgerMeta 适配 |
| `cmd/faultdrill` | 带流量故障演练 |
| `examples/produce-consume` | 客户验收示例 |
