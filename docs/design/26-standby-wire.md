# Warm Standby 同步协议（薄章；M3）

上游：[16-warm-standby.md](./16-warm-standby.md)、[21-wire-protocol.md](./21-wire-protocol.md)。  
**能力必做**；排期在阶段一（优雅切主、重试、幂等、租约、滚本）达标之后。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | Standby 预热需要 Primary→Standby 软状态通道 |
| 选型 | Broker↔Broker 专用流（非 Client 热路径）；见 16 |
| 放弃 | Standby 写权威消息盘；用 max(本地, Meta) 盖 cursor |
| 代价 | 额外带宽与协议面 |
| 验收 | 与 16 Promote 不变量一致 |

## 1. 通道选择（已定倾向）

| 选项 | 结论 |
|---|---|
| 复用 Client `OM` 帧 | **不**；Standby 不是租户 Client |
| **Broker↔Broker 内部流** | **已定**：独立 gRPC / 内部帧（可与 Admin 同端口不同 path） |
| 经 Metadata 传全量软状态 | **禁止**热路径；Meta 只存 Promote 相关指针 |

## 2. 消息类型（概念）

```text
StandbyHello { bundle, primaryEpoch, standbyId }
StandbyOffer { accepted, epoch }
TailHint { openLedgerId, lac, cursorSnapshots[] }     // 低频或变更推
CursorDelta { topic, sub, markDelete, ver }
Invalidate { reason }                                  // primary 失联 / Unload
PromoteIntent { expectEpoch }                          // 仅 Standby→Meta，非本流
```

Primary **推送**软状态；Standby **不**写 OL LedgerMeta、**不**Append。

## 3. 与 Promote / Fence

- Promote 谓词与探测：[16](./16-warm-standby.md) §3 / §3.1（**可早于满 TTL**；须 Renew 滞后/Unload/lease 兜底）。  
- 晋升成功后 **必须**仍走 `OL.Fence+Recover`；线协议再快也不能跳过。  
- Cursor 校准：**只认 CursorStore**，不用镜像 max。  

## 4. 阶段

| 阶段 | 交付 |
|---|---|
| M1–M2 | 无 Standby 流；文档与接口预留 |
| M3 | Hello/TailHint/CursorDelta；演练短路径 Recover |

## 5. 验收

1. 抓包无 Client magic `OM` 混入 Standby 流（或明确分端口）。  
2. Primary 杀进程 → Standby Promote + Fence；无双写。  
3. 软状态落后时退化为冷 Recover，仍正确。  
