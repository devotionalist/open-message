# Admin / 控制面 API（薄章）

上游：[05-routing-bundle.md](./05-routing-bundle.md)、[09-broker-lifecycle.md](./09-broker-lifecycle.md)、[07-multitenancy.md](./07-multitenancy.md)。

## 0. 为何合理

| 问 | 答 |
|---|---|
| 问题 | 运维/CI 需要受控 Unload、建 topic；不能靠 SSH 改 Meta |
| 选型 | 独立 HTTP Admin + M2 LoadManager gRPC；鉴权独立 |
| 放弃 | Admin 走 Client 热路径帧；只改本地图假迁主 |
| 代价 | 多一套 API 面 |
| 验收 | Unload 后旧主拒写；无 token 拒绝 |

## 1. 通道

| 通道 | 用途 |
|---|---|
| HTTP Admin（Broker 或独立 admin） | 运维、CI、控制台 |
| 内部 gRPC（**M2 必做**） | LoadManager ↔ Broker（[05](./05-routing-bundle.md) §6） |

**不**走 Client 二进制热路径帧（[21](./21-wire-protocol.md)）；Admin 鉴权独立（mTLS / token）。

## 2. 资源面（最小集）

| API | 语义 |
|---|---|
| `PUT /admin/v1/tenants/{t}` | 建租户 |
| `PUT .../namespaces/{ns}` | 策略：persistence E/Qw/Qa、retention、quota |
| `PUT .../topics/{topic}` | 建逻辑 topic / partitions |
| `DELETE .../topics/{topic}` | 关闭写 → 等 cursor → trim → 删 Index |
| `GET .../topics/{topic}/stats` | 吞吐、积压、owner |
| `GET .../brokers` | NodeStore 列表 |
| `POST .../bundles/{id}/unload` | 优雅 Unload |
| `POST .../bundles/{id}/load?broker=` | 指定迁入（可选） |
| `GET .../ledgers/orphans` | 协作 [22](./22-orphan-gc.md) 报告 |
| `POST .../ledgers/{id}/delete` | 运维强制删（危险；需二次确认） |

Lookup **不属于** Admin；Client/Proxy 走线协议 Lookup。

## 3. 与 Ownership

Unload/Load **必须**走 Ownership CAS + epoch；Admin 不得「只改本地图」宣称迁主成功。

## 4. 阶段

| 阶段 | 交付 |
|---|---|
| M1 | unload/load、topic CRUD 最小 HTTP |
| M2 | stats、配额、orphan/MarkOrphan、**bundle 密度**、LoadManager RPC |
| M3 | offload 触发、Standby assign/promote 查询 |

## 5. 验收

1. Unload 后旧 Owner 拒写；Lookup 指向新 Owner。  
2. 无 token 拒绝 Admin。  
3. 删 topic 不留下永远 OPEN 的 Index 尾。  
