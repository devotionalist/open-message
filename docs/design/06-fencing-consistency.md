# 一致性与 Fencing 时序细章

主文档：§17。通则：[00-principles.md](./00-principles.md)。关联：[01-ledger.md](./01-ledger.md)、[02-metadata.md](./02-metadata.md)、[09-broker-lifecycle.md](./09-broker-lifecycle.md)。

## 1. 为何合理

分布式下「旧主复活」比宕机更危险。必须用 **epoch + fence** 保证：

- 至多一个 bundle owner 能成功写；  
- 至多一个 ledger writer 能达到 Qa；  
- 恢复后日志尾部唯一。

## 2. 世代令牌

| 令牌 | 存哪 | 用途 |
|---|---|---|
| `bundleEpoch` | OwnershipStore | 服务/写请求世代；Renew/Unload 校验 |
| `writerEpoch` | 可等于 bundleEpoch；写入 Bookie 侧可选校验 | 拒旧 Append |
| `chainVersion` / `metaVersion` | LedgerIndex / LedgerMeta | 防丢更新 |

## 3. 状态机

### 3.1 Bundle ownership

```text
Unowned --TryAcquire--> Owned(broker, epoch, lease)
Owned --Renew--> Owned(同 epoch, 新 lease)
Owned --Unload/Expire--> Unowned
```

### 3.2 Ledger

```text
OPEN --Close--> CLOSED
OPEN --Fence--> IN_RECOVERY --Recover--> CLOSED
CLOSED --Delete--> Tombstone/GC
```

非法：CLOSED → OPEN（永不）；IN_RECOVERY → OPEN（禁止，只能 CLOSED 后开**新** ledger）。

## 4. 主切换完整时序（自愈主路径）

```text
T0  OldOwner 持有 epoch=e1，lease 有效，写 OPEN ledger L
T1  OldOwner 断网/崩溃；续约停止
T2  leaseExpire < now
T3  NewOwner TryAcquire → epoch=e2
T4  NewOwner Fence(L)；OldOwner 若诈尸 Append → ErrFenced（达不到 Qa）
T5  NewOwner Recover(L) → CLOSED(last)
T6  NewOwner Create(L'); CasIndex 挂 L'
T7  NewOwner 用 e2 对外服务
T8  Client 收到错误/超时 → Lookup → 连 NewOwner；未确认消息可重发
```

**安全条件**：T4 在 T7 对外成功写之前完成；否则存在双写窗口。实现上：**未完成 Recover+挂新链前，拒绝 Client Produce**（可接受短暂不可写）。

## 5. 并发 Acquire

两 Broker 同时 TryAcquire：CAS 一胜一败；败者 GetOwner 后做 redirect。无脑裂双主。

## 6. 读一致性

| 读类型 | 规则 |
|---|---|
| Tail 消费 | 只分发 ≤ LAC；LAC 来自当前 owner 内存，failover 后以 Recover 为准重算 |
| Catch-up | CLOSED ledger 全可读；OPEN 仅到 LAC |
| Lookup | 允许旧缓存，纠错靠 NotOwner |

## 7. 故障矩阵（fencing 视角）

| 场景 | 风险 | 机制 |
|---|---|---|
| 旧主写成功到部分 bookie | 分叉 | Fence 使达不到 Qa；Recover 读修 |
| 新主未 Fence 就写新 ledger | 旧主仍写旧 L | **禁止**；强制先 Fence/Recover |
| 客户端连两主各写成功 | 双写 | 旧主必须拒；Client 单飞行 + Lookup |
| Meta 显示新主但旧主租约墙钟未过 | 允许仅当 **合法 Promote**（[16](./16-warm-standby.md) §3.1：探测成立且 Renew 已停）或 Unload；**冷** `TryAcquire` 仍仅过期/Unload | Promote CAS 与 TryAcquire 互斥；时钟用 Meta 服务端时间；健康 Renew 中禁止 Promote |

## 8. 扩容时一致性

- Unload：主动交权，等同优雅 failover（先停写 → Fence/Close → 放权）。  
- Split bundle：先 Unload 原子区间或按子区间串行交权，避免两 owner 短暂重叠。  

## 9. 不变量（总）

1. 同一 bundle 有效 epoch 唯一。  
2. 同一 OPEN ledger 成功 Append 的 writer 世代唯一。  
3. CLOSED ledger 的 lastEntryId 唯一且只增定一次。  
4. Client 可见的已确认写 ⊆ 达 Qa 的写。  

## 10. 阶段

- M1：进程内 fence 标志 + bundleEpoch；单测覆盖诈尸。  
- M2：Bookie 持久化 fenced 状态 + writerEpoch。  
- M2：同 Owner 生产幂等；M3：Meta 幂等表缩小跨切主不确定窗口（[23](./23-producer-idempotency.md)）。
