# Zeabur 部署

> 一个仓库、两个服务。本文只讲**怎么让它们互不牵连**——
> 其余通用部署问题看 Zeabur 官方文档。

## 两个服务的边界

| 服务 | 构建根 | 构建方式 | 该被什么改动触发 |
|---|---|---|---|
| `aegis-api-git` | 仓库根 | `deploy/docker/Dockerfile` | Go 代码、`migrations/`、`deploy/` |
| `aegis-console-git` | `aegis-console` | zbpack 自动识别 Next.js | `aegis-console/` |

**后端镜像里没有、也不该有前端。** 两处保证这一点：

1. `.dockerignore` 排除 `aegis-console`、`node_modules`、`.next`——
   Dockerfile 里的 `COPY . .` 因此看不到前端源码。
2. 后端 Dockerfile 只有 Go 构建阶段，没有任何 `npm` / `pnpm` 调用。

前端由 `aegis-console-git` 独立构建，产物也独立。

## 客户端 IP：不用配

Zeabur 的入口网关在集群内网，业务容器看到的直连对端是一个 `10.x` / `100.64.x` 地址。
后端默认就把这些网段算作受信代理，并从 `X-Forwarded-For` 里取真实客户端 ——
`auto` 档还会认出 `ZEABUR_SERVICE_ID` 并补上 CDN 边缘网段，
所以 `TRUSTED_PROXIES` 留空即可。

判定结果不对时，设 `CLIENT_IP_DEBUG_HEADER=true` 后 curl 一次就能看到结论与全部依据，
详见 [client-ip.md](client-ip.md)。

## `zbpack.json`：把构建计划钉在仓库里

仓库根有一个 `zbpack.json`：

```json
{ "dockerfile": { "path": "deploy/docker/Dockerfile" } }
```

**这一行解决的是一个真实的失败**：Git 构建以仓库根为构建根，而根目录没有
`Dockerfile`，zbpack 会按 `go` 规划然后失败。此前的绕法是在服务上设
`ZBPACK_DOCKERFILE_PATH=deploy/docker/Dockerfile` 这个环境变量 —— 但那是
**服务级**的，换个服务、重建一次服务就没了，而且从仓库里完全看不出来。
放进 `zbpack.json` 之后它跟着代码走，新建的服务开箱即用。

`aegis-console-git` 的构建根是 `aegis-console`，读不到根目录这份配置，
因此不受影响，继续走 zbpack 对 Next.js 的自动识别。

## Watch Paths：**CLI 设不了，只能在 Dashboard 配**

这是目前唯一必须手工做的一步，也是「改前端却重建了后端」的根因。

```
$ zeabur service update --help
Available Commands:
  tag   Update image tag of a prebuilt service      ← 只有这一个
```

`zeabur template deploy` 的 spec 里**有** `watchPaths` 字段
（`https://schema.zeabur.app/prebuilt.json` → `definitions.Git.properties.watchPaths`），
但实测下发后服务上仍然是 `["*"]`——平台侧没有采纳。因此：

| | 能不能用 CLI |
|---|---|
| 读当前值 | 能：`zeabur service get --id <svc> --json` 看 `WatchPaths` |
| 写 | **不能**，去 Dashboard → 服务 → Settings → Watch Paths |

**填进哪个框很重要**：是服务 Settings 里的 **Watch Paths**，一行一条。
这几行**不是 Dockerfile 路径、不是 Dockerfile 内容**——填错框的表现是构建期
`dockerfile parse error on line 1: unknown instruction: /aegis-console/**`，
因为 Zeabur 把那几行当 Dockerfile 解析了。`aegis-console-git` 走 zbpack 对
Next.js 的自动识别，它**根本不该有任何 Dockerfile 设置**。

要配的值（gitignore 语法，**顺序有意义**）：

```
# aegis-api-git —— 先全收，再把前端排除掉
/**
!/aegis-console/**

# aegis-console-git —— 只收前端
/aegis-console/**
```

**顺序不能反。** gitignore 是「最后一条命中的规则说了算」，写成

```
!/aegis-console/**
/**            ← 它在后面，把前端又收回来了
```

的话，排除完全不生效：改前端照样重建后端，而且从设置界面上看不出问题
（两条规则都在，只是次序让第一条失效）。

配完用 CLI 核对：

```bash
zeabur service get --id 6a7c06562d4cb87f2ba38eb8 --json | jq '{Name,RootDirectory,WatchPaths}'
zeabur service get --id 6a7c0f012d4cb87f2ba39383 --json | jq '{Name,RootDirectory,WatchPaths}'
```

## 常用 CLI 速查

```bash
zeabur project list --json                       # 项目与 ID
zeabur service list --project-id <proj> --json   # 服务清单
zeabur service get --id <svc> --json             # 构建根 / Watch Paths / 自定义命令
zeabur variable list --id <svc>                  # 环境变量（含只读的服务间注入）
zeabur deployment list --service-id <svc>        # 部署历史与 PLANTYPE
zeabur service redeploy --id <svc>               # 重新部署
zeabur service exec --id <svc> -- sh -c "wget -qO- http://127.0.0.1:8088/healthz"
```

两个已知的接口抖动，都按「重试」处理，不是配置问题：

- `deployment log --type build/runtime` 会随机 `unexpected EOF`
- 构建机拉 GitHub 会超时（`fetch git ref: dial tcp ...: i/o timeout`）。
  **失败的那条 deployment 仍会显示正确的 COMMITSHA 与 PLANTYPE**，
  别据此以为构建成功了 —— 以 `deployment list` 的状态列为准。

`zeabur service exec` 对 PREBUILT 的数据库服务不通（`unexpected EOF`），
对 aegis-api 这类自建容器可用。

## 校验构建计划是否正确

`deployment list` 的 `PLANTYPE` 列要是 **`docker`**。落成 `go` 就说明
`zbpack.json` 没被读到（多半是构建根不是仓库根），此时构建必然失败。
