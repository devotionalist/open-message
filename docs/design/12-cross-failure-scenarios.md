# 深挖：跨组件组合故障推演

把 Ledger / Meta / Ownership / Client 串在一起，验证「可自愈」不是单章自说自话。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 单章自愈矩阵过关 ≠ 组合故障可收敛 |
| 选型 | 显式组合场景 + L0–L2 分层结论 |
| 放弃 | 只测单点 kill |
| 代价 | 场景维护成本 |
| 验收 | 入 Chaos C*；与 15/23 幂等阶段一致 |

## 场景 1：Owner 崩溃 + 未确认写 + 客户端重试

```text
初始: epoch=1, ledger L open, lac=9
Client Send entry10 → 仅 1 个 bookie 成功 → Broker 崩溃（无 Receipt）
```

自愈：

1. lease 过期 → Broker2 Acquire epoch=2  
2. Fence+Recover(L)：可能 last=9 或 10  
3. 开 L'；Client Lookup 后重发 payload  
4. 若 last=10 且 Client 重发 → L' 再写一条业务重复（**M2 内存幂等在切主后不保证**；**M3** Meta 幂等收口）  
5. 若 last=9 → 仅 L' 有新 MessageID，语义干净  

**设计结论**：M1 at-least-once；M2 同 Owner 去重；**跨崩溃切主精确一次 = M3**（见 [23](./23-producer-idempotency.md)）。

## 场景 2：Meta 分片迁移中的 Acquire

```text
Broker2 TryAcquire(bundle) → ErrShardMoving
```

自愈：退避重试 → 新 shard Cas 成功 → 继续 Recover。  
**禁止**：本地强行认为自己是主并开写。

## 场景 3：滚本只完成 OL Close+Create，Index Cas 未成功

```text
open-ledger: L=CLOSED, L'=OPEN
open-message Index: 仍 open=L（未 append L'）
Broker 崩溃
```

自愈：

1. 新主读 Index：open=L  
2. OL.Recover/Open(L) 已 CLOSED → 取 lastEntryId  
3. Cas Index 挂新本：**一律 Create L''**（已定默认；禁止挂回 orphan，见 [22](./22-orphan-gc.md)）  
4. 未挂上的 L' → `MarkOrphan` → **open-ledger orphan GC**  

**设计结论**：跨系统无原子；靠 Index Cas + MarkOrphan + OL GC。禁止 CasCloseAndAppend 幻想。

## 场景 4：Bookie 丢盘 + 未补副本 + 再丢一台（Qa=2）

```text
Qw=3,Qa=2；已确认数据在 B0,B1,B2
B0 永久丢盘且 AutoRecovery 未完成；再丢 B1
```

结果：可能无法读已确认数据 → **耐久承诺被第二次故障打破**。  

自愈/运维：

- 监控 underreplicated > 0 告警  
- Recovery 限速但不沉默失败  
- 关键租户用更大 Qa 或隔离盘  

## 场景 5：慢消费 + 长 backlog + Broker OOM

```text
permits 仍被快速客户端要爆内存预读
```

自愈：

1. in-flight read 限额卡住预读  
2. backlog quota 拒写保护磁盘  
3. 仍 OOM → 进程崩溃 → 他者接管（cursor 已刷部分）→ at-least-once  

**设计结论**：背压在 L0 必须先于 L2 failover；单靠 failover 不是背压方案。

## 场景 6：Split Bundle 与在途 Produce

```text
Unload 左半区间时 Client 仍向旧主 Send
```

自愈：旧主 Unloading 返回 NotOwner；Client Lookup 到新主。  
进行中的 Recover/Close 完成后才放权（[09](./09-broker-lifecycle.md)）。

## 场景 7：时钟回拨与租约

```text
Broker 墙钟回拨导致以为 lease 仍有效
```

规则：leaseExpire 比较以 **Metadata 服务端时间** 为准；Renew 带服务器返回的 now。  
Client 超时用本地单调时钟即可。

## 矩阵：故障停在哪一层

| 场景 | 主要自愈层 |
|---|---|
| 1 | L2 Ownership + L1 Recover；同连接幂等 M2；跨切主精确一次 M3 |
| 2 | L0 重试 |
| 3 | L1 Load 补偿 + GC |
| 4 | L2 AutoRecovery；防住靠运维/Qa |
| 5 | L0 背压；极端 L2 |
| 6 | L0 NotOwner + L1 Unload 序 |
| 7 | Meta 服务端时间 |

## 设计缺口跟踪（深挖后仍开放）

| 缺口 | 倾向结论 | 阶段 |
|---|---|---|
| 生产幂等（同 Owner） | 要做 | M2 |
| 生产幂等（跨切主） | 要做 | M3 |
| Orphan ledger GC | 要做后台 | M1 可手动，M2 自动（MarkOrphan+全量对账） |
| OPEN ledger 上做 rereplicate | 默认等 Close | M3 |
| 跨 DC 同步 | 不做强同步；异步 geo 另章 | 远期 |
