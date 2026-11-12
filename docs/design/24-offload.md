# Ledger Offload（薄章；M3 实装）

上游：[architecture §3](../architecture.md)、[20-broker-openledger-contract.md](./20-broker-openledger-contract.md)、[22-orphan-gc.md](./22-orphan-gc.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 热盘成本；冷数据需卸到对象存储 |
| 选型 | 仅 CLOSED offload；location 只在 OL；热路径禁直写对象存储 |
| 放弃 | Produce 主写对象存储；Index 存 OffloadLocation |
| 代价 | 冷读延迟；状态机顺序要求严 |
| 验收 | 校验可读后再删热副本 |

## 1. 已定

| 项 | 约定 |
|---|---|
| 热路径 | **禁止** Produce 直写对象存储 |
| 候选 | 仅 **CLOSED** ledger |
| 位置字段 | **`OffloadLocation` 只在 open-ledger LedgerMeta**；OM LedgerIndex **不存** |
| 读 | Broker → OL Client Read；Client/OL 见 location 后回源 |
| 删热副本 | Offload 成功且校验后，延迟删 bookie 本地；AR 不再为已冷删副本补热盘（策略可配） |

## 2. 状态机（已定）

```text
HOT (CLOSED, 热副本在)
  → OFFLOADED          // CAS offloadLocation；冷读校验通过
  → HOT_DELETED        // 删 bookie 本地；仍可靠 location 回源
  → INDEX_REMOVED      // 全部 cursor 越过 + retention；OM Index 摘 id
  → META_DELETED       // 删 OL LedgerMeta（此后须仍能按 retention 扫对象或已 OBJECT_GC）
  → OBJECT_GC          // 删对象字节
```

| 禁止 | 原因 |
|---|---|
| 未 OFFLOADED 校验就 HOT_DELETED | 冷读失败 → 丢数据 |
| Index 仍引用就 META_DELETED | 丢 location / 不可读 |
| META_DELETED 前无对象保留策略就 OBJECT_GC | 同上 |

Broker **不**写 OffloadLocation；最多 Admin 触发前缀 offload。

## 3. 阶段

| 阶段 | 交付 |
|---|---|
| M1 | LedgerMeta 字段预留（open-ledger 已对齐） |
| M3 | 上传、回源、延迟删热、指标与故障重试 |

## 4. 验收

1. Index 无 OffloadLocation 字段。  
2. 杀 Broker 不影响已写 location。  
3. 回源失败不得提前删热副本。  
