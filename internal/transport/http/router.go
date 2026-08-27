package httptransport

import (
	"fmt"
	"net/http"
	"strings"

	"aegis/internal/middleware"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
	"aegis/pkg/response"
	"aegis/pkg/tracing"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	auth            *service.AuthService
	admin           *service.AdminService
	user            *service.UserService
	signin          *service.SignInService
	points          *service.PointsService
	notifications   *service.NotificationService
	app             *service.AppService
	site            *service.SiteService
	version         *service.VersionService
	roleApp         *service.RoleApplicationService
	email           *service.EmailService
	payment         *service.PaymentService
	wallet          *service.WalletService
	vip             *service.VipService
	workflow        *service.WorkflowService
	storage         *service.StorageService
	avatar          *service.AvatarService
	monitor         *service.MonitorService
	realtime        *service.RealtimeService
	system          *service.PlatformSettingsService
	security        *service.SecurityService
	captcha         *service.CaptchaService
	firewallLog     *service.FirewallLogService
	ipBan           *service.IPBanService
	geoBan          *service.GeoBanService
	geoFence        *service.GeoFenceService
	geoAnalytics    *service.GeoAnalyticsService
	location        *service.LocationService
	lottery         *service.LotteryService
	announcement    *service.AnnouncementService
	ldapSvc         *service.LDAPService
	oidcSvc         *service.OIDCService
	samlSvc         *service.SAMLService
	sessions        *redisrepo.SessionRepository
	org             *service.OrganizationService
	tmpl            *service.TemplateService
	audit           *service.AuditService
	plugin          *service.PluginService
	appFunction     *service.AppFunctionService
	cardKey         *service.CardKeyService
	authProtocol    *service.AuthProtocolService
	dashboard       *service.DashboardService
	orgApproval     *service.OrgApprovalService
	sessionMgmt     *service.SessionMgmtService
	storageResource *service.StorageResourceService
	userMaster      *service.UserMasterService
	report          *service.ReportService
	risk            *service.RiskService
	deviceMarketing *service.DeviceMarketingService
	platformBanner  *service.PlatformBannerService
	// ticket 工单系统（管理端 + 用户端），权限判定在 service 层收敛
	ticket *service.TicketService
	// notify 统一通知出口（飞书/钉钉/企微/Slack/Webhook/邮件/站内信/实时）
	notify *service.NotifyHub
	// adminInbox 管理员站内收件箱（与用户站内信分表，主键空间不同）
	adminInbox *service.AdminInboxService
	// appOAuth 应用级第三方登录渠道配置中心（管理端 CRUD + 运行期解析）
	appOAuth *service.AppOAuthService
	// authProviderHealth 统一探测 LDAP/OIDC/SAML 可用性（单例 + 30s TTL 缓存）
	// 仅在管理员列表等需要标注"账号可能无法登录"的超管场景被调用
	authProviderHealth *service.AuthProviderHealthService
	// egress 出海代理网关管理面（端点 / 规则 / 自测 / 路由解释）
	egress *service.EgressService
	// governance 平台治理：全站应用的冻结 / 限制 / 封禁 / 归档与申诉
	governance *service.PlatformGovernanceService
	// legal 法律文本（用户协议 / 隐私政策），公开读 + 超管写
	legal *service.LegalService
	// aiProvider AI 供应商通道（系统级 + 应用级）；aiAgent Agent 会话与 SSE 流。
	aiProvider *service.AIProviderService
	aiAgent    *service.AIAgentService
}

