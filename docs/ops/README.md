# 运维手册入口

**层属 L5。** 文档总入口：[../README.md](../README.md)。  
规格（L4）：[../design/32-operations.md](../design/32-operations.md)。  
测试拓扑（非生产）：[../deploy/local-k8s.md](../deploy/local-k8s.md)。

## Runbook 目录（须按 32 / operator 结构填写）

落盘建议路径：`docs/ops/runbooks/<name>.md`。

| Runbook | 状态 | 说明 |
|---|---|---|
| [runbooks/install-deploy.md](./runbooks/install-deploy.md) | 骨架 | 安装与首次部署 |
| [runbooks/scale-out.md](./runbooks/scale-out.md) | 骨架 | Broker/Bookie/Meta/Proxy 扩容 |
| [runbooks/scale-in.md](./runbooks/scale-in.md) | 骨架 | 缩容顺序与护栏 |
| [runbooks/om-meta-outage.md](./runbooks/om-meta-outage.md) | 骨架 | OM Meta 故障 |
| [runbooks/ol-meta-outage.md](./runbooks/ol-meta-outage.md) | 骨架 | OL Meta 故障 |
| [runbooks/dual-meta-outage.md](./runbooks/dual-meta-outage.md) | 骨架 | 双 Meta |
| [runbooks/broker-drain-unload.md](./runbooks/broker-drain-unload.md) | 骨架 | Drain/Unload |
| [runbooks/standby-promote.md](./runbooks/standby-promote.md) | 骨架 | 热备升主 |
| [runbooks/owner-crash.md](./runbooks/owner-crash.md) | 骨架 | Owner 崩溃冷切 |
| [runbooks/bookie-down.md](./runbooks/bookie-down.md) | 骨架 | Bookie 故障 |
| [runbooks/orphan-gc.md](./runbooks/orphan-gc.md) | 骨架 | Orphan |
| [runbooks/trim-offload.md](./runbooks/trim-offload.md) | 骨架 | Trim / Offload |
| [runbooks/backup-restore.md](./runbooks/backup-restore.md) | 骨架 | 备份恢复 |
| [runbooks/rolling-upgrade.md](./runbooks/rolling-upgrade.md) | 骨架 | 滚动升级 |
| [observability.md](./observability.md) | 骨架 | 指标与告警目录 |

演练原始输出默认：`tmp/ops-records/`（gitignore，不进文档树）。结论若需固化，写入对应 runbook / [observability](./observability.md)。

**规则**：告警必须链到上表某一本；无 runbook 不得进生产值班表。  
**生产级**：骨架可占位；对应能力进里程碑退出 / 生产 HA 前须升到可照做（[../design/36-production-grade-bar.md](../design/36-production-grade-bar.md) §3）。
