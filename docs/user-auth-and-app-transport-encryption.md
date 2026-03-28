# Aegis 用户侧认证接口与应用加密传输接入文档

## 1. 适用范围

本文档基于当前代码实现整理，覆盖两部分内容：

1. 用户侧认证与账户安全相关接口
2. 应用加密传输机制的实际接入方法

文档以当前服务端真实行为为准，不按管理端字段名做理想化推断。

## 2. 基础约定

### 2.1 基础地址

- 本地示例：`http://127.0.0.1:8080`
- 生产环境请替换为实际网关地址

### 2.2 通用响应包格式

成功响应与失败响应统一使用如下结构：

```json
{
  "code": 200,
  "message": "登录成功",
  "data": {},
  "requestId": "01H..."
}
```

字段说明：

- `code`: 业务码，成功通常为 `200`
- `message`: 业务消息
- `data`: 业务数据，失败时通常不存在
- `requestId`: 请求链路 ID，便于排查

### 2.3 用户认证头

需要用户登录态的接口使用：

```http
Authorization: Bearer <accessToken>
```

### 2.4 公共错误语义

认证链路常见错误码：

- `40000`: 请求参数错误
- `40001`: 不支持的 OAuth2 提供商
- `40002`: OAuth2 状态无效或已过期
- `40004`: `providerUserId` 不能为空
- `40006`: 新密码不能与当前密码相同
- `40010`: OAuth2 提供商未完成配置
- `40013`: OAuth2 回调参数不完整
- `40024`: 当前应用要求提供设备标识
- `40025`: 当前应用要求提供注册 IP
- `40100`: 未认证
- `40101`: 账号或密码错误
- `40102`: Token 无效
- `40103`: 会话用户不存在
- `40104`: Token 已失效
- `40105`: 会话不存在或已过期
- `40106`: 当前密码错误
- `40301`: 用户账户已被禁用
- `40302`: 用户账户暂时被冻结
- `40398`: 被风控拦截
- `40901`: 账号已存在
- `40902`: IP 已注册过账号
- `50321`: 认证或安全模块暂不可用

## 3. 登录结果模型

注册、登录、OAuth 登录、Passkey 登录、二次认证完成后，统一返回 `LoginResult`。

```json
{
  "accessToken": "<jwt>",
  "refreshToken": "<refresh-jwt>",
  "expiresAt": "2026-04-27T10:00:00Z",
  "refreshExpiresAt": "2026-05-27T10:00:00Z",
  "tokenType": "Bearer",
  "userId": 123,
  "account": "demo",
  "provider": "password"
}
```

如果命中了二次认证，不会立即发放 `accessToken`，而是返回：

```json
{
  "userId": 123,
  "account": "demo",
  "provider": "password",
  "requiresSecondFactor": true,
  "authenticationState": "second_factor_required",
  "challenge": {
    "challengeId": "9f7c...",
    "state": "pending",
    "methods": ["totp", "recovery_code"],
    "expiresAt": "2026-03-28T12:00:00Z"
  }
}
```

## 4. 用户侧认证接口

### 4.1 密码注册

- 方法：`POST`
- 路径：`/api/auth/register/password`
- 鉴权：无需登录

请求体：

```json
{
  "appid": 10000,
  "account": "demo",
  "password": "P@ssw0rd!",
  "nickname": "演示用户",
  "markcode": "device-001"
}
```

字段说明：

- `appid`: 应用 ID，必填
- `account`: 账号，必填，服务端会做归一化
- `password`: 密码，必填，受密码策略约束
- `nickname`: 昵称，可选
- `markcode`: 设备标识，可选；当应用要求登录设备标识时必须传

成功响应：

- 直接注册成功并登录：返回 `LoginResult`
- 若开启用户二次认证：返回待二次认证的 `LoginResult`

实现说明：

- 注册会走密码策略校验
- 注册会走风控评估
- 应用若开启 `registerCheckIp`，同一 IP 已注册过任意账号时会拒绝
- 当前实现已经对同一 `appid + account` 的并发注册做了 `singleflight` 合并，重复并发请求只会实际执行一次注册逻辑

### 4.2 密码登录

- 方法：`POST`
- 路径：`/api/auth/login/password`
- 鉴权：无需登录

请求体：

```json
{
  "appid": 10000,
  "account": "demo",
  "password": "P@ssw0rd!",
  "markcode": "device-001"
}
```

成功响应：

- 正常登录：返回 `LoginResult`
- 命中 2FA：返回待二次认证的 `LoginResult`

实现说明：

- 登录前会检查应用是否允许登录
- 当应用开启 `loginCheckDevice` 时，`markcode` 必填
- 登录会走风控评估与插件钩子

### 4.3 获取 OAuth2 授权地址

- 方法：`POST`
- 路径：`/api/auth/oauth2/auth-url`
- 鉴权：无需登录

请求体：

```json
{
  "appid": 10000,
  "provider": "github",
  "markcode": "device-001"
}
```

成功响应：

```json
{
  "code": 200,
  "message": "获取授权地址成功",
  "data": {
    "url": "https://github.com/login/oauth/authorize?..."
  }
}
```

当前内置默认提供商：

- `qq`
- `wechat`
- `github`
- `google`
- `microsoft`
- `weibo`

注意：

- 只有在配置了对应 `clientId` 和 `redirectUrl` 后才可用
- 授权状态 `state` 由服务端写入 Redis，默认有效期 5 分钟

### 4.4 OAuth2 回调登录

- 方法：`GET`
- 路径：`/api/auth/oauth2/callback`
- 鉴权：无需登录

查询参数：

- `provider`: OAuth 提供商
- `code`: 授权码
- `state`: 服务端下发的 OAuth 状态

成功响应：

- 正常登录：返回 `LoginResult`
- 命中 2FA：返回待二次认证的 `LoginResult`

### 4.5 移动端 OAuth 直登

- 方法：`POST`
- 路径：`/api/auth/oauth2/mobile-login`
- 鉴权：无需登录

请求体：

```json
{
  "appid": 10000,
  "provider": "wechat",
  "providerUserId": "openid_xxx",
  "unionId": "union_xxx",
  "nickname": "张三",
  "avatar": "https://...",
  "email": "demo@example.com",
  "accessToken": "provider_access_token",
  "refreshToken": "provider_refresh_token",
  "markcode": "device-001",
  "rawProfile": {
    "gender": 1
  }
}
```

说明：

- 适合移动端已经自行完成三方授权的场景
- `providerUserId` 必填
- 服务端会基于 `(appid, provider, providerUserId)` 查找或创建本地账号

### 4.6 二次认证校验

- 方法：`POST`
- 路径：`/api/auth/2fa/verify`
- 鉴权：无需登录

请求体：

```json
{
  "challengeId": "9f7c...",
  "code": "123456",
  "recoveryCode": ""
}
```

说明：

- `code` 与 `recoveryCode` 二选一
- 校验通过后签发正式登录会话

### 4.7 Passkey 登录参数获取

- 方法：`POST`
- 路径：`/api/auth/passkey/options`
- 别名：`/api/auth/passkey/auth-options`
- 鉴权：无需登录

请求体：

```json
{
  "appid": 10000,
  "markcode": "device-001"
}
```

成功响应：

```json
{
  "code": 200,
  "message": "获取成功",
  "data": {
    "session": {
      "challengeId": "8cc2...",
      "appid": 10000,
      "expiresAt": "2026-03-28T12:00:00Z"
    },
    "options": {}
  }
}
```

### 4.8 Passkey 登录校验

- 方法：`POST`
- 路径：`/api/auth/passkey/verify`
- 别名：`/api/auth/passkey/login`
- 鉴权：无需登录

请求体：

