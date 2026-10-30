package httptransport

import (
	"strings"

	"aegis/internal/config"
	"aegis/internal/middleware"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
	"aegis/pkg/clientip"
	"aegis/pkg/crashlog"

	"go.uber.org/zap"
)

// RouterDeps 是 NewRouter 的全部依赖。
//
// 换成具名字段之前，NewRouter 有 66 个位置参数。那个签名的问题不是难看：
//
//   - 调用点写成 `NewRouter(nil, nil, … 63 个 nil …, config.CORSConfig{}, nil, "")`，
//     三处（openapi / postman / 两个测试）各一份。加一个服务就要改四处，
//     而漏改的那处**编译不过**还算运气好——两个相邻的同类型参数写反了顺序，
//     编译照过，故障要到运行时才以「某个功能行为不对」的形式出现。
//   - 同类型参数相邻是常态（这里有 50 多个 `*service.XxxService`），
//     所以类型系统在这个签名上几乎不设防。
//
// 具名字段把上面两件事都消掉：漏填一个字段得到的是零值 nil（各 handler 本来
// 就要判 nil），填错位置则因为字段名不匹配而编译不过。
type RouterDeps struct {
	// ── 认证与账号 ────────────────────────────────────────────────────
	Auth               *service.AuthService
	Admin              *service.AdminService
	User               *service.UserService
	UserMaster         *service.UserMasterService
	Security           *service.SecurityService
	Captcha            *service.CaptchaService
	Sessions           *redisrepo.SessionRepository
	SessionMgmt        *service.SessionMgmtService
	LDAP               *service.LDAPService
	OIDC               *service.OIDCService
	SAML               *service.SAMLService
	AuthProtocol       *service.AuthProtocolService
	AuthProviderHealth *service.AuthProviderHealthService
	AppOAuth           *service.AppOAuthService
	// OAuth2Server 对外的 OAuth2 / OIDC 授权服务器（Hydra 的 login / consent provider）
	OAuth2Server *service.OAuth2ServerService

	// ── 应用与平台 ────────────────────────────────────────────────────
	App              *service.AppService
	AppFunction      *service.AppFunctionService
	CardKey          *service.CardKeyService
	RewardedAd       *service.RewardedAdService
	FeatureTime      *service.FeatureTimeService
	AdPolicy         *service.AdPolicyService
	CrashReport      *service.CrashReportService
	PointsStore      *service.PointsStoreService
	Invite           *service.InviteService
	Leaderboard      *service.LeaderboardService
	AccountDeletion  *service.AccountDeletionService
	CloudStorage     *service.CloudStorageService
	Site             *service.SiteService
	Version          *service.VersionService
	Release          *service.ReleaseService
	ClientControl    ClientControlAPI
	ListenTogether   *service.ListenTogetherService
	DeviceLimit      *service.DeviceLimitService
	PlatformSettings *service.PlatformSettingsService
	PlatformBanner   *service.PlatformBannerService
	Governance       *service.PlatformGovernanceService
	Legal            *service.LegalService
	Plugin           *service.PluginService
	Template         *service.TemplateService
	Egress           *service.EgressService
	AIProvider       *service.AIProviderService
	AIAgent          *service.AIAgentService
	// AIGateway 面向应用终端用户的计费中转（/api/ai/v1 与网关里的 /ai/*）
	AIGateway *service.AIGatewayService
	// AIRelay 中转的请求体上限与读请求体期限（AI_RELAY_*，见 config/ai_relay.go）
	AIRelay config.AIRelayConfig

	// ── 组织架构 ──────────────────────────────────────────────────────
	Organization    *service.OrganizationService
	OrgApproval     *service.OrgApprovalService
	RoleApplication *service.RoleApplicationService

	// ── 增值能力 ──────────────────────────────────────────────────────
	SignIn       *service.SignInService
	AutoSign     *service.AutoSignService
	Points       *service.PointsService
	Lottery      *service.LotteryService
	ItemGrant    *service.ItemGrantService
	Level        *service.LevelService
	Wallet       *service.WalletService
	Vip          *service.VipService
	Payment      *service.PaymentService
	Workflow     *service.WorkflowService
	Ticket       *service.TicketService
	Announcement *service.AnnouncementService

	// ── 消息与通知 ────────────────────────────────────────────────────
	Notification *service.NotificationService
	Notify       *service.NotifyHub
	AdminInbox   *service.AdminInboxService
	Email        *service.EmailService
	Realtime     *service.RealtimeService
	OnlineLive   *service.OnlineLiveHub

	// ── 存储与媒体 ────────────────────────────────────────────────────
	Storage         *service.StorageService
	StorageResource *service.StorageResourceService
	Avatar          *service.AvatarService

	// ── 风控与观测 ────────────────────────────────────────────────────
	Risk            *service.RiskService
	FirewallLog     *service.FirewallLogService
	IPBan           *service.IPBanService
	GeoBan          *service.GeoBanService
	GeoFence        *service.GeoFenceService
	GeoAnalytics    *service.GeoAnalyticsService
	Location        *service.LocationService
	Audit           *service.AuditService
	Monitor         *service.MonitorService
	Dashboard       *service.DashboardService
	Report          *service.ReportService
	DeviceMarketing *service.DeviceMarketingService
	MemoryManager   *service.MemoryManager
	DatabaseManager *service.DatabaseManager

	// ── 基础设施 ──────────────────────────────────────────────────────
	Firewall *middleware.Firewall
	// TrafficRamp 突发流量爬坡（自适应准入），挂在 Firewall 之后：
	// 先由防火墙拒掉恶意流量，剩下的合法洪峰再整形。nil 时中间件直通。
	TrafficRamp *middleware.TrafficRamp
	ReplayGuard *middleware.ReplayGuard
	CrashLog    *crashlog.Logger
	Logger      *zap.Logger
	CORS        config.CORSConfig
	// ClientIP 真实客户端 IP 判定器，由 middleware.ClientIP 挂在中间件栈首位。
	// 为 nil 时（openapi / routes 等零依赖装配）退化为直接使用直连地址。
	ClientIP *clientip.Resolver
	// ClientControlGate 版本管控的网关闸门判定来源，通常与 ClientControl 是同一个服务；nil 时不拦截。
	ClientControlGate middleware.ClientControlGateService
	// DocsPortalURL 是 /docs 的 302 目标；相对路径在配了 ConsoleBaseURL 时
	// 会拼到控制台域名下（门户本就是控制台的一个页面），见 resolveDocsPortalURL。
	DocsPortalURL string
	// ConsoleBaseURL 控制台对外地址，首页的「控制台」入口用它；留空则首页不放入口。
	ConsoleBaseURL string
}

