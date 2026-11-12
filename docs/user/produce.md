# 生产消息

## 1. 成功的定义

| 结果 | 含义 | 你该怎么做 |
|---|---|---|
| **SendReceipt** | 消息已达集群耐久门槛（Qa） | 可向下游认为「已提交」 |
| 超时 / 可重试错误 / 断连 | **不确定**是否已写入 | 按 [delivery-semantics](./delivery-semantics.md) 重发或开幂等 |
| 不可重试错误 | 配置/权限/话题问题等 | 修配置；不要盲重试 |

**切记**：没有 Receipt ≠ 失败落盘；也 ≠ 成功。按「不确定」处理。

## 2. MessageID

Receipt 携带 `MessageID = (ledgerId, entryId[, batchIndex])`。  
业务日志建议记下 MessageID 与你的业务单号，便于对账。

## 3. 背压（pending）

SDK 维护 in-flight 窗口（`maxPending`）：

- 窗口满：阻塞 `Send` 或返回背压错误（可配）。  
- Receipt 归来后窗口释放。  
- Broker/存储变慢 → Receipt 变慢 → 自然限速。

配额耗尽（`QuotaExceeded`）：退避后重试或降速；见 [errors-and-retries](./errors-and-retries.md)。

## 4. 幂等生产（推荐生产环境开启）

```text
enable_idempotency = true
producer_name      = 稳定唯一名（如实例 ID，进程内稳定）
sequence_id        = 从 0 或续传点起单调递增（每条 Send 一个）
```

| 阶段 | 开启后承诺 |
|---|---|
| M2 | **同一 Owner 仍存活**时，超时重发同 seq → 同一 MessageID，不重复 Append |
| M3 | **跨 Owner 切主**后，对已确认 seq 仍可去重 |

未开启幂等 = 至少一次语义；重发可能重复。  
**不要**把「开了幂等」写成默认 exactly-once（见 [delivery-semantics](./delivery-semantics.md)）。

## 5. 分区与有序

- 单分区内：同一 Producer 的发送顺序在成功路径上保持。  
- 多分区：按路由键/分区策略并行；**跨分区不保证全局顺序**。  
- 需要按业务 key 有序并行：消费侧用 Key_Shared（[consume](./consume.md)）。

## 6. 与 Proxy / 切主

Owner 变更时可能收到 `NotOwner` / 超时。SDK 应 Lookup 后迁连再发。  
计划内运维 Drain 时窗口很短；崩溃切主时请依赖重试，不要人工「重启业务当修复」。