```json
{
  "appid": 10000,
  "challengeId": "8cc2...",
  "credential": {
    "id": "...",
    "rawId": "...",
    "response": {},
    "type": "public-key"
  },
  "markcode": "device-001"
}
```

说明：

- `credential` 与 `payload` 任传其一即可
- 服务端也支持从原始 JSON 根节点自动提取 `credential`、`payload`、`attestation`、`assertion`

### 4.9 刷新令牌

- 方法：`POST`
- 路径：`/api/auth/refresh`
- 鉴权：无需登录，但必须提供 refresh token

请求体：

```json
{
  "refreshToken": "<refresh-token>",
  "markcode": "device-001"
}
```

兼容字段：

- `refreshToken`: 推荐字段
- `token`: 兼容旧客户端字段，语义已切换为 refresh token

也可通过请求头传 refresh token：

```http
Authorization: Bearer <refresh-token>
```

实现说明：

- 当前实现为独立 refresh token 模式，刷新成功会同时返回新的 access token 和新的 refresh token
- refresh token 为单次轮换模式，旧 refresh token 一旦成功使用，会被标记为已轮换
- refresh token 默认绑定原设备；请求体未传 `markcode` 时，服务端会沿用 refresh 会话中的 `deviceId`
- 若旧 refresh token 被重复使用，或设备绑定校验失败，服务端会吊销整条 refresh family，并清理该 family 关联的 access 会话

### 4.10 登出

- 方法：`POST`
- 路径：`/api/auth/logout`
- 鉴权：需要登录

请求头：

```http
Authorization: Bearer <accessToken>
```

成功响应：

```json
{
  "code": 200,
  "message": "退出成功",
  "data": null
}
```

### 4.11 校验当前密码

- 方法：`POST`
- 路径：`/api/auth/password/verify`
- 鉴权：需要登录

请求体：

```json
{
  "password": "old-password"
}
```

成功响应：

```json
{
  "code": 200,
  "message": "验证成功",
  "data": {
    "valid": true
  }
}
```

### 4.12 修改密码

- 方法：`POST`
- 路径：`/api/auth/password/change`
- 鉴权：需要登录

请求体：

```json
{
  "currentPassword": "old-password",
  "newPassword": "new-password"
}
```

实现说明：

- 会重新走密码策略校验
- 若用户已有密码，必须验证 `currentPassword`
- 修改完成后会记录密码修改时间和密码强度分数

## 5. 用户账户安全接口

这部分接口不属于 `/api/auth`，但属于用户认证完成后的安全能力，前端通常需要一并接入。

### 5.1 获取安全概览

- 方法：`GET`
- 路径：`/api/user/security`
- 鉴权：需要登录

返回字段包括：

- `hasPassword`
- `twoFactorEnabled`
- `twoFactorMethod`
- `passkeyEnabled`
- `passwordStrengthScore`
- `passwordChangeRequired`
- `passwordChangedAt`
- `passwordExpiresAt`
- `oauth2Bindings`
- `oauth2Providers`
- `twoFactor`
- `recoveryCodes`
- `passkeys`
- `modules`

### 5.2 发起 TOTP 绑定

- 方法：`POST`
- 路径：`/api/user/two-factor/enroll`
- 鉴权：需要登录

成功响应示例：

```json
{
  "code": 200,
  "message": "获取成功",
  "data": {
    "enrollmentId": "3fd5...",
    "secret": "BASE32SECRET",
    "secretMasked": "BASE****CRET",
    "provisioningUri": "otpauth://totp/...",
    "issuer": "aegis",
    "accountName": "demo",
    "expiresAt": "2026-03-28T12:00:00Z"
  }
}
```

### 5.3 启用 TOTP

- 方法：`POST`
- 路径：`/api/user/two-factor/enable`
- 鉴权：需要登录

请求体：

```json
{
  "enrollmentId": "3fd5...",
  "code": "123456"
}
```

成功后返回：

- `twoFactor`: 最新二次认证状态
- `recoveryCodes`: 初次生成的恢复码

### 5.4 关闭 TOTP

- 方法：`POST`
- 路径：`/api/user/two-factor/disable`
- 鉴权：需要登录

请求体：

```json
{
  "code": "123456",
  "recoveryCode": ""
}
```

说明：

- `code` 与 `recoveryCode` 二选一

### 5.5 获取恢复码摘要

- 方法：`GET`
- 路径：`/api/user/two-factor/recovery-codes`
- 鉴权：需要登录

### 5.6 生成恢复码

- 方法：`POST`
- 路径：`/api/user/two-factor/recovery-codes`
- 鉴权：需要登录

请求体：

```json
{
  "code": "123456",
  "recoveryCode": ""
}
```

### 5.7 重置恢复码

- 方法：`POST`
- 路径：`/api/user/two-factor/recovery-codes/regenerate`
- 鉴权：需要登录

请求体与 5.6 相同。

### 5.8 获取 Passkey 列表

- 方法：`GET`
- 路径：`/api/user/passkey`
- 鉴权：需要登录

### 5.9 获取 Passkey 注册参数

- 方法：`POST`
- 路径：`/api/user/passkey/register/options`
- 鉴权：需要登录

成功响应：

```json
{
  "code": 200,
  "message": "获取成功",
  "data": {
    "session": {
      "challengeId": "f91a...",
      "appid": 10000,
      "userId": 123,
      "expiresAt": "2026-03-28T12:00:00Z"
    },
    "options": {}
  }
}
```

### 5.10 完成 Passkey 注册

- 方法：`POST`
- 路径：`/api/user/passkey/register`
- 鉴权：需要登录

请求体：

```json
{
  "challengeId": "f91a...",
  "credentialName": "MacBook Touch ID",
  "credential": {
    "id": "...",
    "rawId": "...",
    "response": {},
    "type": "public-key"
  }
}
```

### 5.11 删除 Passkey

- 方法：`DELETE`
- 路径：`/api/user/passkey/:credentialId`
- 鉴权：需要登录

### 5.12 当前会话列表

- 方法：`GET`
- 路径：`/api/user/sessions`
- 鉴权：需要登录

返回 `SessionListResult`：

```json
{
  "items": [
    {
      "tokenHash": "sha256:...",
      "current": true,
      "account": "demo",
      "provider": "password",
      "deviceId": "device-001",
      "ip": "127.0.0.1",
      "userAgent": "Mozilla/5.0",
      "issuedAt": "2026-03-28T10:00:00Z",
      "expiresAt": "2026-04-27T10:00:00Z"
    }
  ],
  "total": 1
}
```

### 5.13 踢出单个会话

- 方法：`DELETE`
- 路径：`/api/user/sessions/:tokenHash`
- 鉴权：需要登录

### 5.14 踢出全部会话

- 方法：`POST`
- 路径：`/api/user/sessions/revoke-all`
- 鉴权：需要登录

请求体：

```json
{
  "includeCurrent": false
}
```

### 5.15 更新个人资料

- 方法：`PUT`
- 路径：`/api/user/profile`
- 鉴权：需要登录

请求体：

```json
{
  "nickname": "新昵称",
  "avatar": "storage://avatars/u-123.png",
  "email": "new@example.com",
  "phone": "13800138000",
  "birthday": "2000-01-15",
  "bio": "hello",
  "contacts": [
    {
      "platform": "wechat",
      "value": "demo-wechat"
    }
  ]
}
```

返回体：

```json
{
  "profile": {
    "userId": 123,
    "nickname": "新昵称",
    "avatar": "https://cdn.example.com/avatars/u-123.png",
    "email": "old@example.com",
    "phone": "13900001111"
  },
  "pendingChanges": [
    {
      "field": "email",
      "value": "new@example.com",
      "maskedValue": "n***w@example.com",
      "purpose": "profile_email_change",
      "expiresAt": "2026-03-28T12:15:00Z",
      "requestedAt": "2026-03-28T12:00:00Z"
    }
  ]
}
```

