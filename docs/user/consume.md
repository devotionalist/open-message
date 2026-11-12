# 消费消息

## 1. 基本模型

| 概念 | 含义 |
|---|---|
| Topic | 一份逻辑日志（可多分片/分区） |
| Subscription | 一组消费进度；**多个 subscription 各自收全量**（发布订阅） |
| Consumer | 挂在某个 subscription 上的连接 |
| Cursor | 该 subscription 的位点（集群侧）；你通过 Ack 推进 |

```text
Subscribe(topic, subscription, mode, initialPosition)
→ Flow(permits)          # 告诉 Broker 能推多少
← Message …
→ Ack / 再 Flow
```

无 `Flow`（permits=0）时 Broker **不会**推消息。

## 2. 四种模式怎么选

| 模式 | 消费者数 | 顺序 | Ack | 典型场景 |
|---|---|---|---|---|
| **Exclusive** | 仅 1 | 分区内全局有序 | 累计（cumulative） | 单活流水线 |
| **Failover** | 多；仅 active 收 | 同 Exclusive | 累计 | 有序 + 消费端主备 |
| **Shared** | 多；竞争消费 | **无**全局顺序 | **必须**逐条（individual） | 任务队列、扩消费能力 |
| **Key_Shared** | 多；按 key 粘连 | **同 key 有序** | individual（实现约定） | 按用户/订单并行且保序 |

选型误区：

- 要「多实例抢活且全局有序」→ Failover，不是 Shared。  
- 要用 Shared 却发 cumulative Ack → 错误；会丢或卡位点。  
- 多 subscription ≠ Shared；那是多组各收全量。

## 3. 起始位置

| initialPosition | 行为 |
|---|---|
| Latest | 从订阅创建后的新消息开始（常见） |
| Earliest | 从当前保留的最早消息开始 |

具体是否可对已存在 subscription 改起始位，以 SDK/发行说明为准。

## 4. Ack 与重投

| 行为 | 说明 |
|---|---|
| Ack | 确认处理完成；位点可前移 / 记入 individual 集合 |
| 未 Ack 断连 | 消息可对其他消费者重投（Shared）或对同一 cursor 重投（Exclusive/Failover） |
| Nack / ackTimeout | 若 SDK 支持：进入重投；可能换 Consumer（Shared） |

**at-least-once**：业务处理须可承受重复，或依赖生产幂等 + 业务去重键。

## 5. Failover 注意

- 仅 active 收消息；standby 连着但不收。  
- active 断开后自动切到下一优先消费者；可能短暂空窗。  
- 未 Ack 消息会重投给新 active。

## 6. Key_Shared 注意

- 同一 key 粘到同一消费者（策略：sticky / auto_split，以 SDK 为准）。  
- 消费者增减可能触发 key 范围重平衡；重平衡窗口内可能短暂重复或延迟。  
- 无 key 的消息策略见发行说明（勿假设）。

## 7. 下一步

错误与切主 → [errors-and-retries.md](./errors-and-retries.md)  
重复/丢失边界 → [delivery-semantics.md](./delivery-semantics.md)
