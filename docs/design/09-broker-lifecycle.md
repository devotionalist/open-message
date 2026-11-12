# Broker 生命周期细章（加载 · 服务 · 卸载 · 自愈）

通则：[00-principles.md](./00-principles.md)。关联：[05-routing-bundle.md](./05-routing-bundle.md)、[06-fencing-consistency.md](./06-fencing-consistency.md)。

## 1. 为何合理

Broker 是无状态服务层，但仍有**运行时生命周期**。必须定义清楚，否则：

- unload 不干净 → 双主窗口；  
- load 一半就接写 → 数据分叉；  
- 崩溃恢复无序 → 长时间不可用。

## 2. Broker 进程态

```text
Starting → Registered → Serving → Draining → Stopped
               ↑            |
               +-- 心跳续约 --+
```

- **Registered**：写入 NodeStore（地址、容量标签、isolation 组）。  
- **Serving**：可接受 Lookup；对已持有 bundle 服务数据面。  
- **Draining**：拒绝新 Acquire；Unload 所有 bundle；停心跳前完成。

## 3. Bundle / Topic 生命周期

```text
Bundle:  Unowned → Acquiring → Owned → Unloading → Unowned
Topic:   Closed → Loading → Ready → Fenced → Closed
```

### 3.1 Acquire + Load（续服）

1. `TryAcquire(bundle)` 成功得 epoch。  
2. 对该 bundle 内**被访问**的 topic 懒 Load（非一次性加载全部）。  
3. Load topic：读 LedgerIndex → 若 open ledger 则 Fence+Recover → 开新 ledger → 挂链 → 加载 cursors → Ready。  
4. **Ready 前拒绝 Produce**（可接受 Lookup 已指向本节点但短暂不可写）。

### 3.2 服务中

- 续约 bundle lease。  
- Produce/Consume/Ack。  
- 滚账本、刷 cursor。  

### 3.3 Unload（优雅）

1. 标记 bundle Unloading；拒绝新 Producer。  
2. 等待 in-flight Send 完成或超时取消。  
3. 对各 topic：停分发 → Close 或 Fence+Recover 链尾 → 刷 cursor。  
4. `Unload` CAS 放权。  
5. 丢弃内存 runtime / cache。  

### 3.4 崩溃

- 无优雅路径；依赖 lease 过期 + 他者 Acquire + Recover（[06](./06-fencing-consistency.md)）。

## 4. 故障与自愈

| 故障 | 自愈 |
|---|---|
| Load 中途崩溃 | 租约过期；新主重新 Recover；可能 orphan ledger → GC |
| Unload 卡死 | 超时强制 Fence+放权；或等租约过期 |
| 续约失败 | 主动停止写并卸载本地；避免持过期 epoch |
| Topic 热点 OOM | 卸载该 topic/bundle；背压 Client；告警扩 partition |
| 注册中心短暂不可见 | 本地继续服务至 lease 风险点；不能无限无续约写 |

## 5. 扩容

- 水平加 Broker：Register → 迁 bundle。  
- 缩容：Drain → Stop；确保 bundle 都被他人持有。  
- 滚动升级：逐台 Drain；配合 `lostBookieRecoveryDelay` **无关**（那是 bookie）；Broker 侧用租约与 Unload。

## 6. 不变量

1. Serving 写请求必须持有效 epoch。  
2. Unload 完成后本进程不得再成功 Append 该 bundle 下账本。  
3. Load 完成前不得对该 topic 返回成功 Receipt。  

## 7. 阶段

- M1：单进程两逻辑 broker 测 Acquire/Unload/Recover。  
- M2：真实多进程 + 心跳 + Drain。  
- M2：半自动 LoadManager + 密度上限（见 [05](./05-routing-bundle.md) §6）。  
- M3：自动 split / 均衡。
