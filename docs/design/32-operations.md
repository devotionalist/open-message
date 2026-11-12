# 运维规格：安装 · 部署 · 扩缩容 · 演练 · 可观测 · 恢复

Runbook 落盘目录：[../ops/README.md](../ops/README.md)。  
演练要求见本文 §6；故障场景对照 [31](./31-test-strategy.md)。原始输出写本地 `tmp/ops-records/`，不进 docs。  
本地测试拓扑：[../deploy/local-k8s.md](../deploy/local-k8s.md)（**非生产等价**）。

**目的**：把 HA/自愈设计变成值班可执行步骤；无手册 / 无指标 / 无 Admin 不得宣称「生产可运维」。

---

## 1. 能力总览

| 能力 | 要解决什么 | 主要手段 | 关联设计 |
|---|---|---|---|
| **安装** | 一次性备齐依赖与配置约定 | 清单、镜像、StorageClass、前缀隔离 | deploy、02、19、27 |
| **部署** | 把集群带到可服务状态 | 组件序、健康检查、冒烟收发 | 09、14、25 |
| **扩容** | 抬高并发且不搬历史消息体 | 加 Broker/Bookie/Meta shard/Proxy | 00 §5、05、architecture §6 |
| **缩容** | 缩小规模无野主、无丢已确认 | 先迁走再下线；隔离组下限 | 05、07、09 |
| **故障演练** | 证明人+系统能恢复 | 剧本 + 持续流量 | 15、16、31 |
| **可观测性** | 先于用户发现问题 | 指标/日志/追踪/告警→runbook | 15 §7、27、30 |
| **故障恢复** | 检测→隔离→收敛→续服 | Runbook + Admin + 自愈路径 | 00 §4、12、27、28 §3.6 |
| **备份恢复** | DC/误删后回到一致点 | 双 Meta + 数据联合恢复 | 27、24 |
| **变更升级** | 滚动无长窗 | Drain 序、版本 skew、回滚 | 09、15、27 |

---

## 2. 安装（Install）

### 2.1 交付物

| 项 | 要求 |
|---|---|
| 组件清单 | Proxy、Broker、openledgerd、AR sidecar、OM Meta（Oxia/OpenKV）、OL Meta（可同集群不同前缀）、可选 Admin |
| 镜像/版本 | 发布说明含 OM↔OL 兼容矩阵 |
| 存储 | Bookie/Meta 需要持久卷；Broker **无**消息权威盘 |
| 网络 | Broker↔Bookie、Broker↔Meta、Client↔Proxy；端口约定 |
| 配置模板 | `METADATA_BACKEND`、OL 地址、E/Qw/Qa 默认、租约 TTL/renew |
| 密钥 | Admin/Client token 或 mTLS；轮换规程入口 |

### 2.2 安装检查单

```text
[ ] StorageClass / PVC 约定明确（测试：om-hostpath；生产：另册）
[ ] OM/OL Meta key 前缀隔离（/open-message vs /open-ledger）
[ ] 镜像版本写入发布记录
[ ] 最小拓扑：Proxy×2、Broker×2、Bookie×(Qa 要求)、Meta 法定人数
```

---

## 3. 部署（Deploy）

### 3.1 建议拉起顺序

```text
1. Meta（Oxia/OpenKV）健康
2. openledgerd（+ AR）注册到 OL Meta
3. Broker 注册 NodeStore；探活 Meta + OL
4. Proxy；指向 Broker 服务发现
5. 冒烟：Lookup → Produce → Consume → Ack
6. 对账指标：index_ledger_missing_ol = 0（若已部署任务）
```

### 3.2 健康与就绪

| 组件 | Ready 条件（概念） |
|---|---|
| Meta | 法定人数；读写探活成功 |
| Bookie | cookie/盘可写；gRPC 通；registry 可见 |
| Broker | Meta+OL 可连；无「半加载对外写」 |
| Proxy | 上游可连；帧校验开 |

### 3.3 禁止

