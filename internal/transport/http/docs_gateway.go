package httptransport

import (
	"net/http"
	"strings"
	"unicode"

	authprotocol "aegis/internal/domain/authprotocol"
	"aegis/internal/service"

	"github.com/getkin/kin-openapi/openapi3"
)

// 应用接入网关的 OpenAPI 元数据 —— 由接入目录生成，不手工逐条维护。
//
// 这一组是**多平台客户端唯一需要生成的部分**：Android / Java / Kotlin / Swift /
// Dart 客户端从这里用 openapi-generator 产出，管理端接口只有控制台（TypeScript）会用。
// 因此三件事必须做到位，否则生成出来的客户端不可用：
//
//	operationId  —— 决定方法名。默认值是从路径拼的 post__api__v1__apps__by_appkey__auth__login，
//	                生成到 Kotlin 里没法看，这里一律显式给短名。
//	requestBody  —— 缺了它，写接口生成出来就是没有参数的空方法。
//	security     —— 缺了它，生成的客户端不会带 Authorization 头。
//
// 目录与真实路由的一致性由 TestGatewayCatalogMatchesRegisteredRoutes 保证，
// 所以这里只需要按 key 补模型，加路由时不会漏。

const gatewayDocTag = "App Gateway"

// gatewayOpenAPIPrefix 与 normalizeOpenAPIPath(":appkey") 的产物保持一致。
const gatewayOpenAPIPrefix = "/api/v1/apps/{appkey}"

// gatewayRequestModels 目录 key → 请求模型。
//
// 没有请求体也没有查询参数的接口不在这里出现（如 GET /me）；
// GET 类的模型会被摊成查询参数，其余摊成 JSON 请求体（见 BuildOpenAPISpec）。
func gatewayRequestModels() map[string]any {
	return map[string]any{
		// 认证生命周期
		"captcha":      AppCaptchaRequest{},
		"smsCode":      authprotocol.SMSCodeInput{},
		"register":     authprotocol.RegisterInput{},
		"login":        authprotocol.LoginInput{},
		"refresh":      AppRefreshRequest{},
		"secondFactor": SecondFactorVerifyRequest{},

		// 第三方登录
		"oauthURL":      AppOAuthURLRequest{},
		"oauthCallback": OAuthCallbackQuery{},
		"oauthExchange": OAuthMobileLoginRequest{},
		"oauthBindURL":  OAuthBindURLRequest{},

		// 邮箱验证码与密码
		"emailCode":           AppEmailCodeRequest{},
		"emailVerify":         AppEmailVerifyRequest{},
		"passwordForgot":      AppPasswordForgotRequest{},
		"passwordResetVerify": AppPasswordResetVerifyRequest{},
		"passwordVerify":      VerifyPasswordRequest{},
		"passwordChange":      ChangePasswordRequest{},

		// Passkey
		"passkeyOptions":  AppPasskeyOptionsRequest{},
		"passkeyLogin":    AppPasskeyLoginRequest{},
		"passkeyRegister": PasskeyRegistrationFinishRequest{},

		// 资料与设置
		"profileUpdate":  UpdateProfileRequest{},
		"profileConfirm": ConfirmProfileChangeRequest{},
		"settings":       SettingsCategoryQuery{},
		"settingsUpdate": UpdateSettingsRequest{},
		"avatarRestore":  AvatarRestoreRequest{},

		// 二次认证
		"totpEnable":              TOTPEnableRequest{},
		"totpDisable":             TOTPDisableRequest{},
		"recoveryCodesCreate":     RecoveryCodesRegenerateRequest{},
		"recoveryCodesRegenerate": RecoveryCodesRegenerateRequest{},

		// 会话与审计
		"sessionRevokeAll": UserSessionRevokeAllRequest{},
		"loginAudits":      UserLoginAuditQuery{},
		"sessionAudits":    UserSessionAuditQuery{},

		// 签到与积分
		"signin":                 SignInRequest{},
		"signinHistory":          PaginationQuery{},
		"integralTransactions":   PaginationQuery{},
		"experienceTransactions": PaginationQuery{},

		// 站内信
		"notifications":         NotificationQuery{},
		"notificationRead":      NotificationReadRequest{},
		"notificationReadBatch": NotificationReadBatchRequest{},
		"notificationClear":     NotificationClearRequest{},

		// 钱包 / 会员 / 支付
		"walletTransactions": WalletTransactionsQuery{},
		"walletConsume":      WalletConsumeRequest{},
		"vipPurchase":        VipPurchaseRequest{},
		"payOrders":          UserPaymentOrdersQuery{},
		"payOrderCreate":     CreatePaymentOrderRequest{},

		// 存储
		"storageObjectLink": StorageObjectLinkRequest{},

		// 工单
		"tickets":      TicketListQuery{},
		"ticketCreate": TicketCreateRequest{},
		"ticketReply":  TicketReplyRequest{},
		"ticketRating": TicketRatingRequest{},
		"ticketCancel": TicketCancelRequest{},

		// 内容与版本
		"versionCheck": VersionCheckQuery{},
	}
}

// gatewayRouteDocs 把接入目录展开成 OpenAPI 元数据表。
func gatewayRouteDocs() map[string]docOperation {
	models := gatewayRequestModels()
	docs := make(map[string]docOperation, len(service.GatewayOperations()))
	for _, operation := range service.GatewayOperations() {
		path := gatewayOpenAPIPrefix + operation.Path
		doc := docOperation{
			OperationID:  gatewayOperationID(operation.Key),
			Summary:      operation.Summary,
			Description:  gatewayOperationDescription(operation),
			Tags:         []string{gatewayDocTag},
			RequestModel: models[operation.Key],
		}
		if operation.Upload {
			// 上传走 multipart，不能用 JSON 模型描述，否则生成的客户端会把
			// 文件当成一个字符串字段传上去。
			doc.RequestBody = multipartUploadRequestBody()
			doc.RequestModel = nil
		}
		if operation.Auth {
			security := openapi3.SecurityRequirements{
				openapi3.NewSecurityRequirement().Authenticate("bearerAuth"),
			}
			doc.Security = &security
		} else {
			doc.Security = &openapi3.SecurityRequirements{}
		}
		docs[routeKey(operation.Method, path)] = doc
	}
	return docs
}

