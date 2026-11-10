# 通行密钥（Passkey）

Aegis 用 [go-webauthn](https://github.com/go-webauthn/webauthn) 做 WebAuthn 依赖方（Relying Party）：
注册时校验认证器的公钥与证明，登录时校验签名、挑战、来源与 RP ID 哈希。私钥始终留在用户设备的密码管理器里。

## 两种依赖方

| | 平台级（默认） | 应用级（独立依赖方） |
|---|---|---|
| 配置位置 | 平台配置 · 系统安全 · Passkey（`SECURITY_PASSKEY_*`） | 控制台 · 应用 · 通行密钥（`apps.settings.passkey`） |
| RP ID | 跟随请求的 Origin（见 `service/security_passkey_rp.go`） | 固定为配置值 |
| 允许来源 | `SECURITY_PASSKEY_RP_ORIGINS` | 配置的网页源 + 每个安卓证书推出的 `android:apk-key-hash:…` |
| 适用 | 控制台、浏览器内的网页 | 原生 App（安卓 Credential Manager），以及同域名的网站 |

原生 App 的请求没有 Origin，clientDataJSON 里的来源是 `android:apk-key-hash:<签名证书 SHA-256 的 base64url（无填充）>`，
平台级配置给不出可用的 RP ID，也不会有这种来源，所以**要在 App 里用通行密钥，必须配应用级依赖方**。

配了应用级依赖方之后，这个应用的注册 Begin/Finish、登录 Begin/Verify 四个入口全部按它校验；
显式关闭（`enabled=false`）时四个入口都返回 42214。

## 配置

```json
{
  "enabled": true,
  "rpId": "voyage.example.com",
  "rpName": "Voyage",
  "origins": ["https://voyage.example.com"],
  "android": [
    {
      "packageName": "com.example.voyage",
      "sha256CertFingerprints": ["97:DB:8B:…:AD:18"]
    }
  ]
}
```

- `rpId`：网站域名，不带协议、端口、路径。凭据绑定在它上面，**改了之后已创建的通行密钥全部失效**。
- `origins`：https 源，主机必须是 `rpId` 或其子域（本地开发允许 `http://localhost`）。
- `sha256CertFingerprints`：`keytool -list -v` 输出的 SHA-256，冒号或无分隔均可，保存时规范成大写冒号。
  APK 签名轮换（v3.1 / v3.2）时把新旧证书都登记上。

管理接口：`GET|PUT /api/admin/apps/:appkey/passkey`（读取需 `app:read`，修改需 `app:write`，审计等级高）。
返回里带推导出的完整来源 `allowedOrigins` 与应发布的 `assetLinks`。

## Digital Asset Links

Credential Manager 创建或使用通行密钥前，会抓取 `https://<rpId>/.well-known/assetlinks.json`，
核对请求方的包名与签名证书在不在 `delegate_permission/common.get_login_creds` 声明里；不在就报 `SecurityError`。

Aegis 在 `GET /api/public/apps/:appkey/passkey/assetlinks.json` 提供一份按配置生成的副本（免鉴权、原样数组），
RP ID 域名的网站把 `/.well-known/assetlinks.json` 反代到这里即可，指纹只在控制台维护一处。
Voyage 官网的实现见 `website/app/.well-known/assetlinks.json/route.ts`。

## 客户端流程（应用网关）

`/config` 的 `auth.passkey`：`{enabled, rpId}`。`rpId` 只在配了应用级依赖方时下发，原生客户端没有它就不显示入口。

登录（可发现凭据，不需要先填账号）：

1. `POST /auth/passkey/options` `{deviceId?}` → `{session: {challengeId, expiresAt}, options: {publicKey: {...}}}`
2. 把 `options.publicKey` 原样交给平台 API（安卓 `GetPublicKeyCredentialOption(requestJson)`）
3. `POST /auth/passkey/login` `{challengeId, credential: <authenticationResponseJson>, deviceId?, device?}`
   → 与密码登录相同的登录结果（可能要求两步验证或设备数选择）

注册（已登录）：

1. `POST /me/passkeys/options` → 同上形状，`publicKey` 里带 `user` 与 `excludeCredentials`
2. 平台 API 创建（安卓 `CreatePublicKeyCredentialRequest(requestJson)`）
3. `POST /me/passkeys` `{challengeId, credential: <registrationResponseJson>, credentialName?}`

管理：`GET /me/passkeys`（含 `aaguid`，可据此认出密码管理器）、`DELETE /me/passkeys/{credentialId}`。
删除后建议客户端调用 Credential Manager 的 `SignalUnknownCredentialRequest` 让密码管理器同步移除。

Kotlin SDK：`auth.passkeyOptions(deviceId)`、`auth.passkeyLogin(credential: JsonElement, challengeId, deviceId, device)`、
`me.passkeyRegisterOptions()`、`me.registerPasskey(challengeId, credential: JsonElement, name)`、`me.deletePasskey(id)`。

## 控制台管理员登录

管理员在「账户安全」绑定的通行密钥（`/api/admin/profile/passkey/*`，平台依赖方，要求可发现凭据）可以直接登录控制台：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/auth/passkey/options` | 发起：返回 `session.challengeId` 与 `options`（`navigator.credentials.get` 的选项，不带 `allowCredentials`，不需要账号） |
| POST | `/api/admin/auth/passkey/login` | `{challengeId, credential}`，`credential` 是断言的 WebAuthn JSON；成功返回体与密码登录相同 |

- 用户验证定为**必须**（指纹、面容或 PIN）。通行密钥本身就是多因素，通过后直接签发会话，开着 TOTP 也不再要第二步；没有用户验证的断言被拒。
- 凭据 ID 全表唯一，从它与 user handle（`a:<管理员 ID>`）反查管理员；两者对不上、凭据未登记一律 40107。
- 挑战在校验前用 GETDEL 取走，同一份断言只能交一次，失败也要重新发起（40057）。挑战与应用用户的登录挑战同一存储，靠 `admin_passkey_login` 前缀与 AppID=0 区分，互相不能冒用。
- 风控走管理员登录场景（`SceneAdminLogin`，`extra.method = passkey`）：发起时按 IP / UA 评估一次，认出管理员后带上账号再评估一次；插件的登录前钩子与会话签发钩子照常触发；账号停用照常拒绝（40310）。不要求图形验证码。
- 审计：`auth.passkey.login`（成功）与 `auth.passkey.login.failed`，失败时若已认出管理员会带上账号。
- 登录页两个入口：账号输入框 `autocomplete="username webauthn"` 的自动填充（条件式 UI，挑战快过期时自动换新），以及「使用通行密钥登录」按钮（挑战预取，按下后直接调起选择框，满足 Safari 的用户手势要求）。

## 错误码

| 码 | 含义 |
|---|---|
| 40052 / 40057 | 注册 / 登录挑战已失效或不存在 |
| 40054 / 40107 | 注册 / 登录校验失败（来源、RP ID、签名不符；具体原因只记在服务端日志 `passkey … rejected` 的 detail 里） |
| 40110 | 登录时服务端不认识这把凭据（已从账号删除） |
| 42214 | 应用已关闭通行密钥 |
| 42215 | 挑战签发后应用的 RP ID 被改，需重新发起 |
| 42216 | 控制台保存的配置不合法 |

## 测试

`go test ./internal/service -run 'TestAndroidPasskey|TestAndroidApkKeyHash|TestNormalizePasskeyPolicy'`：
用软件认证器按规范拼出注册与登录响应，以 `android:apk-key-hash` 来源走应用级依赖方完整验签，并确认别的证书签的 App 被拒。

`TestAdminPasskeyLoginIntegration`（Postgres + miniredis）：管理员注册通行密钥后登录直接签发会话（开着 TOTP 也不要第二步），
并确认重放、来源不符、缺少用户验证、user handle 不符、未登记凭据、非管理员挑战、停用账号都被拒。
