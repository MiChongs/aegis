# deploy/

| 目录 | 用途 |
|---|---|
| `swarm/` | **生产**：Docker Swarm 统一栈（`aegis-stack.yml`）、配置模板（`cluster.env.example`）、Traefik 路由模板。用 `aegisctl`（`cmd/aegisctl`）操作，手册见 [docs/distributed-deploy.md](../docs/distributed-deploy.md) |
| `docker/` | 各服务的 Dockerfile（统一栈与 CI 都用它们），以及本机开发用的 `docker-compose.yml` 与一键脚本 |
| `dokploy/` | 旧的 HK Dokploy 编排（`risk-stack.yml`、`music-stack.yml`）。切到统一栈后只作回滚保留，新改动不再写进这里 |
| `linux/`、`windows/` | 单机直接跑二进制的脚本 |

常用：

```bash
go build -o ~/.local/bin/aegisctl ./cmd/aegisctl
aegisctl status          # 集群总览
aegisctl deploy          # 发布（先迁移、后滚动更新，失败自动回滚）
aegisctl doctor          # 体检
```
