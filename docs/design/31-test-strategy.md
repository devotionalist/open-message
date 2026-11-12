# 测试策略：单元 · 流程 · 集成 · 故障 · 压力

故障/压力落点：`test/chaos`、`test/opsdrill`、`test/stress`；剧本要求见下文与 [32](./32-operations.md)。  
部署面：[../deploy/local-k8s.md](../deploy/local-k8s.md)。  
工程四性：[30-engineering-quality.md](./30-engineering-quality.md)。

**目的**：用五层测试把设计不变量变成可失败断言；禁止「手动看一眼」代替门禁。

---

## 1. 分层总览

| 层级 | 英文 | 验什么 | 环境 | 门禁 |
|---|---|---|---|---|
| **单元测试** | Unit | 单包/单状态机正确性 | 本机；Fake Meta/OL | PR 必绿 |
| **流程测试** | Flow / Scenario | 多组件编排路径（无真实集群） | 本机进程内或轻量 compose | PR 核心路径；其余 Nightly |
| **集成测试** | Integration | 真实/近真实依赖联调 | 本地 K8s（hostPath）或 docker | M1+ 冒烟；里程碑全量 |
| **故障测试** | Fault / Chaos | 注入故障后 safety+liveness | K8s + FaultInjector | 见 C* 矩阵 |
| **压力测试** | Stress / Load | 吞吐、延迟、资源、背压、天花板 | 专用压测环境 | 里程碑基线；回归对比 |

```text
Unit → Flow → Integration → Fault
                ↘ Stress（可并行独立流水线）
```

下层失败不得用上层「碰巧绿」掩盖。

---

## 2. 单元测试（Unit）

### 2.1 范围

- `internal/metadata`、`ledger`（OL 适配）、`topic`、`broker` 子模块、`client` 协议编解码。  
- 表驱动；覆盖正常 + **负面**（CAS 冲突、Fence 后写、滚本步骤失败）。

### 2.2 规则

| 要求 | 禁止 |
|---|---|
| Fake/Memory Meta、Fake OL Client | 单测打真实外网/共享集群 |
| 可注入 Clock、Id 生成器 | 依赖墙钟 sleep 当同步原语（偶发） |
| 断言精确：版本、错误码、LAC、MessageID | 只 assert nil error |
| race（Go）关键包开启 | 忽略 data race |

### 2.3 最低覆盖主题

按包覆盖 `internal/*` 与 `test/*` 强制主题。另强制：

- 未达 Qa → 不 Receipt / 不推进 LAC  
- Cursor CasAdvance 禁止回退；Shared 跳洞 Cas 须失败（M3）  
- MarkOrphan / 滚本 Cas 幂等重入  

### 2.4 门禁

```text
go test ./internal/...
覆盖率：核心包 M1 ≥70%（metadata/topic/ledger 适配）
```

---

## 3. 流程测试（Flow）

### 3.1 定义

**不启完整 K8s**，但跨多个内部组件，按设计时序跑通端到端业务路径（可用 in-process Broker + Memory Meta + Memory/OL Fake）。

### 3.2 必测流程（与 [13](./13-flows-and-diagrams.md) 对齐）

| ID | 流程 | 断言要点 |
|---|---|---|
| F-P1 | Lookup → Produce → Receipt | MessageID 单调；Qa 语义（Fake 可配） |
| F-C1 | Subscribe → Flow → Message → AckCumulative | Cursor 前移；重启后续读 |
| F-U1 | Drain → Unload → 他者 Acquire → Produce | 旧节点 NotOwner；新节点可写 |
| F-R1 | 杀 Writer 语义 → Recover → 新 ledger → 续写 | 已 Receipt 可读；尾唯一 |
| F-O1 | 滚本三步（Close/Create/Index Cas） | Index 唯一 open；步骤失败可补偿 |
| F-I1 | 同 Owner 幂等重发（M2） | 同 seq 同 MessageID |
| F-D1 | Exclusive 切换 / Failover（按里程碑） | 单活；cursor 一份 |

### 3.3 门禁

- PR：F-P1、F-C1、F-U1 冒烟。  
- 里程碑：上表范围内全绿。

---

## 4. 集成测试（Integration）

### 4.1 定义

真实（或契约级真实）依赖：**Oxia 或 OpenKV**、**openledgerd**、多 Broker、可选 Proxy；部署在 **本地 K8s + om-hostpath**。

### 4.2 场景

| ID | 场景 | 通过标准 |
|---|---|---|
| I1 | 双 Broker 集群 Produce/Consume | 端到端收发稳定 ≥1–5 min |
| I2 | 经 Proxy 收发 | Client 不感知 Broker 地址 |
| I3 | Meta 后端切换配置（若里程碑含） | 同用例绿 |
| I4 | 多 Bookie Qa≥2 写读 | 杀一副本后已确认仍可读（接故障测） |
| I5 | Admin Unload | 与设计 25/09 一致 |

### 4.3 规则

