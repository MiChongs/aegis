# aegis-mcp

Aegis 管理端的 MCP 服务（Python，基于 [FastMCP](https://gofastmcp.com) 4）。AI 助手（Claude Desktop、Claude Code、Cursor 等）凭 **MCP 密钥**
以签发它的管理员身份查询与管理 Aegis：应用、用户、会员、积分、工单、内容、审计……

后端的密钥模型与判定规则见 [docs/mcp.md](../docs/mcp.md)。

## 1. 签发密钥

控制台 → 个人资料 → **MCP 密钥** → 签发密钥。默认只读、全部查看权限、90 天有效。
明文只显示一次，签发弹窗里同时给出照抄即可用的客户端配置。

密钥能做的 = 你此刻的权限 ∩ 密钥范围（权限点、应用、只读、来源 IP、每分钟次数）。

## 2. 接入客户端

需要 [uv](https://docs.astral.sh/uv/)。

**JSON 配置**（Claude Desktop、Cursor、Cherry Studio 等）：

```json
{
  "mcpServers": {
    "aegis": {
      "command": "uvx",
      "args": ["--from", "git+https://github.com/MiChongs/aegis#subdirectory=mcp", "aegis-mcp"],
      "env": {
        "AEGIS_BASE_URL": "https://aegis.example.com",
        "AEGIS_MCP_KEY": "aegis_mcp_..."
      }
    }
  }
}
```

仓库是私有的、或想用本地代码时，把 `--from` 换成本地路径：`"args": ["--from", "/path/to/aegis/mcp", "aegis-mcp"]`。

**Claude Code**：

```bash
claude mcp add aegis -e AEGIS_BASE_URL=https://aegis.example.com -e AEGIS_MCP_KEY=aegis_mcp_... -- uvx --from "git+https://github.com/MiChongs/aegis#subdirectory=mcp" aegis-mcp
```

`AEGIS_BASE_URL` 填后端地址或控制台地址都可以（控制台会把 `/api` 与 `/openapi.json` 反代到后端）。

## 3. HTTP 模式（团队共用一个服务）

```bash
AEGIS_BASE_URL=https://aegis.example.com aegis-mcp --transport http --host 0.0.0.0 --port 8765
```

每个客户端在请求头里带自己的密钥：`Authorization: Bearer aegis_mcp_...`（也认 `X-Aegis-MCP-Key`）。
设置了 `AEGIS_MCP_KEY` 时它是没带请求头的客户端的缺省密钥，对外暴露时不要设。
服务本身不做 TLS，放在反向代理后面。也支持 `--transport sse`。

## 4. 命令行开关

| 开关 | 作用 |
|---|---|
| `--read-only` | 只暴露只读工具（与密钥是否只读无关，双保险） |
| `--modules users,vip,lottery` | 只挂载这些模块（通用层恒挂载）；`--exclude-modules` 反之 |
| `--tool-search` | 只下发 `search_tools` / `run_tool` 与通用层，其余四百多个工具按需检索（BM25）后调用。客户端有工具数量上限或上下文紧张时用 |
| `--list-modules` | 列出可用模块 |

## 环境变量

| 变量 | 说明 |
|---|---|
| `AEGIS_BASE_URL` | 必填，Aegis 根地址 |
| `AEGIS_MCP_KEY` | stdio 模式必填，`aegis_mcp_` 开头 |
| `AEGIS_TIMEOUT` | 单次请求超时秒数，默认 30 |
| `AEGIS_MAX_RESULT_CHARS` | 单个工具结果的最大字符数，超出截断，默认 60000 |
| `AEGIS_VERIFY_TLS` | 设为 `0` 跳过证书校验（仅自签名测试环境） |

## 工具

共 432 个，按领域分 25 个模块，工具名前缀即模块：完整清单见 [TOOLS.md](TOOLS.md)（脚本生成）。

| 模块 | 覆盖 |
|---|---|
| 通用层 | `whoami`、`list_endpoints`、`describe_endpoint`、`call_api`：自述、接口目录（逐条标出能否调用）、参数说明、调用任意管理端接口——没有专门工具的接口（存储、AI、邮件、组织架构、远程函数等）走这里 |
| `apps_` | 应用列表、详情、新建修改删除、开关、登录策略、统计、工作台、治理视图 |
| `users_` | 检索、详情、资料、启停（含批量）、删除、重置密码、会话、封禁（含批量）、OAuth2 授权、登录绑定、账号注销全流程、设备数控制、用户云存储 |
| `identity_` | 用户主档：跨应用身份、映射与同步、标签、分群、黑白名单、合并、申诉、注销、用户设置维护 |
| `audit_` / `risk_` | 登录与会话审计、平台审计日志、崩溃报告；风控规则 / 策略 / 名单 / 评估 / 复核 / 画像 / 模板、IP 与地域封禁、地理围栏、防火墙日志 |
| `points_` / `wallet_` / `payment_` | 积分与经验概况、流水、调整、批量、冲正；钱包与流水、余额调整；订单、退款、凭证、支付渠道配置、交易设置 |
| `store_` / `cardkey_` / `invite_` / `signin_` / `leaderboard_` | 积分商城、卡密、邀请、签到与奖励策略试算、自动签到、排行榜与结算 |
| `vip_` / `features_` / `ads_` / `lottery_` | 会员套餐、功能标识、发放（可预览）与收回、权益、流水、试用、折算策略；功能解锁；激励广告与广告服务策略；抽奖活动、奖品、出奖模拟、审计、记录与人工奖品发放 |
| `grant_` | 物品发放：目录与模板、画布规则（完整画布或表单直线画布）新建修改启停删除、校验与预演（单人追踪 / 抽样估算）、立即执行与取消、执行记录、发放记录、人工物品处理与失败重试、总览 |
| `egress_` | 出海网关（限超管）：总览、运行态（线路 TCP / UDP 健康）、画布按名字编辑（规则链增改移删、命中去向、网关设置、线路、mihomo 节点 / 策略组 / 订阅 / 分流 / 核心、连线）、校验、路由解释、TCP / UDP 自测、探测、导入 Clash / mihomo 配置与分享链接、导出 YAML、应用、版本与回滚；mihomo 核心状态、策略组测速与切换、订阅与规则集更新、连接、日志、DNS、安装与启停。编辑只改草稿，`egress_apply` 才生效 |
| `content_` / `tickets_` / `releases_` / `reports_` / `admins_` | 公告、Banner、站内信、平台公告；工单与配置；发布中心（灰度、定向、命中模拟）；报表中心；管理员、角色、授权策略与判定解释、临时权限、委托、会话、在线 |

工具都带 MCP 注解：只读工具 `read_only_hint`，不可逆或影响面大的写工具 `destructive_hint`，客户端据此要求确认。
后端「整体覆盖」的保存接口（设备数策略、折算策略、抽奖活动、发放规则、签到策略……）在工具里一律先读后合并，只传要改的字段即可。
所有调用记入 Aegis 审计日志，会话列为 `mcp:<密钥前缀>`，可用 `audit_logs(session_id=...)` 回查。

## 开发

```
src/aegis_mcp/
  core/        配置、HTTP 客户端、运行时（按请求解析密钥）、Route 接口声明、参数类型与 READ/WRITE/DESTRUCTIVE
  modules/     每个领域一个文件：NAMESPACE + server（FastMCP 子服务），自动发现挂载
  server.py    组装：中间件（错误、耗时、调用日志、结果截断）、挂载、只读过滤、检索模式
```

新增工具：在对应模块顶部用 `GET("/api/admin/...")` 等声明接口（模板与后端路由表一字不差），
`@server.tool(**READ("模块名"))` 写工具函数，再在 `tests/test_<模块>.py` 里用 `backend` / `call` 夹具断言发出的请求。

```bash
uv venv && uv pip install -e ".[dev]"
.venv/bin/python -m pytest
.venv/bin/python scripts/gen_tools_doc.py > TOOLS.md   # 增删工具后
```

约束测试（`tests/test_conventions.py`）检查：每个声明的接口都存在于后端 `routes.golden`；读写标签与注解一致；
只读工具引用的接口必须是 GET 或权限点为 `*:read` 的 POST 查询；中文说明；参数 snake_case；只读模式与检索模式。
对真实后端的端到端测法见 [docs/mcp.md](../docs/mcp.md#测法)。
