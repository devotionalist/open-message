# 查找与路由：Bundle / Partition 细章

主文档：§16。通则：[00-principles.md](./00-principles.md)。

## 1. 为何合理

- **Bundle** 作为 ownership 单位 → 迁移粒度粗于单 topic，摊薄 Meta 与卸载抖动。  
- **Partition** 作为并行单位 → 单流写瓶颈时水平拆，而不搬账本。  
- Lookup 只读 Ownership(bundle) → O(1) 分片点查，可缓存。

## 2. 模型

```text
Tenant / Namespace
  └── Bundle = hash range [start, end)   // ownership key
        └── Topics whose hash(fullName) ∈ range
              └── Partitioned topic = { name-partition-0..N-1 }  // 各自 hash、可跨 bundle
                    └── each partition: ManagedLedger + ledgers
```

- `hash`：稳定 32-bit（如 murmur3）；升级算法需版本化，禁止默默变更。  
- 非分区 topic：一个 ML。  
- 分区数变更（M2+）：只影响新生产路由；旧 partition 保留至消费完（禁止瞬时丢 partition 数据）。

## 3. Lookup 正常路径

1. Client → 任意 Broker：`Lookup(topic)`。  
2. `b = bundle(namespace, hash(topic))`。  
3. `GetOwner(b)`；若无主且策略允许 → 本 Broker `TryAcquire(b)`（懒加载）。  
4. 返回 `{brokerAddr, epoch}`；Client 连 owner。  
5. 非 owner 收到 Produce → `NotOwner` + 可选 hint。

**缓存**：Client 与 Broker 均可缓存 bundle→owner；以 epoch/TTL 失效。

## 4. 故障与自愈

| 故障 | 自愈 |
|---|---|
| Owner 宕机 | lease 过期 → 他者 Acquire → Client NotOwner/超时 → 再 Lookup |
| Lookup 打到分裂中 Meta | ErrRetry → 退避重试 |
| 缓存指到旧主 | 旧主拒写 NotOwner → 再 Lookup |
| Bundle 过热单 Broker | Split bundle（元数据改范围）→ 部分 hash 区间迁走；**不搬 ledger** |
| 懒 Acquire 惊群 | 单飞行 Acquire + 等待；失败者 GetOwner |

## 5. 扩容

### 5.1 加 Broker

1. 注册 NodeStore。  
2. LoadManager 或人工选中过载 bundle → Unload。  
3. 新 Broker Acquire。  
4. 旧 Broker 释放内存中的 topic runtime。  

RTO：秒～数十秒级（Recover 链尾 ledger + 加载热 topic）。

### 5.2 Split Bundle

```text
[0x0000_0000, 0xFFFF_FFFF) 
  → [0, mid) on BrokerA + [mid, max) on BrokerB
```

- 仅改 Ownership 两个 key；topic 下次 Lookup 自然落到新 bundle。  
- 已加载在错误 Broker 的 topic：Unload 触发。  

### 5.3 加 Partition

- 提高单逻辑 topic 写并行；Client 按 key 路由。  
- 与加 Broker 正交：分区多但 bundle 粗仍会挤在少数 Broker → 需同时够 bundle 数。

## 6. LoadManager（已定：M2 退出条件）

崩溃爆炸半径 =「该 Broker 上所有 bundle」。无调度则 HA 文档中的「限制过密」无法落地。

| 项 | M2 必做 | M3 |
|---|---|---|
| 负载上报 | bundle 数、produce/consume rate、CPU、backlog | 更细指标 |
| 密度上限 | `max_bundles_per_broker`（可配）；超限 **P0 告警** | — |
| 调度 | **半自动**：选最重 bundle → Unload → 他者 Acquire；冷却防抖 | 自动 split/合并 |
| Admin | `unload` / bundle stats（[25](./25-admin-api.md)） | promote/standby 等 |
| LoadManager↔Broker | **内部 RPC 必做**（非可选） | — |

策略：超阈值 Unload 最重；禁止无冷却的来回迁。

## 7. 不变量

1. 同一 bundle 至多一 owner。  
2. Topic→bundle 映射纯函数，不因 owner 变化而变。  
3. 迁移不改变 MessageID 与 ledger 内容。  

## 8. 阶段

- M1：固定 N bundle；**手动** Unload；单机多 bundle 仅开发。  
- M2：**分区 topic + 上报 + 半自动 LoadManager + 密度告警**（退出门槛）。  
- M3：自动 split / 合并 bundle。