- 未 Ready 的 Broker 接入 Proxy 流量。  
- 生产用 local-k8s/hostPath 文档冒充多故障域。  

---

## 4. 扩容（Scale-out）

| 面 | 动作 | 预期 | 不做什么 |
|---|---|---|---|
| **Broker** | 加节点 → LoadManager/人工 Unload 迁 bundle | 并行度随 bundle 升；RTO 秒～数十秒 | 搬 ledger 字节 |
| **Bookie** | 注册新 bookie → 新 ledger/fragment 可用 | 历史 CLOSED 默认不搬 | 期望旧热写立刻打满新盘 |
| **Meta** | 加 shard / split | 异 key Cas 吞吐升；迁移键短暂 ErrRetry | 全表扫描 Lookup |
| **Proxy** | 加副本 | Client LB；注意 Lookup 缓存 | 无 drain 直接砍实例当常态 |
| **分区/Bundle** | 加 partition / split bundle | 写/消费并行 | 瞬时删旧 partition 数据 |

细则：[05](./05-routing-bundle.md)、architecture §6。演练：**O2、O4、O5、O6**。

---

## 5. 缩容（Scale-in）

| 面 | 必须顺序 | 护栏 |
|---|---|---|
| **Broker** | 迁空 bundle → Drain → Unload → 下线 | `max_bundles` 不因缩容被突破到单点过密；无野主 |
| **Bookie** | 确认无 underreplicated 依赖 / AR 完成 → 摘注册 → 下线 | Qa 耐久；勿在副本不足时摘盘 |
| **Proxy** | drain 连接 → 摘 LB → 下线 | 防惊群 |
| **隔离组** | 存活 ≥ `min_brokers_in_isolation_group` / `min_bookies_in_isolation_group` | 否则拒缩容（[07 §4.1](./07-multitenancy.md)） |

演练：**O3**。缩容失败标准：丢已确认、双主、组内低于下限。

---

## 6. 故障演练（Ops Drill）

### 6.1 与 Chaos 区别

| | 故障测试 C* | 运维演练 O* |
|---|---|---|
| 主角 | 自动化注入 + Verifier | **人按 runbook**（可脚本辅助） |
| 目的 | 系统行为符合设计 | 值班可执行、步骤无歧义 |
| 流量 | 通常持续 | **必须持续**（不停 TrafficGenerator） |

### 6.2 剧本

权威要求：本文 §6 + [ops/README](../ops/README.md) runbook；含双 Meta / Backup。

### 6.3 报告字段

时间线、动作执行人、指标截图、是否达标、问题单、是否更新 runbook。

---

## 7. 可观测性（Observability）

### 7.1 三大支柱

| 支柱 | 最低要求 |
|---|---|
| **指标** | RED（Rate/Errors/Duration）+ 控制面 + 存储 |
| **日志** | 结构化；含 requestId/topic/bundle/epoch；禁止热路径打 payload |
| **追踪** | Proxy→Broker→OL Client 关键路径可关联（按里程碑） |

### 7.2 指标目录（最低集）

| 域 | 指标（名可调，语义不可缺） |
|---|---|
| 数据面 | produce/consume QPS、错误率（按 ErrorCode）、p99 |
| 切主 | `failover_duration_seconds`、`not_owner_total`、`lease_renew_fail` |
| Cursor | 刷盘滞后、Cas 冲突 |
| Meta | Cas QPS、ErrShardMoving、探活失败 |
| OL/Bookie | Append 延迟、Fenced、underreplicated |
| 对账 | `index_ledger_missing_ol`、`orphan_pending`/`orphan_deleted` |
| Proxy | Lookup 刷新、上游失败、连接数 |
| Load | bundles/broker、超密度告警 |

### 7.3 告警

每条告警必须：**级别、阈值、持续时长、链接 runbook、升级人**。  
无 runbook 的告警不得进生产值班表。

---

## 8. 故障恢复（Recovery）

### 8.1 通用流程

