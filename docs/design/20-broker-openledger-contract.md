# Broker ↔ open-ledger 集成契约（已定）

上游权责：[19-open-ledger-integration.md](./19-open-ledger-integration.md)。  
open-ledger API：其仓库 `docs/design.md`（Bookie API + Client quorum）。  
合理性模板：[37](./37-design-rationale.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 两仓边界若只靠口头 → ledgerId/ensemble/错误/Orphan 必漂 |
| 选型 | 书面契约：发号/选盘/Offload/MarkOrphan/错误映射均钉死 |
| 放弃 | Broker 本地发号；Broker 维护 bookie 列表写 Meta |
| 代价 | Client 适配层必须齐 API |
| 验收 | 20 §7 |

## 1. 调用关系

```text
ManagedLedger (open-message)
    │  open-ledger Client（Go 适配或官方 client）
    ├── Create / Close / Fence / Recover / AddEntry / Read / Delete
    ├── MarkOrphan(ledgerId)   // 滚本/挂链永久失败；见 22；写 OL Meta orphan 标记
    │       → 写/读 **open-ledger Metadata**（LedgerMeta）+ Bookie gRPC
    └── 成功后再 Cas 本仓 LedgerIndex（仅 id 链）
```

**已定**：Client API **必须**暴露 `MarkOrphan`；Broker **禁止**自扫 Bookie 目录删盘。

Broker **不**直连拼装 quorum；一律走 Client。Bookie 进程是 **openledgerd**（非本仓 `cmd/bookie`）。

## 2. ledgerId 谁发号（已定）

| 角色 | 职责 |
|---|---|
| **open-ledger Client** | 分配集群内唯一 `ledgerId`（实现：Client 调 OL Meta 的发号器 / snowflake / 序列键，细则在 open-ledger） |
| **Broker** | **不**本地生成权威 id；Create 成功后拿到 `ledgerId` 再写入 LedgerIndex |

禁止 Broker 与 OL 各发各的号导致冲突。

## 3. ensemble 谁选（已定）

| 角色 | 职责 |
|---|---|
| **open-ledger Client** | 根据 E/Qw/Qa、bookie registry、亲和/隔离标签（若有）选择 ensemble；写进 **OL LedgerMeta.fragments** |
| **Broker** | 只传策略意图（如 namespace 的 persistency E/Qw/Qa、isolation 偏好）；**不**自己维护 bookie 列表写 Meta |

写失败换 fragment：Client 换 ensemble 并 CAS **OL LedgerMeta**；Broker 无感知细节，只见 AddEntry 错误/重试结果。

## 4. OffloadLocation 挂哪（已定）

| 字段 | 位置 |
|---|---|
| **OffloadLocation** | **open-ledger LedgerMeta**（仅 CLOSED） |
| LedgerIndex | **不存** OffloadLocation |

读路径：Broker 经 Client Read；若已 offload，由 **open-ledger 读路径**回源（或 Client 返回需冷读），Broker 不解析对象 URI。  
上传任务：M3 在 open-ledger 或旁路 worker 写 OL LedgerMeta；见 [24-offload.md](./24-offload.md)。  
Orphan：Create 未挂链 → [22-orphan-gc.md](./22-orphan-gc.md)。

## 5. 错误映射（Broker 可见）

| open-ledger / Client | Broker → Client（线协议） | 含义 |
|---|---|---|
| Fenced | `Fenced` / 可重试类 | **同 Owner**：按 [03](./03-autorecovery.md) 迷你滚本；**非 Owner**：Lookup |
| Sealed / closed | 触发滚本 | 应换本 |
| Not enough Qa / timeout | `Timeout` / `Unavailable` | 不确定；幂等见 [23](./23-producer-idempotency.md) |
| Conflict entry | `Internal` / 不可静默成功 | 同 id 不同 payload |
| Meta Cas 冲突 | Client 内重试；透出则 `Unavailable` | |
| Bookie 不足 / IN_RECOVERY 卡住 | `Unavailable` | 退避 Recover；见 [11](./11-ledger-recovery-deep.md) §3.1 |
| 选不出 ensemble | `Unavailable` | |

Receipt **仅**在 Client 确认 Qa durable 后返回；Broker 不得提前 Recipt。

## 6. 进程与端口（部署）

| 进程 | 仓 | 说明 |
|---|---|---|
| `openledgerd` | open-ledger | Bookie gRPC（默认如 `:3181`） |
| `openledger-autorecovery` | open-ledger | AR sidecar |
| `broker` | open-message | 仅 Client 连上述地址 |
| Oxia（或其它） | 可共享集群 | **key 前缀隔离** OM vs OL |

## 7. 验收

1. 本仓无 `led/` LedgerMeta key；无 `cmd/bookie`。  
2. Create 返回的 ledgerId 写入 Index 前后一致。  
3. 杀 Bookie：Broker 只见 Client 错误；AR 不在 Broker 内。  
4. Index Cas 永久放弃挂链后必调 `MarkOrphan`；OL Meta 可见 orphan 标记；无 Broker 本地删副本旁路。  
