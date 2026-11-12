# Runbook：安装与首次部署

规格：[../../design/32-operations.md](../../design/32-operations.md) §2–§3。

## 1. 症状与指标

新环境；或大版本重装。

## 2. 影响面

全站不可用直至冒烟通过。

## 3. 立即隔离

- 未完成冒烟前勿切生产流量。  
- 禁止跳过 Meta/Bookie Ready。

## 4. 步骤

1. 按 32 §2 安装检查单备齐镜像、盘、前缀、密钥。  
2. 按 32 §3.1 顺序拉起 Meta → openledgerd → Broker → Proxy。  
3. 确认各组件 Ready。  
4. 冒烟：Lookup → Produce → Consume → Ack。  
5. 确认基础指标可见（见 [../observability.md](../observability.md)）。

## 5. 验证

- 端到端收发成功；`index_ledger_missing_ol`（若有）为 0。

## 6. 事后

- 记录版本与拓扑到本地运维台账或 `tmp/ops-records/`（不进 docs）。