实现说明：

- 普通字段会立即生效：`nickname`、`avatar`、`birthday`、`bio`、`contacts`
- 敏感字段不会直接生效：`email`、`phone`
- 敏感字段会先进入待确认状态，返回在 `pendingChanges` 中
- 同一 `appid` 下，邮箱和手机号会做占用校验，已被其他账号使用时会拒绝

### 5.16 确认敏感资料变更

- 方法：`POST`
- 路径：`/api/user/profile/changes/confirm`
- 鉴权：需要登录

请求体：

```json
{
  "field": "email",
  "code": "123456"
}
```

字段说明：

- `field`: 当前支持 `email`、`phone`
- `code`: 发往新邮箱或新手机号的验证码

实现说明：

- `email` 会校验邮件验证码，验证码用途为 `profile_email_change`
- `phone` 会校验短信验证码，验证码用途为 `profile_phone_change`
- 校验通过后，资料才会真正落库并清除 pending 状态

### 5.17 用户签到幂等

- 方法：`POST`
- 路径：`/api/user/signin`
- 鉴权：需要登录

实现说明：

- 服务端除了数据库唯一约束和进程内 `singleflight` 外，还增加了 Redis 分布式幂等锁
- 弱网重复点击、连点器、多实例同时到达时，只有一个请求会实际执行签到写入
- 其他并发请求会等待首个请求结果，成功时直接复用已签到结果；长时间未完成时返回处理中冲突错误

## 6. JWT 与会话行为

### 6.1 Token 类型

当前用户侧签发两类令牌：

- `access token`: 用于访问业务接口，类型为 `Bearer`
- `refresh token`: 用于刷新访问令牌，不用于访问业务接口

### 6.2 默认有效期

如果未通过环境变量覆盖：

- `JWT_TTL`: 默认 `30d`
- `JWT_REFRESH_TTL`: 默认 `7d`

### 6.3 服务端会话校验

除了 JWT 签名外，服务端还会校验 Redis 中的 access/refresh 会话与黑名单状态，因此：

- JWT 未过期但 Redis 会话不存在，仍会判定为失效
- access token 登出后会进入黑名单
- refresh token 刷新后，旧 refresh token 会被标记为已轮换，不能再次使用
- 一旦检测到 refresh token 重放，服务端会吊销整个 refresh family

## 7. 应用加密传输

## 7.1 生效范围

加密中间件全局注册，实际会对以下用户侧路径生效：

- `/api/auth`
- `/api/user`
- `/api/user-settings`
- `/api/points`
- `/api/notifications`
- `/api/email`
- `/api/pay`
- `/api/storage`
- `/api/app/public`

默认不生效的路径包括：

- `/healthz`
- `/readyz`
- `/api/admin`
- `/api/public/pay`
- `/api/storage/proxy/*`

## 7.2 服务端启用条件

对某个 `appid`，仅当应用配置中的传输加密策略满足以下条件时才会强制启用：

- `transportEncryption.enabled = true`
- 且存在可用 `secret`

`secret` 的来源优先级：

1. `settings.transportEncryption.secret`
2. `settings.transportEncryption.key`
3. `settings.transportEncryption.passphrase`
4. 若都为空，则回退为 `app.appKey`

## 7.3 实际强制行为

一旦命中启用策略：

- 未携带加密头的请求会被直接拒绝
- 返回 HTTP `400`
- 业务码为 `40061`
- 错误消息为：`当前应用已开启加密通信`

## 7.4 请求头协议

客户端应至少发送以下头：

```http
X-Aegis-Encrypted: 1
X-Aegis-Appid: 10000
X-Aegis-Nonce: <base64url nonce>
X-Aegis-Algorithm: XChaCha20Poly1305
```

对于有请求体的加密请求，额外建议发送：

```http
X-Aegis-Plain-Content-Type: application/json
Content-Type: application/octet-stream
```

头字段说明：

- `X-Aegis-Encrypted`: 是否启用本次加密请求，接受 `1/true/yes/on`
- `X-Aegis-Appid`: 应用 ID。对登录、注册、公开接口的加密请求，建议始终传此头
- `X-Aegis-Nonce`: 本次请求随机 nonce，Base64URL 编码，长度由算法决定
- `X-Aegis-Algorithm`: 加密算法名
- `X-Aegis-Plain-Content-Type`: 原始明文内容类型。服务端解密后会恢复成该类型

## 7.5 AppID 识别逻辑

中间件会按以下顺序解析 `appid`：

1. `X-Aegis-Appid`
2. `Authorization` 中 JWT 的 `appid`
3. 明文查询参数 `appid`
4. 明文请求体中的 `appid`

因此对以下场景，`X-Aegis-Appid` 基本上是必填：

- `/api/auth/login/password`
- `/api/auth/register/password`
- `/api/auth/passkey/options`
- `/api/app/public`
- 任意 GET 加密查询接口

原因是这些请求在中间件解密前，通常无法从 JWT 中反推出 `appid`。

## 7.6 当前可用算法

当前请求链路已经支持以下 6 种算法：

- `XChaCha20Poly1305`
- `AES-256-GCM`
- `hybrid-rsa-xchacha20`
- `hybrid-rsa-aes256gcm`
- `hybrid-ecdh-xchacha20`
- `hybrid-ecdh-aes256gcm`

说明：

- 非 hybrid 算法使用应用共享密钥 `secret` 派生对称密钥
- hybrid-RSA 算法要求客户端生成随机会话密钥，并通过 `X-Aegis-Key` 传输经服务端 RSA 公钥加密后的密钥
- hybrid-ECDH 算法要求客户端生成临时 ECDH 密钥对，并通过 `X-Aegis-Key` 传输客户端临时公钥，服务端使用应用 ECDH 私钥与之协商会话密钥
- hybrid 算法在请求和响应两个方向都复用同一个会话密钥

## 7.7 密钥派生

服务端使用如下逻辑派生对称密钥：

1. 先解析 `secret`
2. `secret` 会按以下顺序尝试解码：
   - Base64URL
   - Base64 标准编码
   - Hex
   - 都失败则按普通字符串字节处理
3. 计算：

```text
key = SHA256(appid + ":" + hex(secretMaterial))
```

代码等价表达：

```go
sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%x", appID, material)))
derivedKey := sum[:]
```

## 7.8 AAD 绑定规则

服务端会把 `appid`、HTTP 方法、路径、作用域拼成 AAD。

请求查询字符串：

```text
appid=<appid>|method=<METHOD>|path=<PATH>|scope=request-query
```

请求体：

```text
appid=<appid>|method=<METHOD>|path=<PATH>|scope=request-body
```

响应体：

```text
appid=<appid>|method=<METHOD>|path=<PATH>|scope=response-body
```

要求：

- `method` 必须与真实请求方法一致
- `path` 必须是服务端看到的 URL Path，例如 `/api/auth/login/password`
- AAD 不一致会导致解密失败

## 7.9 GET 请求加密规则

对 `GET`、`DELETE`、`HEAD` 请求，服务端从查询参数 `_payload` 中读取密文。

明文查询串示例：

```text
appid=10000&page=1&limit=20
```

加密后请求示例：

```http
GET /api/user/banner?_payload=<base64url ciphertext> HTTP/1.1
X-Aegis-Encrypted: 1
X-Aegis-Appid: 10000
X-Aegis-Nonce: <base64url nonce>
X-Aegis-Algorithm: XChaCha20Poly1305
```

服务端会在解密成功后把 `req.URL.RawQuery` 还原成真实查询串。

## 7.10 有请求体接口的加密规则

对 `POST`、`PUT`、`PATCH` 等请求：

- HTTP Body 直接放密文字节流
- 服务端按 `request-body` AAD 解密
- 若有 `X-Aegis-Plain-Content-Type`，会恢复为原始 `Content-Type`