// gatewayOperationID 由目录 key 生成方法名：login → appLogin。
// 统一加 app 前缀是为了和管理端接口在同一份规范里区分开 ——
// 生成出来的客户端里 `appLogin()` 与 `adminLogin()` 一眼能分清。
func gatewayOperationID(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	runes[0] = unicode.ToUpper(runes[0])
	return "app" + string(runes)
}

// gatewayOperationDescription 在每条接口上说明它受哪一层包装约束。
//
// 生成式客户端的使用者读到的就是这段：不写清楚，他们会以为
// standard 档的示例在 sealed 档也能直接跑。
func gatewayOperationDescription(operation authprotocol.Operation) string {
	var builder strings.Builder
	builder.WriteString(operation.Summary)
	builder.WriteString("。\n\n")
	switch {
	case operation.Unwrapped:
		builder.WriteString("**免包装路径**：任何安全等级下都以明文可达，不需要签名或加密。")
	default:
		builder.WriteString("按应用的 `security.level` 包装：standard 直发 JSON；" +
			"signed 追加 HMAC 签名头；sealed 在此之上把载荷整体加密。" +
			"包装由 transport 适配器统一处理，请求体与响应体三档完全一致。")
	}
	if operation.Auth {
		builder.WriteString("\n\n需要用户 Bearer 令牌，且令牌必须属于路径上的这个应用（否则 40372）。")
	}
	if operation.Upload {
		builder.WriteString("\n\n`multipart/form-data` 上传，上限 32 MiB。" +
			"sealed 档下整个 multipart 体被加密，原始 Content-Type 由 " +
			"`X-Aegis-Plain-Content-Type` 头声明。")
	}
	if authprotocol.BodylessMethod(operation.Method) && !operation.Unwrapped {
		builder.WriteString("\n\nsealed 档下本接口没有请求体，密文放在 `" +
			authprotocol.SealedPayloadParam + "` 查询参数里，明文是真正的 query string。")
	}
	return builder.String()
}

// ─────────────────────────────────────────────────────────────────────
// 网关专用 DTO
//
// 这些请求体原本以匿名 struct 写在 handler 里，规范里就只能是一个空对象，
// 生成出来的客户端方法没有任何参数。提成具名类型之后它们才进得了 schema。
// ─────────────────────────────────────────────────────────────────────

type AppCaptchaRequest struct {
	// Purpose 验证码用途：login | register
	Purpose string `json:"purpose" form:"purpose"`
}

type AppRefreshRequest struct {
	RefreshToken string `json:"refreshToken" form:"refreshToken"`
	// Token 是 refreshToken 的兼容别名
	Token    string `json:"token" form:"token"`
	DeviceID string `json:"deviceId" form:"deviceId"`
	Device   string `json:"device" form:"device"`
}

type AppOAuthURLRequest struct {
	Provider string `json:"provider" form:"provider" binding:"required"`
	DeviceID string `json:"deviceId" form:"deviceId"`
	Device   string `json:"device" form:"device"`
}

// 下面四个在网关命名空间下**不需要**传 appid：应用由路径唯一确定，
// 由 injectGatewayAppID 注入（旧 /api/email/* 命名空间仍然要传）。

type AppEmailCodeRequest struct {
	Email string `json:"email" form:"email" binding:"required"`
	// Purpose 验证码用途，如 register / bind / reset
	Purpose       string `json:"purpose" form:"purpose"`
	ExpireMinutes int    `json:"expireMinutes" form:"expireMinutes"`
	ConfigName    string `json:"configName" form:"configName"`
}

type AppEmailVerifyRequest struct {
	Email   string `json:"email" form:"email" binding:"required"`
	Code    string `json:"code" form:"code" binding:"required"`
	Purpose string `json:"purpose" form:"purpose"`
}

type AppPasswordForgotRequest struct {
	Email string `json:"email" form:"email" binding:"required"`
	// ResetURL 重置页地址，验证令牌会作为查询参数附加在它后面
	ResetURL   string `json:"resetUrl" form:"resetUrl"`
	ConfigName string `json:"configName" form:"configName"`
}

type AppPasswordResetVerifyRequest struct {
	Email string `json:"email" form:"email" binding:"required"`
	Token string `json:"token" form:"token" binding:"required"`
}

type AppPasskeyOptionsRequest struct {
	Account  string `json:"account" form:"account"`
	DeviceID string `json:"deviceId" form:"deviceId"`
	Device   string `json:"device" form:"device"`
	MarkCode string `json:"markCode" form:"markCode"`
}

type AppPasskeyLoginRequest struct {
	// Credential 是 WebAuthn 断言，结构由浏览器 / 平台 API 给出
	Credential map[string]any `json:"credential" binding:"required"`
	SessionID  string         `json:"sessionId" form:"sessionId"`
	DeviceID   string         `json:"deviceId" form:"deviceId"`
	Device     string         `json:"device" form:"device"`
	MarkCode   string         `json:"markCode" form:"markCode"`
}

// gatewayDocMethods 供测试断言目录里出现的方法都被规范支持。
var gatewayDocMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodDelete: true,
}
