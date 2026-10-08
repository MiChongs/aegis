# OAuth2 / OIDC 授权服务器（Ory Hydra）

让别的网站、应用、命令行工具与电视盒子「使用 Aegis 账号登录」，按标准 OAuth 2.0 与 OpenID Connect 接入，
不需要接入方认识 Aegis 自己的接口。

与 `oauth-providers`（应用去接 QQ、微信、GitHub 等第三方登录）方向相反：那边 Aegis 是客户端，这里 Aegis 是身份提供方。

## 分工

| 谁 | 做什么 |
|---|---|
| Ory Hydra（v26） | 协议本身：授权端点、令牌端点、JWKS、发现文档、userinfo、令牌签发 / 刷新 / 吊销、设备码、RP 发起的登出 |
| Aegis | Hydra 的 login / consent / logout provider：登录页（复用 App 网关的全部登录关卡）、两步验证、授权同意页、claims、令牌钩子 |
| 控制台 | 按应用管理客户端；在用户详情里查看与撤销该用户授权过的客户端 |

两边只经由 Hydra 的 Admin API 与令牌钩子对话，Hydra 不读 Aegis 的库，Aegis 也不读 Hydra 的库。

```
接入方 ──/oauth2/auth──▶ Hydra ──302──▶ Aegis /oauth2/login      认人（密码 + 两步验证 + 验证码）
                                 ◀─accept─┘
                         Hydra ──302──▶ Aegis /oauth2/consent    问同意、组 claims
                                 ◀─accept─┘
接入方 ◀──code── Hydra
接入方 ──/oauth2/token──▶ Hydra ──POST──▶ Aegis /api/oauth2/hooks/token   账号与应用此刻还能用吗
```

## 多租户：客户端属于应用

每个 Hydra 客户端都绑定一个 Aegis 应用：`owner = aegis-app:<应用 ID>`，`metadata.aegisAppId = <应用 ID>`。
登录页据此只在**该应用的用户库**里认人；两处不一致的客户端按未绑定处理、无法登录。客户端一律经控制台创建，
不开放动态注册。

**主体（`sub`）是用户 ID**，与 Aegis 其余接口里的 `userId` 相同。用户只属于一个应用，所以不同应用的主体不会撞。
客户端可以把主体类型设为 `pairwise`，此时每个客户端看到的 `sub` 互不相同，无法跨客户端关联同一个人。

Hydra 的登录态按浏览器、不按应用：浏览器记住的是应用甲的账号，却去登录应用乙的客户端时，Aegis 会带上
`prompt=login` 让授权请求重新开始，用户登录应用乙的账号后由 Hydra 替换掉这个浏览器上的旧会话。
客户端要求静默授权（`prompt=none`）时返回 `login_required`。

## 端点

签发方（issuer）是 `HYDRA_PUBLIC_URL`，接入方用发现文档即可自动配置：

| 用途 | 地址 |
|---|---|
| 发现文档 | `{issuer}/.well-known/openid-configuration` |
| 授权 | `{issuer}/oauth2/auth` |
| 令牌 | `{issuer}/oauth2/token` |
| 用户信息 | `{issuer}/userinfo` |
| 公钥 | `{issuer}/.well-known/jwks.json` |
| 吊销 | `{issuer}/oauth2/revoke` |
| 登出 | `{issuer}/oauth2/sessions/logout` |
| 设备码 | `{issuer}/oauth2/device/auth` |

Aegis 一侧（`API_BASE_URL` 下）：

| 地址 | 说明 |
|---|---|
| `GET/POST /oauth2/login`、`POST /oauth2/login/mfa` | 登录与两步验证 |
| `GET/POST /oauth2/consent` | 授权同意 |
| `GET/POST /oauth2/logout`、`GET /oauth2/logged-out` | 登出确认与落地页 |
| `GET/POST /oauth2/device`、`GET /oauth2/device/done` | 设备码输入与完成页 |
| `GET /oauth2/error` | Hydra 无法把错误交回客户端时的落地页 |
| `POST /api/oauth2/hooks/token` | 令牌钩子，只给 Hydra 调，凭 `HYDRA_TOKEN_HOOK_SECRET` 认证 |
| `/api/admin/apps/:appkey/oauth2/*` | 控制台：概览、客户端增删改、重新生成密钥 |
| `/api/admin/apps/:appkey/users/:userId/oauth2-grants` | 控制台：用户的授权记录与撤销 |
| `/api/v1/apps/:appkey/me/oauth2/grants` | 网关：用户自己查看与取消授权（SDK：`oauth2Grants()` / `revokeOAuth2Grant()`） |