明文 JSON：

```json
{
  "appid": 10000,
  "account": "demo",
  "password": "P@ssw0rd!"
}
```

请求示例：

```http
POST /api/auth/login/password HTTP/1.1
Content-Type: application/octet-stream
X-Aegis-Encrypted: 1
X-Aegis-Appid: 10000
X-Aegis-Nonce: <base64url nonce>
X-Aegis-Algorithm: XChaCha20Poly1305
X-Aegis-Plain-Content-Type: application/json

<raw ciphertext bytes>
```

## 7.10.1 `X-Aegis-Key` 规则

仅当 `X-Aegis-Algorithm` 为 hybrid 算法时需要传 `X-Aegis-Key`。

### hybrid-RSA

当算法为：

- `hybrid-rsa-xchacha20`
- `hybrid-rsa-aes256gcm`

`X-Aegis-Key` 的值应为：

- 客户端随机生成的会话密钥
- 使用服务端 `rsaPublicKey` 通过 `RSA-OAEP-SHA256` 加密
- 再做 Base64URL 编码

服务端收到后会：

1. 用 `rsaPrivateKey` 解密会话密钥
2. 把该会话密钥作为本次请求和响应的对称密钥
3. 再按 hybrid 算法中约定的对称算法执行正文加解密

### hybrid-ECDH

当算法为：

- `hybrid-ecdh-xchacha20`
- `hybrid-ecdh-aes256gcm`

`X-Aegis-Key` 的值应为：

- 客户端临时 ECDH 公钥字节
- 推荐直接传 `P-256` 公钥原始字节的 Base64URL 编码

服务端收到后会：

1. 用应用配置中的 `ecdhPrivateKey` 与客户端临时公钥协商共享密钥
2. 对共享密钥做 `SHA256`
3. 结果作为本次请求和响应的对称密钥

## 7.11 响应加密规则

当应用配置 `responseEncryption = true` 且请求不是 `HEAD` 时，服务端会加密响应体。

响应头示例：

```http
Content-Type: application/octet-stream
X-Aegis-Encrypted: 1
X-Aegis-Algorithm: XChaCha20Poly1305
X-Aegis-Nonce: <base64url nonce>
X-Aegis-Plain-Content-Type: application/json; charset=utf-8
Cache-Control: no-store
```

客户端拿到响应后，应按以下规则解密：

- 对称模式：使用同一个共享密钥派生结果
- hybrid 模式：使用与请求阶段相同的会话密钥
- 相同 `appid`
- 相同 `method`
- 相同 `path`
- `scope=response-body`

## 7.12 Node.js 接入示例

下面示例演示共享密钥对称模式。

```js
import crypto from "node:crypto";
import { randomBytes } from "node:crypto";
import sodium from "libsodium-wrappers";

function decodeSecret(secret) {
  const candidates = [
    { enc: "base64url" },
    { enc: "base64" },
    { enc: "hex" }
  ];
  for (const item of candidates) {
    try {
      const buf = Buffer.from(secret, item.enc);
      if (buf.length > 0) return buf;
    } catch {}
  }
  return Buffer.from(secret, "utf8");
}

function deriveKey(appid, secret) {
  const material = decodeSecret(secret);
  const hex = material.toString("hex");
  return crypto.createHash("sha256").update(`${appid}:${hex}`).digest();
}

function aad(appid, method, path, scope) {
  return Buffer.from(`appid=${appid}|method=${method.toUpperCase()}|path=${path}|scope=${scope}`, "utf8");
}

async function encryptJsonBody(appid, secret, method, path, payload) {
  await sodium.ready;
  const key = deriveKey(appid, secret);
  const nonce = randomBytes(sodium.crypto_aead_xchacha20poly1305_ietf_NPUBBYTES);
  const plaintext = Buffer.from(JSON.stringify(payload));
  const cipher = sodium.crypto_aead_xchacha20poly1305_ietf_encrypt(
    plaintext,
    aad(appid, method, path, "request-body"),
    null,
    nonce,
    key
  );
  return {
    body: Buffer.from(cipher),
    headers: {
      "Content-Type": "application/octet-stream",
      "X-Aegis-Encrypted": "1",
      "X-Aegis-Appid": String(appid),
      "X-Aegis-Nonce": Buffer.from(nonce).toString("base64url"),
      "X-Aegis-Algorithm": "XChaCha20Poly1305",
      "X-Aegis-Plain-Content-Type": "application/json"
    }
  };
}
```

## 7.13 Go 接入示例

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/secure-io/sio-go"
)

func deriveKey(appID int64, secret string) []byte {
	material, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(material) == 0 {
		material = []byte(secret)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%x", appID, material)))
	return sum[:]
}

func aad(appID int64, method, path, scope string) []byte {
	return []byte(fmt.Sprintf("appid=%d|method=%s|path=%s|scope=%s", appID, method, path, scope))
}

func encryptBody(appID int64, secret, method, path string, payload any, nonce []byte) ([]byte, error) {
	stream, err := sio.XChaCha20Poly1305.Stream(deriveKey(appID, secret))
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	writer := stream.EncryptWriter(&buf, nonce, aad(appID, method, path, "request-body"))
	if _, err := writer.Write(plain); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func main() {
	appID := int64(10000)
	secret := "transport-secret"
	path := "/api/auth/login/password"
	nonce, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718")

	body, err := encryptBody(appID, secret, http.MethodPost, path, map[string]any{
		"appid":    appID,
		"account":  "demo",
		"password": "P@ssw0rd!",
	}, nonce)
	if err != nil {
		panic(err)
	}

	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Aegis-Encrypted", "1")
	req.Header.Set("X-Aegis-Appid", "10000")
	req.Header.Set("X-Aegis-Algorithm", "XChaCha20Poly1305")
	req.Header.Set("X-Aegis-Plain-Content-Type", "application/json")
	req.Header.Set("X-Aegis-Nonce", base64.RawURLEncoding.EncodeToString(nonce))

	_ = req
}
```

## 7.13.1 Node.js hybrid-RSA 示例

```js
import crypto from "node:crypto";
import { publicEncrypt, randomBytes } from "node:crypto";
import sodium from "libsodium-wrappers";

function aad(appid, method, path, scope) {
  return Buffer.from(`appid=${appid}|method=${method.toUpperCase()}|path=${path}|scope=${scope}`, "utf8");
}

async function encryptHybridRSA(appid, method, path, payload, rsaPublicKeyPem) {
  await sodium.ready;
  const sessionKey = randomBytes(32);
  const nonce = randomBytes(sodium.crypto_aead_xchacha20poly1305_ietf_NPUBBYTES);
  const plaintext = Buffer.from(JSON.stringify(payload));
  const ciphertext = sodium.crypto_aead_xchacha20poly1305_ietf_encrypt(
    plaintext,
    aad(appid, method, path, "request-body"),
    null,
    nonce,
    sessionKey
  );

  const encryptedKey = publicEncrypt(
    {
      key: rsaPublicKeyPem,
      oaepHash: "sha256",
      padding: crypto.constants.RSA_PKCS1_OAEP_PADDING
    },
    sessionKey
  );

  return {
    body: Buffer.from(ciphertext),
    sessionKey,
    headers: {
      "Content-Type": "application/octet-stream",
      "X-Aegis-Encrypted": "1",
      "X-Aegis-Appid": String(appid),
      "X-Aegis-Nonce": Buffer.from(nonce).toString("base64url"),
      "X-Aegis-Algorithm": "hybrid-rsa-xchacha20",
      "X-Aegis-Key": Buffer.from(encryptedKey).toString("base64url"),
      "X-Aegis-Plain-Content-Type": "application/json"
    }
  };
}
```

## 7.13.2 Go hybrid-ECDH 示例

```go
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/secure-io/sio-go"
)

func parseServerECDHPublic(publicKeyPEM string) (*ecdh.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid public key pem")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecdsaPub := parsed.(interface{ ECDH() (*ecdh.PublicKey, error) })
	return ecdsaPub.ECDH()
}

func main() {
	serverPub, _ := parseServerECDHPublic("SERVER_ECDH_PUBLIC_KEY_PEM")
	clientPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	shared, _ := clientPriv.ECDH(serverPub)
	sessionKey := sha256.Sum256(shared)

	stream, _ := sio.AES_256_GCM.Stream(sessionKey[:])
	nonce := bytes.Repeat([]byte{7}, stream.NonceSize())
	aad := []byte("appid=10000|method=GET|path=/api/user/banner|scope=request-query")

	var body bytes.Buffer
	writer := stream.EncryptWriter(&body, nonce, aad)
	_, _ = writer.Write([]byte("appid=10000&page=1"))
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/user/banner?_payload="+base64.RawURLEncoding.EncodeToString(body.Bytes()), nil)
	req.Header.Set("X-Aegis-Encrypted", "1")
	req.Header.Set("X-Aegis-Appid", "10000")
	req.Header.Set("X-Aegis-Algorithm", "hybrid-ecdh-aes256gcm")
	req.Header.Set("X-Aegis-Key", base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes()))
	req.Header.Set("X-Aegis-Nonce", base64.RawURLEncoding.EncodeToString(nonce))
}
```

## 7.13.3 Android 依赖库

下面给的是能直接跑通本文示例的最小依赖组合。

- 只接 `AES-256-GCM`、`hybrid-rsa-aes256gcm`、`hybrid-ecdh-aes256gcm`：只需要 Android/JCA，`OkHttp` 只是本文发送请求示例使用
- 需要 `XChaCha20Poly1305`、`hybrid-rsa-xchacha20`、`hybrid-ecdh-xchacha20`：增加 `lazysodium-android` 和 `jna`

Gradle 示例：

```kotlin
dependencies {
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.goterl:lazysodium-android:5.2.0@aar")
    implementation("net.java.dev.jna:jna:5.17.0@aar")
}
```

说明：

- `lazysodium-android` 官方 README 当前要求同时引入 `jna`
- `lazysodium-android` 官方 README 当前注明最低 `minSdk` 为 `24`
- `lazysodium-android` 当前 Maven Central 版本是 `5.2.0`
- `jna` 官方当前最新版本是 `5.18.1`，但 `lazysodium-android` README 仍明确示例 `5.17.0@aar`，本文先跟随上游示例，避免 Android 打包兼容性偏差
- 如果你的 App 不需要 `XChaCha20Poly1305`，可以删掉 `lazysodium-android` 与 `jna`
- 如果项目已统一使用其他 `OkHttp` 版本，继续沿用现有版本即可

## 7.13.4 Android Kotlin 共享密钥示例

Android 原生标准库直接接 `AES-256-GCM` 最稳妥。

```kotlin
import android.util.Base64
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

