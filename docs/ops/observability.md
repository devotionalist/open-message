# 可观测性目录（骨架）

规格：[../design/32-operations.md](../design/32-operations.md) §7。  
实现打点对齐开发 [30](../design/30-engineering-quality.md)。

## 1. 指标（最低集）

| 名（建议） | 类型 | 告警？ | Runbook |
|---|---|---|---|
| `om_produce_requests_total` | counter | 错误率 | — |
| `om_produce_errors_total{code=}` | counter | 是 | 按 code |
| `om_produce_latency_seconds` | histogram | p99 | 容量/慢盘 |
| `om_failover_duration_seconds` | histogram | 是 | owner-crash / standby-promote |
| `om_not_owner_total` | counter | 突发 | broker-drain-unload |
| `om_lease_renew_fail_total` | counter | 是 | owner-crash |
| `om_meta_cas_total` / `om_meta_cas_fail_total` | counter | 是 | om-meta-outage |
| `om_index_ledger_missing_ol` | gauge | 是 | dual-meta / 对账 |
| `om_orphan_pending` | gauge | 是 | orphan-gc |
| `ol_underreplicated_ledgers` | gauge | 是 | bookie-down |
| `om_bundles_per_broker` | gauge | 超密度 | scale-out |

（名称以实现为准；**语义**不得删减。）

## 2. 告警表示例

| 告警 | 条件（示例） | 级别 | Runbook |
|---|---|---|---|
| OwnerFailoverSlow | failover p99 > 阈值 | P1 | owner-crash |
| MetaDown | meta 探活失败 | P0 | om-meta-outage |
| Underreplicated | >0 持续 N 分钟 | P1 | bookie-down |
| BundleDensityHigh | bundles/broker > max | P0 | scale-out |

## 3. 日志 / 追踪

- 日志字段：`request_id`、`topic`、`bundle`、`epoch`、`error_code`。  
- 禁止默认打印消息 payload。  
- 追踪：Proxy→Broker→OL 关键路径（按里程碑开启）。