## 权限与 claims

| 权限 | 同意页上 | 写进 ID Token / userinfo |
|---|---|---|
| `openid` | 必选 | `sub`、`appid`、`app_key` |
| `offline_access` | 必选（请求了才出现） | 签发刷新令牌 |
| `profile` | 可取消 | `name`（昵称，没有时为账号）、`nickname`、`preferred_username`（账号）、`picture`、`updated_at` |
| `email` | 可取消 | `email` |
| `phone` | 可取消 | `phone_number` |
| 其他 | 可取消，原样展示名字 | 无；用于接入方自己的资源服务器 |

不下发 `email_verified` / `phone_number_verified`：管理员导入、后台改写的联系方式没有经过验证码，Aegis 无从担保。

访问令牌默认不透明，资源服务器经 Hydra 的内省接口判定，内省结果的 `ext.appid` 是应用 ID。
个别客户端需要 JWT 访问令牌时在控制台把「访问令牌格式」设为 `jwt`。

两步验证登录的 ID Token 带 `acr = urn:aegis:acr:mfa` 与 `amr = ["pwd","otp"]`（恢复码为 `kba`）。

## 状态关卡

Hydra 记住登录、记住同意、刷新令牌时都不会再来问密码，这几条路径上的判定由 Aegis 补齐：

| 时机 | 判定 |
|---|---|
| 登录页提交 | 与 App 网关密码登录完全相同：防爆破锁定、风控、插件钩子、应用策略（含登录设备检查）、封禁与冻结、密码、两步验证、登录一致性 |
| 记住登录（skip） | 用户属于该应用、账号未封禁冻结、应用开放登录 |
| 同意页 | 同上 |
| 每次签发与刷新令牌（钩子） | 账号未封禁冻结、应用未停用、接口能力未被治理冻结；刷新时按当前资料重算 ID Token |
| 封禁、删除、重置密码、强制下线 | 异步撤销该用户的全部授权与 Hydra 登录态，已签发的令牌立即失效 |

应用的认证方式里没有开放「账号密码」时，网页授权登录不可用。应用要求登录验证码时登录页会出验证码；
页面不跑脚本，点选类与音频类验证码在这里改用图形验证码。

浏览器没有原生设备 ID，登录页以一枚长期 Cookie（`aegis_oauth2_did`）充当，供「登录设备检查」与登录一致性使用。

## 安全

- **CSRF**：表单令牌 = HMAC(主密钥, Cookie 值 | challenge)，Cookie 为 HttpOnly + SameSite=Lax、路径 `/oauth2`。
  令牌绑定 challenge，抓到一张表单也挪不到别的授权请求上。
- **点击劫持**：页面带 `X-Frame-Options: DENY` 与 `frame-ancestors 'none'`。
- **CSP** 收到 `default-src 'none'`（页面无脚本），但**不声明 `form-action`**：表单提交后会经 Hydra 一路 302 到
  接入方的回调地址，浏览器对 `form-action` 的检查覆盖整条重定向链。
- **同源表单与 CORS**：gin-contrib/cors 对 `Origin` 与 `Host` 相同的请求直接放行，因此反向代理必须保留原始
  `Host` 头，否则登录表单会被 CORS 拒成 403。
- **回调地址**：网页用 https；http 只允许回环地址；移动应用用反向域名形式的私有协议（RFC 8252）。不允许 `#` 片段。
- **公开客户端**（SPA、移动、桌面）必须用 PKCE，不能用客户端凭据模式；不能把公开客户端改成机密客户端。
- **重新生成密钥**会让旧密钥立即失效，并吊销该客户端已签发的访问令牌。
- **Hydra 管理端不做任何认证**，compose 里只绑回环地址；生产环境不得暴露到公网。