object AegisAndroidCrypto {
    private fun tryDecode(secret: String, flags: Int): ByteArray? {
        return try {
            Base64.decode(secret, flags).takeIf { it.isNotEmpty() }
        } catch (_: IllegalArgumentException) {
            null
        }
    }

    private fun decodeSecret(secret: String): ByteArray {
        return tryDecode(secret, Base64.URL_SAFE or Base64.NO_WRAP) ?:
            tryDecode(secret, Base64.NO_WRAP) ?:
            try {
                hexToBytes(secret).takeIf { it.isNotEmpty() }
            } catch (_: IllegalArgumentException) {
                null
            } ?:
            secret.toByteArray(StandardCharsets.UTF_8)
    }

    private fun hexToBytes(hex: String): ByteArray {
        require(hex.length % 2 == 0) { "invalid hex" }
        return ByteArray(hex.length / 2) { i ->
            hex.substring(i * 2, i * 2 + 2).toInt(16).toByte()
        }
    }

    private fun bytesToHex(bytes: ByteArray): String =
        bytes.joinToString("") { "%02x".format(it) }

    fun deriveKey(appId: Long, secret: String): ByteArray {
        val material = decodeSecret(secret)
        val source = "$appId:${bytesToHex(material)}".toByteArray(StandardCharsets.UTF_8)
        return MessageDigest.getInstance("SHA-256").digest(source)
    }

    fun aad(appId: Long, method: String, path: String, scope: String): ByteArray =
        "appid=$appId|method=${method.uppercase()}|path=$path|scope=$scope"
            .toByteArray(StandardCharsets.UTF_8)

    data class EncryptedRequest(
        val body: ByteArray,
        val nonceBase64Url: String,
        val headers: Map<String, String>
    )

    fun encryptJsonBody(appId: Long, secret: String, method: String, path: String, json: String): EncryptedRequest {
        val key = deriveKey(appId, secret)
        val nonce = ByteArray(12).also { SecureRandom().nextBytes(it) }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(aad(appId, method, path, "request-body"))
        val ciphertext = cipher.doFinal(json.toByteArray(StandardCharsets.UTF_8))

        return EncryptedRequest(
            body = ciphertext,
            nonceBase64Url = Base64.encodeToString(nonce, Base64.URL_SAFE or Base64.NO_WRAP),
            headers = mapOf(
                "Content-Type" to "application/octet-stream",
                "X-Aegis-Encrypted" to "1",
                "X-Aegis-Appid" to appId.toString(),
                "X-Aegis-Algorithm" to "AES-256-GCM",
                "X-Aegis-Nonce" to Base64.encodeToString(nonce, Base64.URL_SAFE or Base64.NO_WRAP),
                "X-Aegis-Plain-Content-Type" to "application/json"
            )
        )
    }
}

// 使用示例
val request = AegisAndroidCrypto.encryptJsonBody(
    appId = 10000,
    secret = "transport-secret",
    method = "POST",
    path = "/api/auth/login/password",
    json = """{"appid":10000,"account":"demo","password":"P@ssw0rd!"}"""
)
```

如果你用 `OkHttp` 发送，可直接把 `request.body` 作为二进制请求体，`request.headers` 原样写入请求头。

## 7.13.5 Android Java 共享密钥示例

```java
import android.util.Base64;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.LinkedHashMap;
import java.util.Map;

import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

public final class AegisAndroidCryptoJava {
    public static final class EncryptedRequest {
        public final byte[] body;
        public final String nonceBase64Url;
        public final Map<String, String> headers;

        public EncryptedRequest(byte[] body, String nonceBase64Url, Map<String, String> headers) {
            this.body = body;
            this.nonceBase64Url = nonceBase64Url;
            this.headers = headers;
        }
    }

    private static byte[] tryBase64Decode(String secret, int flags) {
        try {
            byte[] decoded = Base64.decode(secret, flags);
            return decoded.length > 0 ? decoded : null;
        } catch (IllegalArgumentException ex) {
            return null;
        }
    }

    public static byte[] decodeSecret(String secret) {
        byte[] decoded = tryBase64Decode(secret, Base64.URL_SAFE | Base64.NO_WRAP);
        if (decoded != null) return decoded;

        decoded = tryBase64Decode(secret, Base64.NO_WRAP);
        if (decoded != null) return decoded;

        try {
            return hexToBytes(secret);
        } catch (IllegalArgumentException ignore) {
            return secret.getBytes(StandardCharsets.UTF_8);
        }
    }

    public static byte[] deriveKey(long appId, String secret) throws Exception {
        byte[] material = decodeSecret(secret);
        String source = appId + ":" + bytesToHex(material);
        return MessageDigest.getInstance("SHA-256")
            .digest(source.getBytes(StandardCharsets.UTF_8));
    }