// NewRouter 装配整张路由表。依赖以具名字段传入，理由见 RouterDeps 的注释。
func NewRouter(deps RouterDeps) (*gin.Engine, error) {
	// 中间件栈里直接用到的依赖起个短名。
	//
	// 这不是把 66 个位置参数换成结构体之后又还原回去：具名字段的价值在**调用点**
	// （填错位置编译不过、漏填得到 nil 而不是错位的服务），而注册代码里
	// `middleware.AdminAccess(deps.Admin, deps.App, deps.Governance)` 只是更长，
	// 并不更安全。各 register* 函数头部同样只声明自己那几个。
	var (
		appService          = deps.App
		locationService     = deps.Location
		authProtocolService = deps.AuthProtocol
		firewall            = deps.Firewall
		replayGuard         = deps.ReplayGuard
		cl                  = deps.CrashLog
		log                 = deps.Logger
		corsConfig          = deps.CORS
		clientIPResolver    = deps.ClientIP
		docsPortalURL       = deps.DocsPortalURL
	)
	// 接住 gin 的路由调试回调：既灭掉 debug 档下每条路由一行的滚屏，
	// 也顺手把中间件链深度记下来（那个数只在这个回调里出现）。
	// 详见 route_chains.go；清单的渲染入口是 RouteInventory。
	defer captureRouteChains()()

	router := gin.New()
	// gin 自己那套转发头解析必须关掉（nil = 不信任任何对端 → ClientIP 直接返回
	// RemoteAddr）。客户端 IP 由 middleware.ClientIP 判定后**改写 RemoteAddr**，
	// 两套解析同时开着只会让「到底谁说了算」变成一个没人答得上来的问题：
	// gin 只认 X-Forwarded-For / X-Real-IP，不认 RFC 7239 Forwarded、不支持预设网段、
	// 也不区分「平台注入的头」与「客户端自己写的头」。详见 middleware/client_ip.go。
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("重置 gin 可信代理失败: %w", err)
	}
	router.HandleMethodNotAllowed = true
	// 显式声明 multipart 内存缓冲上限：32MB
	// Gin 默认 32MB，这里显式写出便于一眼读懂上传体量策略；
	// 超过该值的部分会 spill 到临时文件，不影响成功率，仅影响解析成本。
	router.MaxMultipartMemory = 32 << 20
	// 空 CORS 配置会让浏览器的每一个写请求都吃 403（同源 POST 也带 Origin），
	// 而那个 403 没有 body 也没有日志。在启动时说一次，比让人从空白页倒查回来强。
	if warning := middleware.CORSGuardWarning(corsConfig); warning != "" && log != nil {
		log.Warn(warning)
	}
	router.Use(
		// 必须是第一个：它之后的每一环（访问日志、防火墙限流与封禁、WAF、追踪、
		// 地理定位）都要取客户端 IP，排在它前面的那些取到的会是反代地址。
		middleware.ClientIP(clientIPResolver, log),
		middleware.RequestID(),
		middleware.RequestOrigin(),
		middleware.CrashRecovery(log, cl),
		tracing.GinMiddleware("aegis", "/healthz", "/readyz"),
		middleware.CORS(corsConfig),
		// 访问日志走 zap 而不是 gin.Logger()：后者按自己的格式写 stdout，
		// 与平台其余部分的结构化输出混在一起，采集端每条都解析失败。见 access_log.go。
		middleware.AccessLog(log, middleware.AccessLogSkipPaths()...),
		firewall.Handler(),
		replayGuard.Handler(),
		middleware.AppGateway(authProtocolService),
		middleware.AppEncryption(appService),
		middleware.Location(locationService),
	)
	router.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, 40400, "请求的页面不存在")
	})
	router.NoMethod(methodNotAllowed)

	h := deps.newHandler()

	// 注册按域拆分到 router_*.go，这里只负责编排。
	//
	// **这个顺序不能动，也不要按字母重排。** gin 用前缀树存路由，静态段与参数段
	// 在同一层共存时是否 panic 与注册先后有关（`/organizations/tree` 与
	// `/organizations/:orgId` 就是这种共存，TestOrgRoutesRegisterWithoutConflict
	// 专门守着它）。下面这十六行严格复刻拆分前的原始顺序 ——
	// 重排等于在一次纯搬迁里夹带一个行为变更，而它只会在启动时才炸。
	//
	// 整表由 testdata/routes.golden 钉住，改动路由后用
	// `go test ./internal/transport/http/ -run TestRouteTableMatchesGoldenSnapshot -update-route-golden` 重写。
	registerPublicRoutes(router, h, deps)
	registerGatewayRoutes(router, h, deps)
	registerPublicCaptchaAndLegalRoutes(router, h, deps)
	registerAdminAuthRoutes(router, h, deps)
	registerAppCompatRoutes(router, h, deps)
	registerLegacyAuthRoutes(router, h, deps)
	registerAdminAppRoutes(router, h, deps)
	registerPlatformGovernanceRoutes(router, h, deps)
	registerAdminModuleRoutes(router, h, deps)
	registerUserRoutes(router, h, deps)
	registerAdminAppConfigRoutes(router, h, deps)
	registerPlatformStorageRoutes(router, h, deps)
	registerCommerceRoutes(router, h, deps)
	registerWorkflowCompatRoutes(router, h, deps)
	registerAdminSystemRoutes(router, h, deps)
	registerPlatformBannerActiveRoute(router, h, deps)

	docsOptions := DefaultDocsOptions()
	if trimmed := strings.TrimSpace(docsPortalURL); trimmed != "" {
		docsOptions.PortalURL = trimmed
	}
	if err := RegisterDocsRoutes(router, docsOptions); err != nil {
		return nil, fmt.Errorf("register docs routes: %w", err)
	}

	return router, nil
}