```text
检测（告警/探活）→ 隔离（禁写/摘流/禁止乱删）→ 收敛（自愈或手册）→ 续服验证 → 事后复盘
```

对照 [00 §4](./00-principles.md)。

### 8.2 Runbook 矩阵（必须成文）

| Runbook | 触发 | 核心动作 |
|---|---|---|
| `om-meta-outage` | OM Meta 不可用 | 停依赖 Cas 的变更；恢复法定人数；禁止假装切主完成 |
| `ol-meta-outage` | OL Meta 不可用 | 禁滚本/Recover；已 OPEN 谨慎；恢复后对账 |
| `dual-meta-outage` | 双挂 | 控制面停；租约失效后数据面停写；联合恢复序 |
| `broker-drain-unload` | 变更/热点 | Drain→Unload→Acquire |
| `standby-promote` | 热备升主 | 探测谓词→Promote→Fence；禁跳 Fence |
| `owner-crash` | 冷切主 | 等探测/Acquire；观察 failover_duration |
| `bookie-down` | 丢盘/下线 | AR/underreplicated；Qa 风险 |
| `orphan-gc` | 磁盘泄漏 | MarkOrphan；禁抽样删 |
| `trim` / `offload` | 空间/冷层 | 状态机顺序；禁未校验删热 |
| `backup-restore` | DR | 见 §9 |

每本结构见 [ops/README](../ops/README.md)。

### 8.3 禁止动作（全局）

- 未 Fence 完成对外成功写。  
- SSH 改 Bookie 盘冒充 Recover。  
- 单面恢复 Meta 当完整 DR。  
- 抽样 Index 导出做 orphan 删除。  

---

## 9. 备份与恢复（Backup / Restore）

| 对象 | 备份 | 恢复注意 |
|---|---|---|
| OM Meta | 快照/导出（Ownership/Index/Cursor/Node） | 与 OL 世代对齐 |
| OL Meta | LedgerMeta/registry/AR 队列 | 与 Bookie 字节一致 |
| Bookie 盘 / 冷对象 | 卷快照或对象版本 | Offload 状态机 |
| 配置/密钥 | 独立保管 | 轮换 |

**联合恢复序（原则）**：先 Meta 法定人数与版本兼容 → 对账 Index↔OL → 再挂流量；禁止只恢复一套 Meta。  
演练：**O14**；设计缺口见审计 28 运维维。

---

## 10. 变更与滚动升级

```text
1. 读兼容矩阵（OM↔OL↔Meta）
2. Proxy drain / 扩容
3. Broker：Drain → Unload → 滚动（禁默认 kill -9）
4. openledgerd / AR（观察 underreplicated）
5. Meta 后端（最后或按厂商规程；窗口单独公告）
回滚：写成功率崩、对账红、切主风暴 → 停滚、回上一版本、开事故单
```

---

## 11. 里程碑与生产宣称

| 阶段 | 运维最低交付 |
|---|---|
| M1 | 安装/部署文档；O1–O3；基础指标；Drain runbook |
| M2 | 扩缩容 O2–O6；告警→runbook；双 Meta 对账指标；O11–O12 草案 |
| M3 | Standby/AR/Offload runbook；Backup-Restore；O10/O13/O14；隔离组 min capacity 执行 |

**生产 HA**：Qa≥2 + AR SLO + 备份可演练 + 隔离组护栏（与 [28 §7](./28-design-audit-requirements.md) 一致）。

---

## 12. 相关文档

| 文档 | 用途 |
|---|---|
| [../ops/README.md](../ops/README.md) | Runbook 目录 |
| [25](./25-admin-api.md) | 运维 API |
| [27](./27-dual-meta-ops.md) | 双 Meta |
| [15](./15-availability-failover-window.md) / [16](./16-warm-standby.md) | 可用性/热备 |
| [05](./05-routing-bundle.md) | LoadManager 扩缩 |
| [31](./31-test-strategy.md) | 与 Chaos 分工 |
| [00](./00-principles.md) | 自愈分层对照 |