    public static byte[] aad(long appId, String method, String path, String scope) {
        String value = "appid=" + appId
            + "|method=" + method.toUpperCase()
            + "|path=" + path
            + "|scope=" + scope;
        return value.getBytes(StandardCharsets.UTF_8);
    }

    public static EncryptedRequest encryptJsonBody(long appId, String secret, String method, String path, String json) throws Exception {
        byte[] key = deriveKey(appId, secret);
        byte[] nonce = new byte[12];
        new SecureRandom().nextBytes(nonce);

        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.ENCRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
        cipher.updateAAD(aad(appId, method, path, "request-body"));
        byte[] ciphertext = cipher.doFinal(json.getBytes(StandardCharsets.UTF_8));

        String nonceBase64Url = Base64.encodeToString(nonce, Base64.URL_SAFE | Base64.NO_WRAP);
        Map<String, String> headers = new LinkedHashMap<>();
        headers.put("Content-Type", "application/octet-stream");
        headers.put("X-Aegis-Encrypted", "1");
        headers.put("X-Aegis-Appid", String.valueOf(appId));
        headers.put("X-Aegis-Algorithm", "AES-256-GCM");
        headers.put("X-Aegis-Nonce", nonceBase64Url);
        headers.put("X-Aegis-Plain-Content-Type", "application/json");

        return new EncryptedRequest(ciphertext, nonceBase64Url, headers);
    }

    private static String bytesToHex(byte[] bytes) {
        StringBuilder sb = new StringBuilder(bytes.length * 2);
        for (byte b : bytes) {
            sb.append(String.format("%02x", b));
        }
        return sb.toString();
    }

    private static byte[] hexToBytes(String hex) {
        if (hex.length() % 2 != 0) {
            throw new IllegalArgumentException("invalid hex");
        }
        byte[] result = new byte[hex.length() / 2];
        for (int i = 0; i < result.length; i++) {
            int index = i * 2;
            result[i] = (byte) Integer.parseInt(hex.substring(index, index + 2), 16);
        }
        return result;
    }
}

// 使用示例
// EncryptedRequest req = AegisAndroidCryptoJava.encryptJsonBody(
//     10000L,
//     "transport-secret",
//     "POST",
//     "/api/auth/login/password",
//     "{\"appid\":10000,\"account\":\"demo\",\"password\":\"P@ssw0rd!\"}"
// );
```

Android 端解密响应时，规则与请求侧一致：

- 使用同一个 `appid`
- 使用真实 HTTP 方法和路径
- AAD 中的 `scope` 改为 `response-body`
- 非 hybrid 模式继续使用 `deriveKey(appId, secret)` 的结果
- hybrid 模式则改用本次请求缓存下来的会话密钥

## 7.13.6 Android Kotlin 混合加密完整示例

下面示例补齐 Android 端混合加密闭环：

- `hybrid-rsa-xchacha20`
- `hybrid-ecdh-aes256gcm`

其中：

- RSA/ECDH 密钥协商走 Android/JCA
- `XChaCha20Poly1305` 走 `Lazysodium Android`
- `P-256` 对应 Go 服务端的 `ecdh.P256()`
- 响应解密继续复用本次请求生成出来的 `sessionKey`

```kotlin
import android.util.Base64
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import java.nio.charset.StandardCharsets
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.security.PublicKey
import java.security.SecureRandom
import java.security.spec.ECGenParameterSpec
import java.security.spec.X509EncodedKeySpec
import javax.crypto.Cipher
import javax.crypto.KeyAgreement
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

object AegisAndroidHybridKotlin {
    private val lazySodium by lazy { LazySodiumAndroid(SodiumAndroid()) }

    data class HybridRequest(
        val body: ByteArray,
        val sessionKey: ByteArray,
        val headers: Map<String, String>
    )

    private fun aad(appId: Long, method: String, path: String, scope: String): ByteArray =
        "appid=$appId|method=${method.uppercase()}|path=$path|scope=$scope"
            .toByteArray(StandardCharsets.UTF_8)

    private fun rsaPublicKeyFromPem(pem: String): PublicKey {
        val clean = pem
            .replace("-----BEGIN PUBLIC KEY-----", "")
            .replace("-----END PUBLIC KEY-----", "")
            .replace("\\s".toRegex(), "")
        val der = Base64.decode(clean, Base64.DEFAULT)
        return KeyFactory.getInstance("RSA").generatePublic(X509EncodedKeySpec(der))
    }

    private fun ecdhPublicKeyFromPem(pem: String): PublicKey {
        val clean = pem
            .replace("-----BEGIN PUBLIC KEY-----", "")
            .replace("-----END PUBLIC KEY-----", "")
            .replace("\\s".toRegex(), "")
        val der = Base64.decode(clean, Base64.DEFAULT)
        return KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(der))
    }

    private fun encryptAesGcm(key: ByteArray, nonce: ByteArray, aad: ByteArray, plaintext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(aad)
        return cipher.doFinal(plaintext)
    }

    private fun decryptAesGcm(key: ByteArray, nonce: ByteArray, aad: ByteArray, ciphertext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(aad)
        return cipher.doFinal(ciphertext)
    }

    private fun encryptXChaCha20(key: ByteArray, nonce: ByteArray, aad: ByteArray, plaintext: ByteArray): ByteArray {
        val cipher = ByteArray(plaintext.size + 16)
        val cipherLen = longArrayOf(0L)
        val ok = lazySodium.cryptoAeadXChaCha20Poly1305IetfEncrypt(
            cipher,
            cipherLen,
            plaintext,
            plaintext.size.toLong(),
            aad,
            aad.size.toLong(),
            null,
            nonce,
            key
        )
        require(ok) { "xchacha20 encrypt failed" }
        return cipher.copyOf(cipherLen[0].toInt())
    }

    private fun decryptXChaCha20(key: ByteArray, nonce: ByteArray, aad: ByteArray, ciphertext: ByteArray): ByteArray {
        val plain = ByteArray(ciphertext.size)
        val plainLen = longArrayOf(0L)
        val ok = lazySodium.cryptoAeadXChaCha20Poly1305IetfDecrypt(
            plain,
            plainLen,
            null,
            ciphertext,
            ciphertext.size.toLong(),
            aad,
            aad.size.toLong(),
            nonce,
            key
        )
        require(ok) { "xchacha20 decrypt failed" }
        return plain.copyOf(plainLen[0].toInt())
    }

    fun encryptHybridRsaXChaCha20(
        appId: Long,
        method: String,
        path: String,
        payloadJson: String,
        rsaPublicKeyPem: String
    ): HybridRequest {
        val sessionKey = ByteArray(32).also { SecureRandom().nextBytes(it) }
        val nonce = ByteArray(24).also { SecureRandom().nextBytes(it) }
        val encryptedBody = encryptXChaCha20(
            key = sessionKey,
            nonce = nonce,
            aad = aad(appId, method, path, "request-body"),
            plaintext = payloadJson.toByteArray(StandardCharsets.UTF_8)
        )

        val wrappedKey = Cipher.getInstance("RSA/ECB/OAEPWithSHA-256AndMGF1Padding").run {
            init(Cipher.ENCRYPT_MODE, rsaPublicKeyFromPem(rsaPublicKeyPem))
            doFinal(sessionKey)
        }

        return HybridRequest(
            body = encryptedBody,
            sessionKey = sessionKey,
            headers = mapOf(
                "Content-Type" to "application/octet-stream",
                "X-Aegis-Encrypted" to "1",
                "X-Aegis-Appid" to appId.toString(),
                "X-Aegis-Algorithm" to "hybrid-rsa-xchacha20",
                "X-Aegis-Nonce" to Base64.encodeToString(nonce, Base64.URL_SAFE or Base64.NO_WRAP),
                "X-Aegis-Key" to Base64.encodeToString(wrappedKey, Base64.URL_SAFE or Base64.NO_WRAP),
                "X-Aegis-Plain-Content-Type" to "application/json"
            )
        )
    }

    fun encryptHybridEcdhAes256Gcm(
        appId: Long,
        method: String,
        path: String,
        payloadJson: String,
        serverEcdhPublicKeyPem: String
    ): HybridRequest {
        val keyPair = KeyPairGenerator.getInstance("EC").apply {
            initialize(ECGenParameterSpec("secp256r1"))
        }.generateKeyPair()

        val agreement = KeyAgreement.getInstance("ECDH")
        agreement.init(keyPair.private)
        agreement.doPhase(ecdhPublicKeyFromPem(serverEcdhPublicKeyPem), true)
        val shared = agreement.generateSecret()
        val sessionKey = MessageDigest.getInstance("SHA-256").digest(shared)

        val nonce = ByteArray(12).also { SecureRandom().nextBytes(it) }
        val encryptedBody = encryptAesGcm(
            key = sessionKey,
            nonce = nonce,
            aad = aad(appId, method, path, "request-body"),
            plaintext = payloadJson.toByteArray(StandardCharsets.UTF_8)
        )

        return HybridRequest(
            body = encryptedBody,
            sessionKey = sessionKey,
            headers = mapOf(
                "Content-Type" to "application/octet-stream",
                "X-Aegis-Encrypted" to "1",
                "X-Aegis-Appid" to appId.toString(),
                "X-Aegis-Algorithm" to "hybrid-ecdh-aes256gcm",
                "X-Aegis-Nonce" to Base64.encodeToString(nonce, Base64.URL_SAFE or Base64.NO_WRAP),
                "X-Aegis-Key" to Base64.encodeToString(keyPair.public.encoded, Base64.URL_SAFE or Base64.NO_WRAP),
                "X-Aegis-Plain-Content-Type" to "application/json"
            )
        )
    }

    fun decryptHybridResponseXChaCha20(
        appId: Long,
        method: String,
        path: String,
        sessionKey: ByteArray,
        nonceBase64Url: String,
        ciphertext: ByteArray
    ): String {
        val nonce = Base64.decode(nonceBase64Url, Base64.URL_SAFE or Base64.NO_WRAP)
        val plaintext = decryptXChaCha20(
            key = sessionKey,
            nonce = nonce,
            aad = aad(appId, method, path, "response-body"),
            ciphertext = ciphertext
        )
        return String(plaintext, StandardCharsets.UTF_8)
    }

    fun decryptHybridResponseAes256Gcm(
        appId: Long,
        method: String,
        path: String,
        sessionKey: ByteArray,
        nonceBase64Url: String,
        ciphertext: ByteArray
    ): String {
        val nonce = Base64.decode(nonceBase64Url, Base64.URL_SAFE or Base64.NO_WRAP)
        val plaintext = decryptAesGcm(
            key = sessionKey,
            nonce = nonce,
            aad = aad(appId, method, path, "response-body"),
            ciphertext = ciphertext
        )
        return String(plaintext, StandardCharsets.UTF_8)
    }
}
```

调用示例：

```kotlin
val rsaReq = AegisAndroidHybridKotlin.encryptHybridRsaXChaCha20(
    appId = 10000,
    method = "POST",
    path = "/api/auth/login/password",
    payloadJson = """{"appid":10000,"account":"demo","password":"P@ssw0rd!"}""",
    rsaPublicKeyPem = serverRsaPublicKeyPem
)