// newHandler 把依赖装进 Handler。
//
// 两边字段名不同（`RouterDeps.PlatformSettings` ↔ `Handler.system`）是既有命名的
// 遗留，这里逐个对应而不是顺手改名：Handler 的字段名在近千条注册与上百个
// handler 方法里被引用，重命名的收益远小于它带来的 diff。
func (d RouterDeps) newHandler() *Handler {
	return &Handler{
		auth:               d.Auth,
		admin:              d.Admin,
		user:               d.User,
		signin:             d.SignIn,
		autoSign:           d.AutoSign,
		points:             d.Points,
		notifications:      d.Notification,
		app:                d.App,
		site:               d.Site,
		version:            d.Version,
		release:            d.Release,
		clientControl:      d.ClientControl,
		listenTogether:     d.ListenTogether,
		deviceLimit:        d.DeviceLimit,
		roleApp:            d.RoleApplication,
		email:              d.Email,
		payment:            d.Payment,
		wallet:             d.Wallet,
		vip:                d.Vip,
		workflow:           d.Workflow,
		storage:            d.Storage,
		avatar:             d.Avatar,
		monitor:            d.Monitor,
		realtime:           d.Realtime,
		onlineLive:         d.OnlineLive,
		system:             d.PlatformSettings,
		security:           d.Security,
		captcha:            d.Captcha,
		firewallLog:        d.FirewallLog,
		ipBan:              d.IPBan,
		geoBan:             d.GeoBan,
		geoFence:           d.GeoFence,
		geoAnalytics:       d.GeoAnalytics,
		location:           d.Location,
		lottery:            d.Lottery,
		itemGrant:          d.ItemGrant,
		level:              d.Level,
		announcement:       d.Announcement,
		ldapSvc:            d.LDAP,
		oidcSvc:            d.OIDC,
		samlSvc:            d.SAML,
		sessions:           d.Sessions,
		org:                d.Organization,
		tmpl:               d.Template,
		audit:              d.Audit,
		plugin:             d.Plugin,
		appFunction:        d.AppFunction,
		cardKey:            d.CardKey,
		rewardedAd:         d.RewardedAd,
		featureTime:        d.FeatureTime,
		adPolicy:           d.AdPolicy,
		crashReport:        d.CrashReport,
		pointsStore:        d.PointsStore,
		invite:             d.Invite,
		leaderboard:        d.Leaderboard,
		accountDeletion:    d.AccountDeletion,
		cloudStorage:       d.CloudStorage,
		authProtocol:       d.AuthProtocol,
		dashboard:          d.Dashboard,
		orgApproval:        d.OrgApproval,
		sessionMgmt:        d.SessionMgmt,
		storageResource:    d.StorageResource,
		userMaster:         d.UserMaster,
		report:             d.Report,
		risk:               d.Risk,
		deviceMarketing:    d.DeviceMarketing,
		platformBanner:     d.PlatformBanner,
		ticket:             d.Ticket,
		notify:             d.Notify,
		adminInbox:         d.AdminInbox,
		appOAuth:           d.AppOAuth,
		authProviderHealth: d.AuthProviderHealth,
		egress:             d.Egress,
		governance:         d.Governance,
		legal:              d.Legal,
		aiProvider:         d.AIProvider,
		aiAgent:            d.AIAgent,
		aiGateway:          d.AIGateway,
		aiRelayMaxBody:     d.AIRelay.MaxBodyBytes,
		oauth2:             d.OAuth2Server,
		aiAgentWSOrigin:    service.WebSocketOriginChecker(d.CORS.AllowOrigins),
		consoleURL:         strings.TrimRight(strings.TrimSpace(d.ConsoleBaseURL), "/"),
		docsURL:            resolveDocsPortalURL(d.DocsPortalURL, d.ConsoleBaseURL),
	}
}
