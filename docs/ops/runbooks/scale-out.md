# Runbook：扩容

规格：[../../design/32-operations.md](../../design/32-operations.md) §4。演练：O2/O4/O5/O6。

## 1. 症状

CPU/延迟高、bundles 过密、Bookie 盘满、Meta 热点。

## 2. 影响面

变更窗口可能出现 NotOwner/短暂 Unavailable（有重试则业务可耐受）。

## 3. 隔离

- 一次只扩一面（先定 Broker / Bookie / Meta / Proxy）。  
- 禁止为扩容搬历史 ledger。

## 4. 步骤（按面选）

**Broker**：加节点 → 注册 → Unload 最重 bundle → 验证流量。  
**Bookie**：注册 → 确认新 Create 可落到新节点。  
**Meta**：按后端 split；观察 ErrRetry。  
**Proxy**：加副本 → LB。  
**Partition/Bundle**：按 Admin/设计加分区或 split。

## 5. 验证

- 目标负载下降或并行度上升；无丢已确认；P99 可接受。

## 6. 事后

- 更新容量基线；若超密度告警未清继续迁。