- 清单与脚本可重复；禁止「手工点集群」。  
- hostPath **不能**代替多故障域耐久证明——报告须注明局限。

### 4.4 门禁

- M1：I1 冒烟。  
- M2+：I1–I2；I4 与 C6 联动。

---

## 5. 故障测试（Fault / Chaos）

### 5.1 定义

在**持续流量**下注入故障，验证 safety（不丢已确认、无双写）与 liveness（恢复后可服务）。

### 5.2 脚手架

`TrafficGenerator` + `Verifier` + `FaultInjector` + `ScenarioRunner`（见 `test/chaos`、`cmd/faultdrill`）。

### 5.3 矩阵

权威场景：本文故障层 + [27](./27-dual-meta-ops.md) 双 Meta 分面；实现落 `test/chaos`。

| 补充断言 | 说明 |
|---|---|
| 幂等 | C* 中 M2：同 Owner；跨 kill 不重复仅 M3 |
| Orphan | 打断滚本步骤 3 → MarkOrphan；挂链本不被 GC |
| 早 Promote | 租约墙钟未过 + 探测成立 → 升主；旧主停写（M3） |

### 5.4 Verifier（硬规则）

1. **Safety**：所有 SendReceipt ⊆ 故障后可读集合。  
2. **Liveness**：故障结束后 T 秒内成功率恢复阈值。  
3. **Fence**：诈尸零双成功 Receipt。  
4. **Accepted risk**：未 Receipt 可丢或重复——不得当失败，除非声称更强。

### 5.5 门禁

- PR：C1、C3、C5 冒烟（可缩短）。  
- Nightly：全矩阵（含已启用的 C13–C15）。  
- 里程碑退出：对应 ★/◆ 全绿或书面豁免。

---

## 6. 压力测试（Stress / Load）

### 6.1 定义

在目标拓扑上加压，验证**性能与背压行为**符合 [30](./30-engineering-quality.md) / 设计天花板，而非「打挂即过」。

### 6.2 画像

| 维度 | 建议 |
|---|---|
| 单 partition 写 | 找饱和点：p99 延迟、Broker CPU、Bookie journal |
| 多 partition | 验证近似线性（受 bundle/Broker 数限制） |
| 慢消费 | permits=0；积压；内存不 OOM（有界） |
| Meta | 消息 QPS ↑ 时 Meta Cas QPS **不**同比线性（合并刷） |
| Proxy | 连接数与 Lookup 刷新；无惊群打满 Meta |
| 长稳 | ≥30–120 min；无 FD/泄漏单调涨 |

### 6.3 通过标准（须记基线）

```text
记录：版本、拓扑、Qa、消息大小、并发
指标：吞吐、p50/p99、错误率、CPU/RSS、Meta Cas QPS、underreplicated
回归：同拓扑下相对基线退化超阈值（如 p99 +50% 且无说明）→ 失败
```

### 6.4 禁止

- 用压测掩盖正确性（压测中也须抽检 Receipt ⊆ 可读）。  
- 把单 partition 打满解释为「集群不能扩展」（须同时跑多 partition 对照）。  
- 无资源上限的「能跑多久算多久」而无泄漏断言。

### 6.5 门禁

- M1：单节点基线记录（本地 `tmp/ops-records/` 或 CI artifact，不进 docs）。  
- M2+：多 Broker + Proxy 基线；慢消费背压用例。  
- 发布：对比上一基线。

---

## 7. 场景 ID 命名与映射

```text
U-*   单元（包内）
F-*   流程
I-*   集成
C-*   故障（与本文 / test/chaos 统一）
S-*   压力
O-*   运维演练（运维角主签；测试可自动化抽检）
```

每条设计验收项须映射到至少一 ID；维护表（可放 `test/README` 或 plan 附录）。

---

## 8. 与角色握手

| 角色 | 测试如何卡住 |
|---|---|
| 架构 | 不可测 / 断言歧义 → 缺口单 |
| 开发 | 缺单测补偿路径 → 功能未完成 |
| 运维 | 演练≠只 kill → 改 O* |
| 客户 | 示例路径进 F-*/I* |
| 审计 | Critical 无回归用例 → 门禁风险 |

生产级测试门禁见 [36](./36-production-grade-bar.md)。

---

## 9. 相关文档

| 文档 | 用途 |
|---|---|
| [12](./12-cross-failure-scenarios.md) | 组合故障语义 |
| [13](./13-flows-and-diagrams.md) | 流程金样 |
| [15](./15-availability-failover-window.md) | 可用性窗口断言边界 |
| [23](./23-producer-idempotency.md) | 幂等阶段勿测错 |
| [30](./30-engineering-quality.md) | 性能/HA 实现侧 |
| [32](./32-operations.md) | 运维规格；O* 与 C* 分工 |
| [../ops/README.md](../ops/README.md) | Runbook 落盘 |
| [36-production-grade-bar.md](./36-production-grade-bar.md) | 里程碑/生产级门禁 |