## 部署

```bash
# .env
OAUTH2_SERVER_ENABLED=true
API_BASE_URL=https://api.example.com        # 浏览器访问 Aegis 的地址（登录页在这里）
HYDRA_PUBLIC_URL=https://auth.example.com   # 签发方，浏览器访问 Hydra 公开端的地址
# HYDRA_SYSTEM_SECRET / HYDRA_PAIRWISE_SALT / HYDRA_TOKEN_HOOK_SECRET 由一键脚本生成

./deploy/docker/quickstart.sh               # 检测到开关后自动加 --profile oauth2
```

`--profile oauth2` 启动三个服务：`hydra-db-init`（在同一 Postgres 实例里建独立的 `hydra` 库，幂等）、
`hydra-migrate`（一次性迁移）、`hydra`（公开端 4444，管理端 4445 只绑回环）。
Hydra 的固定配置在 `deploy/docker/hydra/hydra.yml`，地址与密钥由 compose 以环境变量注入。

宿主机 `go run` 而 Hydra 在容器里时：

```bash
HYDRA_ADMIN_URL=http://127.0.0.1:4445
HYDRA_TOKEN_HOOK_URL=http://host.docker.internal:8088/api/oauth2/hooks/token
HYDRA_DEV_FLAG=--dev    # 只在本机 http 调试时
```

`HYDRA_SYSTEM_SECRET` 丢失或更换，Hydra 库里加密的数据全部作废（所有令牌与授权记录失效）。

令牌寿命（`hydra.yml`）：访问令牌 1 小时、ID Token 1 小时、刷新令牌 30 天（每次刷新轮换）、授权码 10 分钟、
设备用户码 10 分钟。登录记住时长与同意记住时长由 Aegis 决定（`OAUTH2_LOGIN_REMEMBER_FOR`、`OAUTH2_CONSENT_REMEMBER_FOR`）。

## 接入示例

授权码 + PKCE（公开客户端）：

```
GET {issuer}/oauth2/auth?client_id=…&response_type=code&scope=openid%20offline_access%20profile
    &redirect_uri=https%3A%2F%2Fapp.example.com%2Fcallback&state=…&nonce=…
    &code_challenge=…&code_challenge_method=S256

POST {issuer}/oauth2/token
  grant_type=authorization_code&code=…&redirect_uri=…&client_id=…&code_verifier=…
```

设备码（电视、命令行）：

```
POST {issuer}/oauth2/device/auth   client_id=…&scope=openid profile
  → device_code、user_code（区分大小写，用户输入时可带空格与连字符）、verification_uri
用户在浏览器打开 verification_uri 输入 user_code → 登录 → 同意
POST {issuer}/oauth2/token  grant_type=urn:ietf:params:oauth:grant-type:device_code&device_code=…
```

资源服务器校验访问令牌：`POST http://hydra:4445/admin/oauth2/introspect`（内网），`active=true` 且
`ext.appid` 是自己的应用。

## 测试

`internal/transport/http/oauth2_integration_test.go` 用真实 Postgres + Redis + Hydra 进程，以带 Cookie 的客户端
扮演浏览器，覆盖授权码、错误密码、伪造 CSRF、部分授权、刷新与 claims 重算、记住登录与同意、跨应用切换、
封禁后刷新被拒、两步验证、设备码、授权记录隔离与撤销、密钥轮换、RP 发起的登出、钩子认证：

```bash
AEGIS_TEST_PG_DSN=postgres://postgres:aegis@127.0.0.1:25432/postgres?sslmode=disable \
AEGIS_TEST_REDIS_ADDR=127.0.0.1:26379 \
AEGIS_TEST_HYDRA_BIN=/path/to/hydra \
go test ./internal/transport/http -run TestOAuth2ServerEndToEnd
```

Hydra 用 GitHub Releases 里的 `hydra_<版本>-linux_sqlite_64bit.tar.gz`，测试里以 `DSN=memory` 启动，不需要它自己的库。
测试库不带 pgvector / PostGIS，相关迁移跳过。