val ecdhReq = AegisAndroidHybridKotlin.encryptHybridEcdhAes256Gcm(
    appId = 10000,
    method = "POST",
    path = "/api/user/profile",
    payloadJson = """{"nickname":"new-name"}""",
    serverEcdhPublicKeyPem = serverEcdhPublicKeyPem
)

val decryptedProfile = AegisAndroidHybridKotlin.decryptHybridResponseAes256Gcm(
    appId = 10000,
    method = "POST",
    path = "/api/user/profile",
    sessionKey = ecdhReq.sessionKey,
    nonceBase64Url = profileResponse.header("X-Aegis-Nonce").orEmpty(),
    ciphertext = profileResponse.body!!.bytes()
)

val decryptedLogin = AegisAndroidHybridKotlin.decryptHybridResponseXChaCha20(
    appId = 10000,
    method = "POST",
    path = "/api/auth/login/password",
    sessionKey = rsaReq.sessionKey,
    nonceBase64Url = loginResponse.header("X-Aegis-Nonce").orEmpty(),
    ciphertext = loginResponse.body!!.bytes()
)
```

## 7.13.7 Android Java 混合加密完整示例

```java
import android.util.Base64;

import com.goterl.lazysodium.LazySodiumAndroid;
import com.goterl.lazysodium.SodiumAndroid;

import java.nio.charset.StandardCharsets;
import java.security.KeyFactory;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.MessageDigest;
import java.security.PublicKey;
import java.security.SecureRandom;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.X509EncodedKeySpec;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.Map;

import javax.crypto.Cipher;
import javax.crypto.KeyAgreement;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

public final class AegisAndroidHybridJava {
    private static final LazySodiumAndroid LAZY_SODIUM = new LazySodiumAndroid(new SodiumAndroid());

    public static final class HybridRequest {
        public final byte[] body;
        public final byte[] sessionKey;
        public final Map<String, String> headers;

        public HybridRequest(byte[] body, byte[] sessionKey, Map<String, String> headers) {
            this.body = body;
            this.sessionKey = sessionKey;
            this.headers = headers;
        }
    }

    private static byte[] aad(long appId, String method, String path, String scope) {
        String value = "appid=" + appId
            + "|method=" + method.toUpperCase()
            + "|path=" + path
            + "|scope=" + scope;
        return value.getBytes(StandardCharsets.UTF_8);
    }

    private static PublicKey readRsaPublicKey(String pem) throws Exception {
        String clean = pem
            .replace("-----BEGIN PUBLIC KEY-----", "")
            .replace("-----END PUBLIC KEY-----", "")
            .replaceAll("\\s", "");
        byte[] der = Base64.decode(clean, Base64.DEFAULT);
        return KeyFactory.getInstance("RSA").generatePublic(new X509EncodedKeySpec(der));
    }

    private static PublicKey readEcPublicKey(String pem) throws Exception {
        String clean = pem
            .replace("-----BEGIN PUBLIC KEY-----", "")
            .replace("-----END PUBLIC KEY-----", "")
            .replaceAll("\\s", "");
        byte[] der = Base64.decode(clean, Base64.DEFAULT);
        return KeyFactory.getInstance("EC").generatePublic(new X509EncodedKeySpec(der));
    }

    private static byte[] encryptAesGcm(byte[] key, byte[] nonce, byte[] aad, byte[] plain) throws Exception {
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.ENCRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
        cipher.updateAAD(aad);
        return cipher.doFinal(plain);
    }

    private static byte[] decryptAesGcm(byte[] key, byte[] nonce, byte[] aad, byte[] cipherBytes) throws Exception {
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.DECRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
        cipher.updateAAD(aad);
        return cipher.doFinal(cipherBytes);
    }

    private static byte[] encryptXChaCha20(byte[] key, byte[] nonce, byte[] aad, byte[] plain) {
        byte[] out = new byte[plain.length + 16];
        long[] outLen = new long[1];
        boolean ok = LAZY_SODIUM.cryptoAeadXChaCha20Poly1305IetfEncrypt(
            out,
            outLen,
            plain,
            (long) plain.length,
            aad,
            (long) aad.length,
            null,
            nonce,
            key
        );
        if (!ok) {
            throw new IllegalStateException("xchacha20 encrypt failed");
        }
        return Arrays.copyOf(out, (int) outLen[0]);
    }

