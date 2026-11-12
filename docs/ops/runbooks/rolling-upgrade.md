# Runbook：滚动升级

规格：[../../design/32-operations.md](../../design/32-operations.md) §10。

## 1. 症状

计划发版。

## 2. 影响面

单节点 Drain 窗口；协议/Schema 不兼容则全局风险。

## 3. 立即隔离

- 确认版本兼容矩阵（Broker↔OL↔Meta↔Proxy）。  
- 不兼容：维护窗口，勿混跑。

## 4. 步骤

1. 金丝雀一台：Drain → 升 → 回归。  
2. 按面滚动：Proxy → Broker → openledgerd →（Meta 按产品）。  
3. 每批观察错误率/failover。  
4. 失败：**回滚该面**，勿强推。

## 5. 验证

- 全版本一致；冒烟；无双主/对账告警。

## 6. 事后

- 变更记录；兼容矩阵更新。