    private static byte[] decryptXChaCha20(byte[] key, byte[] nonce, byte[] aad, byte[] cipherBytes) {
        byte[] out = new byte[cipherBytes.length];
        long[] outLen = new long[1];
        boolean ok = LAZY_SODIUM.cryptoAeadXChaCha20Poly1305IetfDecrypt(
            out,
            outLen,
            null,
            cipherBytes,
            (long) cipherBytes.length,
            aad,
            (long) aad.length,
            nonce,
            key
        );
        if (!ok) {
            throw new IllegalStateException("xchacha20 decrypt failed");
        }
        return Arrays.copyOf(out, (int) outLen[0]);
    }

    public static HybridRequest encryptHybridRsaXChaCha20(long appId, String method, String path, String payloadJson, String rsaPublicKeyPem) throws Exception {
        byte[] sessionKey = new byte[32];
        byte[] nonce = new byte[24];
        new SecureRandom().nextBytes(sessionKey);
        new SecureRandom().nextBytes(nonce);

        byte[] body = encryptXChaCha20(
            sessionKey,
            nonce,
            aad(appId, method, path, "request-body"),
            payloadJson.getBytes(StandardCharsets.UTF_8)
        );

        Cipher wrapCipher = Cipher.getInstance("RSA/ECB/OAEPWithSHA-256AndMGF1Padding");
        wrapCipher.init(Cipher.ENCRYPT_MODE, readRsaPublicKey(rsaPublicKeyPem));
        byte[] wrappedKey = wrapCipher.doFinal(sessionKey);

        Map<String, String> headers = new LinkedHashMap<>();
        headers.put("Content-Type", "application/octet-stream");
        headers.put("X-Aegis-Encrypted", "1");
        headers.put("X-Aegis-Appid", String.valueOf(appId));
        headers.put("X-Aegis-Algorithm", "hybrid-rsa-xchacha20");
        headers.put("X-Aegis-Nonce", Base64.encodeToString(nonce, Base64.URL_SAFE | Base64.NO_WRAP));
        headers.put("X-Aegis-Key", Base64.encodeToString(wrappedKey, Base64.URL_SAFE | Base64.NO_WRAP));
        headers.put("X-Aegis-Plain-Content-Type", "application/json");
        return new HybridRequest(body, sessionKey, headers);
    }

    public static HybridRequest encryptHybridEcdhAes256Gcm(long appId, String method, String path, String payloadJson, String serverEcdhPublicKeyPem) throws Exception {
        KeyPairGenerator generator = KeyPairGenerator.getInstance("EC");
        generator.initialize(new ECGenParameterSpec("secp256r1"));
        KeyPair clientKeyPair = generator.generateKeyPair();

        KeyAgreement agreement = KeyAgreement.getInstance("ECDH");
        agreement.init(clientKeyPair.getPrivate());
        agreement.doPhase(readEcPublicKey(serverEcdhPublicKeyPem), true);
        byte[] shared = agreement.generateSecret();
        byte[] sessionKey = MessageDigest.getInstance("SHA-256").digest(shared);

        byte[] nonce = new byte[12];
        new SecureRandom().nextBytes(nonce);
        byte[] body = encryptAesGcm(
            sessionKey,
            nonce,
            aad(appId, method, path, "request-body"),
            payloadJson.getBytes(StandardCharsets.UTF_8)
        );

        Map<String, String> headers = new LinkedHashMap<>();
        headers.put("Content-Type", "application/octet-stream");
        headers.put("X-Aegis-Encrypted", "1");
        headers.put("X-Aegis-Appid", String.valueOf(appId));
        headers.put("X-Aegis-Algorithm", "hybrid-ecdh-aes256gcm");
        headers.put("X-Aegis-Nonce", Base64.encodeToString(nonce, Base64.URL_SAFE | Base64.NO_WRAP));
        headers.put("X-Aegis-Key", Base64.encodeToString(clientKeyPair.getPublic().getEncoded(), Base64.URL_SAFE | Base64.NO_WRAP));
        headers.put("X-Aegis-Plain-Content-Type", "application/json");
        return new HybridRequest(body, sessionKey, headers);
    }

    public static String decryptHybridResponseXChaCha20(long appId, String method, String path, byte[] sessionKey, String nonceBase64Url, byte[] ciphertext) {
        byte[] nonce = Base64.decode(nonceBase64Url, Base64.URL_SAFE | Base64.NO_WRAP);
        byte[] plain = decryptXChaCha20(
            sessionKey,
            nonce,
            aad(appId, method, path, "response-body"),
            ciphertext
        );
        return new String(plain, StandardCharsets.UTF_8);
    }

    public static String decryptHybridResponseAes256Gcm(long appId, String method, String path, byte[] sessionKey, String nonceBase64Url, byte[] ciphertext) throws Exception {
        byte[] nonce = Base64.decode(nonceBase64Url, Base64.URL_SAFE | Base64.NO_WRAP);
        byte[] plain = decryptAesGcm(
            sessionKey,
            nonce,
            aad(appId, method, path, "response-body"),
            ciphertext
        );
        return new String(plain, StandardCharsets.UTF_8);
    }
}
```

Java 调用示例：

```java
HybridRequest rsaReq = AegisAndroidHybridJava.encryptHybridRsaXChaCha20(
    10000L,
    "POST",
    "/api/auth/login/password",
    "{\"appid\":10000,\"account\":\"demo\",\"password\":\"P@ssw0rd!\"}",
    serverRsaPublicKeyPem
);

String decryptedLogin = AegisAndroidHybridJava.decryptHybridResponseXChaCha20(
    10000L,
    "POST",
    "/api/auth/login/password",
    rsaReq.sessionKey,
    loginResponse.header("X-Aegis-Nonce"),
    loginResponse.body().bytes()
);
```

如果你要切换到 `hybrid-rsa-aes256gcm` 或 `hybrid-ecdh-xchacha20`，只需要替换两处：

- 会话密钥包装方式仍然分别是 RSA-OAEP / ECDH，不变
- 业务密文算法从 `encryptAesGcm/decryptAesGcm` 与 `encryptXChaCha20/decryptXChaCha20` 之间切换，同时把 nonce 长度改成对应算法要求的 `12` 或 `24`

## 7.14 前端接入建议

推荐前端 SDK 固化以下规则：

1. 所有用户侧请求先判断应用是否启用传输加密
2. 一旦启用，对受保护路径统一加密，不要做部分接口漏加密
3. 始终发送 `X-Aegis-Appid`
4. 对 GET 使用 `_payload` 方案，对有 Body 的请求直接发送密文字节流
5. 若使用 hybrid 算法，客户端必须缓存本次请求使用的会话密钥，用于解密对应响应
6. 响应若存在 `X-Aegis-Encrypted: 1`，必须按 `response-body` AAD 解密

## 7.15 当前实现注意事项

以下是当前代码里的真实限制，接入时需要明确：

1. `strict` 字段当前会被读取和返回，但中间件没有根据 `strict` 做差异化逻辑分支。
2. `allowedAlgorithms` 现在会在请求链路严格校验，未列入白名单的算法会被拒绝。
3. hybrid-RSA 要求管理端已经为应用生成并保存 RSA 密钥对。
4. hybrid-ECDH 要求管理端已经为应用生成并保存 ECDH 密钥对。
5. `X-Aegis-Key` 只在 hybrid 算法下生效；纯对称算法不需要传这个头。

## 8. 相关接口

### 8.1 获取应用公开配置

- 方法：`GET`
- 路径：`/api/app/public`

请求参数：

- `appid`

返回数据中的 `settings.transportEncryption` 可用于判断是否启用传输加密，但不会返回 `secret`。

### 8.2 管理端查看传输加密配置

- 方法：`GET`
- 路径：`/api/admin/apps/:appkey/encryption`

该接口主要用于管理端配置查看，不属于用户侧接口，但联调加密功能时经常需要配合使用。
