package service

import (
	"aegis/internal/domain/accountdeletion"
	"aegis/pkg/egress"
	"aegis/pkg/sessiontoken"
	riskevents "aegis/services/risk/events"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	devicedomain "aegis/internal/domain/device"
	"aegis/internal/domain/devicelimit"
	oauthdomain "aegis/internal/domain/oauth"
	platformdomain "aegis/internal/domain/platform"
	plugindomain "aegis/internal/domain/plugin"
	"aegis/internal/domain/regip"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	inviteCodeLength      = 24
	inviteCodeMaxAttempts = 16
	inviteCodeCharset     = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

type AuthService struct {
	// publicIDs 主键 ↔ 对外用户 ID（与 UserService 共用一份，见 SetPublicIDs）
	publicIDs   *UserPublicIDs
	cfg         config.Config
	log         *zap.Logger
	pg          *pgrepo.Repository
	sessions    *redisrepo.SessionRepository
	publisher   *event.Publisher
	app         *AppService
	security    *SecurityService
	providers   map[string]*OAuthProvider
	appOAuth    *AppOAuthService
	http        *http.Client
	plugin      *PluginService
	risk        *RiskService
	search      *AdminUserSearchService
	loginGuard  *LoginGuardService
	consistency *LoginConsistencyService
	// governance 平台治理判定：应用被冻结 / 封禁时，连已经签发的会话也当场失效
	governance *PlatformGovernanceService
	// devices 设备字典，见 SetDeviceDirectory
	devices *DeviceMarketingService
	// cardKey 卡密登录。未注入时 cardkey 登录方式直接报「未启用」而不是空指针
	cardKey *CardKeyService
	// recoveryEmail / recoveryCaptcha / recoveryUsers 找回密码的发信、短信与会话治理，见 SetPasswordRecovery
	recoveryEmail   passwordRecoveryMailer
	recoveryCaptcha *CaptchaService
	recoveryUsers   *UserService
	registerFlight  singleflight.Group
	// tokens 终端用户登录令牌（PASETO v4.local）的签发与校验，见 pkg/sessiontoken。
	// 测试里直接以字面量构造 AuthService 时为空，按 cfg 懒构造一次。
	tokens     *sessiontoken.Codec
	tokensOnce sync.Once
	tokensErr  error
	// timingDummyHash 时序均衡用哑 bcrypt 哈希：账号不存在或账号无密码时，
	// 也执行一次等价代价的 bcrypt 比较，使「账号不存在」与「密码错误」耗时不可区分，
	// 防止通过响应时间差枚举有效账号
	timingDummyHash []byte
	deletion        *AccountDeletionService
	// loginObserver 每次真正签发会话之后的回调（物品发放的「登录成功」事件）；回调自己负责不阻塞。
	loginObserver func(appID, userID int64, provider string)
	// notifications 站内信出口（密码已重置等），见 SetNotificationService
	notifications notificationDeliverer
	// deviceLimit 登录设备数控制，见 SetDeviceLimitService；未注入时不做限制
	deviceLimit *DeviceLimitService
	// registerIP 注册 IP 控制，见 SetRegisterIPService；未注入时不做限制
	registerIP *RegisterIPService
}

// SetRegisterIPService 接上注册 IP 控制（docs/register-ip.md）。
func (s *AuthService) SetRegisterIPService(registerIP *RegisterIPService) {
	s.registerIP = registerIP
}

// SetDeviceLimitService 接上登录设备数控制（docs/device-limit.md）。
func (s *AuthService) SetDeviceLimitService(deviceLimit *DeviceLimitService) {
	s.deviceLimit = deviceLimit
}

// SetNotificationService 接上站内信：安全类提醒经 NotificationService.Deliver 投递并实时推送。
// SetPublicIDs 接上全进程共用的对外 ID 换算（UserService.PublicIDs）。
func (s *AuthService) SetPublicIDs(publicIDs *UserPublicIDs) {
	s.publicIDs = publicIDs
}

// PublicIDs 主键 ↔ 对外用户 ID 的换算；未接上时为 nil（换算结果全为 0）。
func (s *AuthService) PublicIDs() *UserPublicIDs {
	if s == nil {
		return nil
	}
	return s.publicIDs
}

func (s *AuthService) SetNotificationService(notifications notificationDeliverer) {
	s.notifications = notifications
}

type PasswordRegisterInput struct {
	AppID           int64
	Account         string
	Password        string
	Nickname        string
	Profile         map[string]any
	SuppressSession bool
	DeviceID        string // 设备唯一识别码
	Device          string // 设备可读名称
	IP              string
	UserAgent       string
}

// SetAccountDeletion 接上账号注销：登录结果带上冷静期状态（按配置登录即撤销），注册前核对重新注册限制。
// SetLoginObserver 注册登录观察者，见 loginObserver。
func (s *AuthService) SetLoginObserver(fn func(appID, userID int64, provider string)) {
	s.loginObserver = fn
}

func (s *AuthService) SetAccountDeletion(deletion *AccountDeletionService) {
	s.deletion = deletion
}

// checkReregister 注册前核对：刚注销、还在限制期内的标识不能再注册。
func (s *AuthService) checkReregister(ctx context.Context, appID int64, identifiers ...RegisterIdentifier) error {
	// 全站封禁：邮箱、手机号、第三方身份撞上了就不让注册
	if sus := suspensionHooks.Load(); sus != nil {
		if err := sus.CheckRegistration(ctx, identifiers...); err != nil {
			return err
		}
	}
	if s.deletion == nil {
		return nil
	}
	return s.deletion.CheckReregister(ctx, appID, identifiers...)
}

// SetPluginService 注入插件服务
func (s *AuthService) SetPluginService(plugin *PluginService) {
	s.plugin = plugin
}

// SetRiskService 注入风控服务
func (s *AuthService) SetRiskService(risk *RiskService) {
	s.risk = risk
}

// SetLoginConsistency 注入登录一致性校验（设备绑定 / 登录 IP / 登录属地）。
// 未注入时三项应用策略降级为不判定。
func (s *AuthService) SetLoginConsistency(consistency *LoginConsistencyService) {
	s.consistency = consistency
}

// SetGovernanceService 注入平台治理服务（bootstrap 中调用）。
func (s *AuthService) SetGovernanceService(governance *PlatformGovernanceService) {
	s.governance = governance
}

// SetAppOAuthService 注入应用级第三方登录配置中心。
// 注入后所有 OAuth 链路改用「应用级配置 → 平台级 .env 兜底」的解析顺序；
// 未注入时（离线导出 / 单测）沿用进程内的平台级配置。
func (s *AuthService) SetAppOAuthService(appOAuth *AppOAuthService) {
	s.appOAuth = appOAuth
}

// SetLoginGuard 注入登录防爆破服务
func (s *AuthService) SetLoginGuard(guard *LoginGuardService) {
	s.loginGuard = guard
}

func (s *AuthService) SetAdminUserSearchService(search *AdminUserSearchService) {
	s.search = search
}

// SetCardKeyService 注入卡密服务，启用「卡密即登录凭证」这一档登录方式。
// SetDeviceDirectory 注入设备字典：扫码登录的发起端设备据此给出 deviceInfo。
func (s *AuthService) SetDeviceDirectory(devices *DeviceMarketingService) {
	s.devices = devices
}

func (s *AuthService) SetCardKeyService(cardKey *CardKeyService) {
	s.cardKey = cardKey
}

func NewAuthService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher, app *AppService, security *SecurityService) *AuthService {
	providers := map[string]*OAuthProvider{}
	for name, providerCfg := range cfg.OAuth {
		providers[name] = NewOAuthProvider(providerCfg)
	}
	// 启动时生成一次随机哑哈希（永不匹配任何真实密码），用于登录时序均衡
	timingDummyHash, err := bcrypt.GenerateFromPassword([]byte(uuid.NewString()), bcrypt.DefaultCost)
	if err != nil {
		// bcrypt 生成仅在 cost 非法时出错，此处不可能发生；兜底使用固定哑哈希
		timingDummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMye1J9XfP0Zp1Gm8mFhT0q5o6S7Yp8mGeW")
		log.Warn("generate timing dummy hash failed, fallback to static", zap.Error(err))
	}
	return &AuthService{
		cfg:       cfg,
		log:       log,
		pg:        pg,
		sessions:  sessions,
		publisher: publisher,
		app:       app,
		security:  security,
		providers: providers,
		// OAuth 令牌交换与用户信息拉取几乎都是境外端点，走出海网关
		http:            egress.NewClient(egress.Profile{Name: "auth.oauth", Timeout: 8 * time.Second}),
		tokens:          mustSessionTokenCodec(cfg),
		timingDummyHash: timingDummyHash,
	}
}

func sessionTokenOptions(cfg config.Config) sessiontoken.Options {
	return sessiontoken.Options{
		Issuer:          cfg.JWT.Issuer,
		Key:             cfg.JWT.SessionTokenKey,
		PreviousKeys:    cfg.JWT.SessionTokenPreviousKeys,
		Secret:          cfg.JWT.Secret,
		AcceptLegacyJWT: cfg.JWT.AcceptLegacyJWT,
	}
}

// mustSessionTokenCodec 配置在 config.Load 里已校验过，这里出错只可能是测试里的半截配置，交给懒构造报错。
func mustSessionTokenCodec(cfg config.Config) *sessiontoken.Codec {
	codec, err := sessiontoken.New(sessionTokenOptions(cfg))
	if err != nil {
		return nil
	}
	return codec
}

func (s *AuthService) tokenCodec() (*sessiontoken.Codec, error) {
	if s.tokens != nil {
		return s.tokens, nil
	}
	s.tokensOnce.Do(func() {
		s.tokens, s.tokensErr = sessiontoken.New(sessionTokenOptions(s.cfg))
	})
	return s.tokens, s.tokensErr
}

// runDetached 在独立 goroutine 中执行非关键路径副作用（审计 / 计数清理 / 插件钩子等）。
// 与请求生命周期解耦：请求返回不等待这些操作；统一带超时与 panic 防护。
func (s *AuthService) runDetached(name string, timeout time.Duration, fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("detached task panic", zap.String("task", name), zap.Any("panic", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		fn(ctx)
	}()
}

func (s *AuthService) PasswordLogin(ctx context.Context, appID int64, account, password, deviceID, device, ip, userAgent string) (*authdomain.LoginResult, error) {
	app, user, account, err := s.authenticatePassword(ctx, appID, account, password, deviceID, device, ip, userAgent)
	if err != nil {
		return nil, err
	}

	// 阶段 3 —— 签发会话（app 沿调用链下传，全程仅拉取一次）
	result, err := s.completeLogin(ctx, app, user, "password", "password", deviceID, device, ip, userAgent)
	if err != nil {
		return nil, err
	}
	s.afterPasswordLoginSuccess(appID, user, account, ip)
	return result, nil
}

// afterPasswordLoginSuccess 成功侧副作用全部异步：清失败计数 / 会话签发钩子，不增加登录响应时延。
func (s *AuthService) afterPasswordLoginSuccess(appID int64, user *userdomain.User, account, ip string) {
	if s.loginGuard != nil {
		s.runDetached("login.guard_success", 3*time.Second, func(actx context.Context) {
			s.loginGuard.RegisterSuccess(actx, appID, account, ip)
		})
	}
	if s.plugin != nil {
		uid := user.ID
		s.runDetached("login.session_hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookAuthSessionIssued, map[string]any{"userId": uid, "account": account, "appId": appID}, plugindomain.HookMetadata{IP: ip, AppID: &appID, UserID: &uid})
		})
	}
}

// authenticatePassword 密码登录的凭据关卡：防爆破 → 风控 / 插件 / 应用策略 / 用户查询 → 账号状态 → 密码。
// 只认人、不签发任何会话；App 网关登录与 OAuth2 授权服务器的登录页共用这一份，
// 因此两条入口的锁定、风控与封禁判定永远一致。返回规范化之后的账号。
func (s *AuthService) authenticatePassword(ctx context.Context, appID int64, account, password, deviceID, device, ip, userAgent string) (*appdomain.App, *userdomain.User, string, error) {
	account = normalizeAccount(account)

	// 阶段 0 —— 防爆破快速失败：锁定期内直接拒绝，不触发任何下游查询，
	// 保证爆破攻击下 PG / 风控 / 插件零负载（Redis 故障 fail-open）
	if s.loginGuard != nil {
		if err := s.loginGuard.Check(ctx, appID, account, ip); err != nil {
			return nil, nil, "", err
		}
	}

	// 阶段 1 —— 并行前置检查：风控评估（等用户查询给出已有账号）、插件钩子、应用策略、用户查询，
	// 用 errgroup 并发执行，总耗时从「各 RTT 之和」降为「最慢一项」；
	// 任一分支失败即取消其余分支（gctx），错误按原串行顺序的优先级返回
	var (
		app     *appdomain.App
		user    *userdomain.User
		riskErr error
		hookErr error
		appErr  error
		userErr error
	)
	g, gctx := errgroup.WithContext(ctx)
	// 风控要知道登录名对应的是不是已有账号（「出现的账号」只数真实账号），所以等用户查询出结果再评估；
	// 插件与应用策略照旧并行。查询失败时整组已取消，评估跳过，错误按下面的优先级返回用户查询的那个
	resolved := make(chan struct{})
	var candidates []*userdomain.User
	if s.risk != nil {
		g.Go(func() error {
			select {
			case <-resolved:
			case <-gctx.Done():
				return nil
			}
			var accountUserID int64
			if len(candidates) > 0 {
				accountUserID = candidates[0].ID
			}
			// 风控内部错误放行（Guard 内部处理）；拦截 / 封禁 → 40398，需要验证码 → 40015
			if _, err := s.risk.Guard(gctx, securitydomain.RiskEvalRequest{
				Scene: securitydomain.SceneLogin, AppID: &appID, IP: ip, DeviceID: deviceID, UserAgent: userAgent, Account: account,
				AccountUserID: accountUserID,
			}, RiskGuardOption{Message: "当前请求被风控拦截，请稍后重试"}); err != nil {
				riskErr = err
				return riskErr
			}
			return nil
		})
	}
	if s.plugin != nil {
		g.Go(func() error {
			hookResult := s.plugin.ExecuteHook(gctx, HookAuthPreLogin, map[string]any{
				"account": account, "appId": appID, "ip": ip, "deviceId": deviceID,
			}, plugindomain.HookMetadata{IP: ip, UserAgent: userAgent, AppID: &appID})
			if !hookResult.Allow {
				msg := hookResult.Message
				if msg == "" {
					msg = "插件拒绝了此登录请求"
				}
				hookErr = apperrors.New(40399, http.StatusForbidden, msg)
				return hookErr
			}
			return nil
		})
	}
	if s.app != nil {
		g.Go(func() error {
			loaded, err := s.app.EnsureLoginAllowed(gctx, appID)
			if err == nil {
				err = s.validateLoginPolicy(loaded, deviceID, device)
			}
			if err != nil {
				appErr = err
				return err
			}
			app = loaded
			return nil
		})
	}
	g.Go(func() error {
		loaded, err := s.resolveLoginCandidates(gctx, appID, account)
		if err != nil {
			userErr = err
			return err
		}
		candidates = loaded
		close(resolved)
		return nil
	})
	_ = g.Wait()
	// 错误优先级与原串行实现一致：风控 > 插件 > 应用策略 > 用户查询，
	// 避免并发下因取消（context.Canceled）导致对外错误码漂移
	for _, err := range []error{riskErr, hookErr, appErr, userErr} {
		if err != nil {
			return nil, nil, "", err
		}
	}

	// 阶段 2 —— 凭据校验（bcrypt 为全链路最重 CPU 操作，置于所有快速检查之后）
	if len(candidates) > 1 {
		// 邮箱或手机号挂在多个账号上：密码对得上的那个就是要登录的人，对不上的都不透露存在与否
		for _, candidate := range candidates {
			if s.verifyCredential(candidate.PasswordHash, password) {
				user = candidate
				break
			}
		}
		if user == nil {
			if s.loginGuard != nil {
				s.loginGuard.RegisterFailure(ctx, appID, account, ip)
			}
			return nil, nil, "", apperrors.New(40101, http.StatusUnauthorized, "账号或密码错误")
		}
		if err := s.ensureUserLoginState(ctx, user); err != nil {
			return nil, nil, "", err
		}
		return app, user, account, nil
	}
	if len(candidates) == 1 {
		user = candidates[0]
	}
	if user == nil {
		// 时序均衡：账号不存在也执行一次等价 bcrypt 比较，并与密码错误统一计失败（防账号枚举）
		_ = bcrypt.CompareHashAndPassword(s.timingDummyHash, []byte(password))
		if s.loginGuard != nil {
			s.loginGuard.RegisterFailure(ctx, appID, account, ip)
		}
		return nil, nil, "", apperrors.New(40101, http.StatusUnauthorized, "账号或密码错误")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, nil, "", err
	}
	if !s.verifyCredential(user.PasswordHash, password) {
		// 失败计数同步执行（保证防爆破窗口准确），审计与插件钩子移出关键路径
		if s.loginGuard != nil {
			s.loginGuard.RegisterFailure(ctx, appID, account, ip)
		}
		userID := user.ID
		s.runDetached("login.failed_audit", 3*time.Second, func(actx context.Context) {
			_ = s.pg.InsertLoginAudit(actx, appID, userID, "password", "", "", ip, deviceID, userAgent, "failed", map[string]any{"reason": "invalid_password", "account": account})
		})
		if s.plugin != nil {
			s.runDetached("login.failed_hook", 5*time.Second, func(actx context.Context) {
				// 带上账号 ID：风控按「登录失败的已有账号」去重计数（IP / 网段多账号），不存在的登录名不会走到这里
				s.plugin.ExecuteHook(actx, HookAuthLoginFailed, map[string]any{"account": account, "appId": appID, "ip": ip, "userId": userID}, plugindomain.HookMetadata{IP: ip, AppID: &appID, UserID: &userID})
			})
		}
		return nil, nil, "", apperrors.New(40101, http.StatusUnauthorized, "账号或密码错误")
	}
	return app, user, account, nil
}

func (s *AuthService) RegisterWithPassword(ctx context.Context, input PasswordRegisterInput) (*authdomain.LoginResult, error) {
	input.Account = normalizeAccount(input.Account)

	// 阶段 0 —— 纯内存 / 单次 App 拉取的本地校验，失败时零额外 IO：
	// 账号格式 → 注册开关 → 密码策略 → 设备策略。
	// App 仅拉取一次（原实现 ValidatePasswordWithAppPolicy 内部会再 GetApp 一次，共两次）
	if err := validateAccount(input.Account); err != nil {
		return nil, err
	}
	var app *appdomain.App
	if s.app != nil {
		loaded, err := s.app.EnsureRegisterAllowed(ctx, input.AppID)
		if err != nil {
			return nil, err
		}
		// 复用已拉取的 app 做密码策略校验（与 ValidatePasswordWithAppPolicy 语义一致）。
		// 带上账号 / 昵称：注册时把密码设成自己的账号是最常见的弱口令形态，
		// 只有把这些串喂给强度引擎才认得出来。
		if check := CheckPasswordPolicyWithContext(input.Password,
			s.app.ResolvePasswordPolicy(loaded),
			registerPasswordContext(input, loaded),
		); !check.IsValid {
			return nil, apperrors.New(40007, http.StatusBadRequest, strings.Join(check.Violations, "; "))
		}
		// 注册后即进入登录态，设备检查策略同步适用（纯内存校验）
		if err := s.validateLoginPolicy(loaded, input.DeviceID, input.Device); err != nil {
			return nil, err
		}
		app = loaded
	} else if err := validatePasswordStrength(input.Password); err != nil {
		return nil, err
	}

	// 阶段 1 —— 并行外部检查：风控评估与注册 IP 准入（一个 Redis 脚本数完全部规则并占位）互相独立，
	// errgroup 并发执行；错误优先级固定为 风控 > 注册 IP 策略。风控拦了而 IP 准入已占位时撤销占位
	var riskErr, policyErr error
	var ticket *RegisterIPTicket
	g, gctx := errgroup.WithContext(ctx)
	if s.risk != nil {
		g.Go(func() error {
			if _, err := s.risk.Guard(gctx, securitydomain.RiskEvalRequest{
				Scene: securitydomain.SceneRegister, AppID: &input.AppID, IP: input.IP, DeviceID: input.DeviceID,
				UserAgent: input.UserAgent, Account: input.Account,
			}, RiskGuardOption{Message: "注册请求被风控拦截"}); err != nil {
				riskErr = err
				return riskErr
			}
			return nil
		})
	}
	if app != nil {
		g.Go(func() error {
			admitted, err := s.admitRegister(gctx, app, regip.MethodPassword, input.IP, input.Account, input.DeviceID, input.UserAgent)
			if err != nil {
				policyErr = err
				return err
			}
			ticket = admitted
			return nil
		})
	}
	_ = g.Wait()
	for _, err := range []error{riskErr, policyErr} {
		if err != nil {
			ticket.Release()
			return nil, err
		}
	}

	// 阶段 2 —— singleflight 合并同一 app+account 的并发注册，仅执行一次真实写入。
	// 只有真正执行写入的那个请求的票会被确认；被合并的请求没有建号，各自的占位在下面撤销
	result, err, _ := s.registerFlight.Do(registerFlightKey(input.AppID, input.Account), func() (any, error) {
		return s.registerWithPasswordOnce(ctx, app, input, ticket)
	})
	ticket.Release()
	if err != nil {
		return nil, err
	}
	loginResult, _ := result.(*authdomain.LoginResult)
	if loginResult == nil {
		return nil, fmt.Errorf("register result is nil")
	}
	return loginResult, nil
}

func (s *AuthService) registerWithPasswordOnce(ctx context.Context, app *appdomain.App, input PasswordRegisterInput, ticket *RegisterIPTicket) (*authdomain.LoginResult, error) {
	// 重复账号快速失败：在执行重 CPU 的 bcrypt 之前先做一次点查，
	// 重复注册（重试 / 撞库）场景下省去 ~100ms 哈希开销；
	// 并发竞争仍由事务内唯一约束兜底，不存在 TOCTOU 风险
	if existing, err := s.pg.GetUserByAppAndAccount(ctx, input.AppID, input.Account); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, apperrors.New(40901, http.StatusConflict, "账号已存在")
	}
	registerEmail, _ := input.Profile["email"].(string)
	if err := s.checkReregister(ctx, input.AppID,
		RegisterIdentifier{Kind: accountdeletion.TombstoneAccount, Value: input.Account},
		RegisterIdentifier{Kind: accountdeletion.TombstoneEmail, Value: registerEmail},
	); err != nil {
		return nil, err
	}

	// 与上面策略校验用同一份上下文，否则落库的强度分会高于实际判定所用的分
	passwordAnalysis := AnalyzePasswordStrengthWithContext(input.Password, registerPasswordContext(input, app))
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return nil, err
	}
	now := timeutil.NowUTC()
	profile := userdomain.Profile{
		Nickname: strings.TrimSpace(input.Nickname),
		Extra: map[string]any{
			"register_ip":         input.IP,
			"register_user_agent": input.UserAgent,
		},
	}
	for key, value := range input.Profile {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(strings.ToLower(key), "register_") {
			continue
		}
		profile.Extra[key] = value
	}
	security := userdomain.ProfileSecurityState{
		PasswordChangedAt:      timePtr(now),
		PasswordStrengthScore:  intPtr(passwordAnalysis.Score),
		PasswordChangeRequired: boolPtr(false),
	}
	// 注册即按应用策略写入密码过期时间，否则「密码有效期 90 天」只对改过密码的老用户生效
	if s.app != nil {
		security.PasswordExpiresAt = s.app.ResolvePasswordLifecycle(ctx, input.AppID, now).ExpiresAt
	}

	// 用户 + 档案（含邀请码）+ 密码安全状态在单个事务中原子落库，
	// 消除原实现「三段独立写入、中途失败产生无档案孤儿用户」的问题。
	// 邀请码为 24 位随机（32 字符集），撞码概率趋近于零：直接依赖唯一约束、
	// 撞码时换码整体重试，省去原实现每次注册的 HasInviteCode 预查询
	var user *userdomain.User
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		inviteCode, codeErr := randomInviteCode(inviteCodeLength)
		if codeErr != nil {
			return nil, codeErr
		}
		profile.InviteCode = inviteCode
		created, createErr := s.pg.CreateUserWithProfile(ctx, input.AppID, input.Account, passwordHash, profile, security)
		if createErr == nil {
			user = created
			break
		}
		if createErr == pgrepo.ErrAccountAlreadyExists {
			return nil, apperrors.New(40901, http.StatusConflict, "账号已存在")
		}
		if pgrepo.IsUniqueViolation(createErr) {
			continue // 邀请码等唯一键撞码：换码后整体重试
		}
		return nil, createErr
	}
	if user == nil {
		return nil, fmt.Errorf("create user with invite code exceeded max retries")
	}
	ticket.Commit(user.ID, user.Account)

	// 注册审计与注册插件钩子移出关键路径（与登录链路一致的 runDetached 语义）
	userID := user.ID
	s.runDetached("register.audit", 3*time.Second, func(actx context.Context) {
		_ = s.pg.InsertLoginAudit(actx, input.AppID, userID, "register", "password", "", input.IP, input.DeviceID, input.UserAgent, "success", map[string]any{
			"account": input.Account,
			"device":  input.Device,
		})
	})
	// 首次设备保持同步写入：确保 register 场景先于 completeLogin 的异步 login 场景落键（SetNX 幂等）
	s.recordFirstDevice(ctx, authdomain.FirstDevice{
		UserID:      user.ID,
		AppID:       input.AppID,
		DeviceID:    input.DeviceID,
		Device:      input.Device,
		IP:          input.IP,
		UserAgent:   input.UserAgent,
		Provider:    "password",
		Scene:       "register",
		FirstSeenAt: now,
	})
	if s.plugin != nil {
		s.runDetached("register.hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookUserRegistered, map[string]any{"userId": userID, "account": input.Account, "appId": input.AppID}, plugindomain.HookMetadata{IP: input.IP, AppID: &input.AppID, UserID: &userID})
		})
	}
	s.syncAdminUserSearch(input.AppID, user.ID)
	s.sendWelcomeMail(input.AppID, input.Account, registerEmail, input.Nickname)
	if input.SuppressSession {
		return &authdomain.LoginResult{UserID: user.ID, PublicUserID: user.PublicID, Account: user.Account}, nil
	}
	// 新建用户必然未被封禁/禁用，无需 ensureUserLoginState；首次设备已以 register 场景同步写入
	return s.completeLogin(ctx, app, user, "password", "password", input.DeviceID, input.Device, input.IP, input.UserAgent)
}

// sendWelcomeMail 注册成功后发欢迎信。是否发由应用邮件策略的「欢迎信」场景决定（默认关），
// 收件地址取注册时填的邮箱，没填而账号名本身是邮箱时用账号名。移出关键路径：
// 发信慢或失败都不该拖住注册。
func (s *AuthService) sendWelcomeMail(appID int64, account string, email string, nickname string) {
	// 找回密码的发信接口里没有欢迎信，按能力断言：测试里注入的替身不必实现它
	mailer, ok := s.recoveryEmail.(interface {
		SendWelcomeEmail(ctx context.Context, appID int64, email string, userName string, configName string) error
	})
	if !ok || mailer == nil {
		return
	}
	address := strings.TrimSpace(email)
	if address == "" && strings.Contains(account, "@") {
		address = strings.TrimSpace(account)
	}
	if _, err := mail.ParseAddress(address); address == "" || err != nil {
		return
	}
	nickname = strings.TrimSpace(nickname)
	s.runDetached("register.welcome", 15*time.Second, func(actx context.Context) {
		if err := mailer.SendWelcomeEmail(actx, appID, address, nickname, ""); err != nil {
			s.log.Debug("send welcome email skipped", zap.Int64("appid", appID), zap.Error(err))
		}
	})
}

// recordFirstDevice 在 Redis 中写入首次设备（已存在则不覆盖）
// 通过 SetNX 保证幂等，后台执行即使失败也不影响主流程
func (s *AuthService) recordFirstDevice(ctx context.Context, record authdomain.FirstDevice) {
	if s.sessions == nil || record.UserID <= 0 || record.AppID <= 0 {
		return
	}
	if record.FirstSeenAt.IsZero() {
		record.FirstSeenAt = timeutil.NowUTC()
	}
	if _, err := s.sessions.EnsureFirstDevice(ctx, record); err != nil {
		s.log.Warn("record first device failed",
			zap.Int64("userId", record.UserID),
			zap.Int64("appId", record.AppID),
			zap.Error(err))
	}
}

// OAuth 授权链路的 state 用途标记
const (
	oauthPurposeLogin = "login"
	oauthPurposeBind  = "bind"
)

// OAuthCallbackOutcome 回调链路的统一出参。
//
// 同一个回调地址同时承载「登录/注册」与「已登录用户绑定」两种用途，
// 由 state 中的 purpose 区分；Handler 按 Purpose 决定响应体，
// 登录场景的响应结构与历史版本保持一致。
type OAuthCallbackOutcome struct {
	Purpose  string                  `json:"purpose"`
	Provider string                  `json:"provider"`
	Login    *authdomain.LoginResult `json:"login,omitempty"`
	Binding  *oauthdomain.Binding    `json:"binding,omitempty"`
}

// resolveOAuthProvider 解析某个 App 下的渠道配置并构造协议适配器。
//
// 注入 AppOAuthService 时走「应用级 → 平台级」两段解析；
// 未注入时（OpenAPI 导出、单元测试）回落到进程内平台级配置，行为与历史版本一致。
func (s *AuthService) resolveOAuthProvider(ctx context.Context, appID int64, provider string) (*OAuthProvider, *oauthdomain.Resolved, error) {
	slug := strings.TrimSpace(strings.ToLower(provider))
	if slug == "" {
		return nil, nil, apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	if s.appOAuth != nil {
		resolved, err := s.appOAuth.Resolve(ctx, appID, slug)
		if err != nil {
			return nil, nil, err
		}
		return NewOAuthProvider(ProviderConfig(resolved)), resolved, nil
	}
	p, ok := s.providers[slug]
	if !ok {
		return nil, nil, apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	return p, &oauthdomain.Resolved{
		Provider: slug, Kind: p.cfg.Kind, DisplayName: slug,
		Source:     oauthdomain.SourcePlatform,
		AllowLogin: true, AllowRegister: true, AllowBind: true,
	}, nil
}

func (s *AuthService) BuildOAuthAuthURL(ctx context.Context, provider string, appID int64, deviceID string) (string, error) {
	p, resolved, err := s.resolveOAuthProvider(ctx, appID, provider)
	if err != nil {
		return "", err
	}
	if !resolved.AllowLogin {
		return "", apperrors.New(40391, http.StatusForbidden, "该渠道仅开放账号绑定，未开放直接登录")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return "", err
		}
		// OAuth Web 启动阶段 device 名称无法通过 302 链路携带，仅强制 deviceID
		if err := s.validateLoginDeviceID(app, deviceID); err != nil {
			return "", err
		}
	}
	if p.cfg.ClientID == "" || p.cfg.RedirectURL == "" {
		return "", apperrors.New(40010, http.StatusBadRequest, "OAuth2 提供商未完成配置")
	}
	return s.issueOAuthState(ctx, p, resolved.Provider, appID, 0, deviceID, oauthPurposeLogin)
}

// BuildOAuthBindURL 已登录用户发起的第三方账号绑定授权。
// state 中额外携带 user_id，回调阶段据此把第三方身份挂到当前账号上。
func (s *AuthService) BuildOAuthBindURL(ctx context.Context, appID, userID int64, provider, deviceID string) (string, error) {
	if userID <= 0 {
		return "", apperrors.New(40100, http.StatusUnauthorized, "请先登录")
	}
	p, resolved, err := s.resolveOAuthProvider(ctx, appID, provider)
	if err != nil {
		return "", err
	}
	if !resolved.AllowBind {
		return "", apperrors.New(40392, http.StatusForbidden, "该渠道未开放账号绑定")
	}
	if p.cfg.ClientID == "" || p.cfg.RedirectURL == "" {
		return "", apperrors.New(40010, http.StatusBadRequest, "OAuth2 提供商未完成配置")
	}
	return s.issueOAuthState(ctx, p, resolved.Provider, appID, userID, deviceID, oauthPurposeBind)
}

func (s *AuthService) issueOAuthState(ctx context.Context, p *OAuthProvider, provider string, appID, userID int64, deviceID, purpose string) (string, error) {
	state := uuid.NewString()
	payload := map[string]string{
		"provider":  provider,
		"appid":     fmt.Sprintf("%d", appID),
		"device_id": deviceID,
		"purpose":   purpose,
	}
	if userID > 0 {
		payload["user_id"] = fmt.Sprintf("%d", userID)
	}
	if err := s.sessions.SetOAuthState(ctx, state, payload, 5*time.Minute); err != nil {
		return "", err
	}
	return p.AuthURL(state), nil
}

func (s *AuthService) HandleOAuthCallback(ctx context.Context, provider, code, state, ip, userAgent string) (*OAuthCallbackOutcome, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" || strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return nil, apperrors.New(40013, http.StatusBadRequest, "OAuth2 回调参数不完整")
	}
	payload, err := s.sessions.ConsumeOAuthState(ctx, state)
	if err != nil {
		return nil, err
	}
	if payload == nil || payload["provider"] != provider {
		return nil, apperrors.New(40002, http.StatusBadRequest, "OAuth2 状态无效或已过期")
	}
	appID, err := parseInt64(payload["appid"])
	if err != nil {
		return nil, apperrors.New(40003, http.StatusBadRequest, "无效的应用标识")
	}
	p, resolved, err := s.resolveOAuthProvider(ctx, appID, provider)
	if err != nil {
		return nil, err
	}
	deviceID := payload["device_id"]
	purpose := payload["purpose"]
	if purpose == "" {
		purpose = oauthPurposeLogin
	}

	if purpose == oauthPurposeBind {
		userID, err := parseInt64(payload["user_id"])
		if err != nil || userID <= 0 {
			return nil, apperrors.New(40002, http.StatusBadRequest, "OAuth2 状态无效或已过期")
		}
		if !resolved.AllowBind {
			return nil, apperrors.New(40392, http.StatusForbidden, "该渠道未开放账号绑定")
		}
		profile, err := p.ExchangeCode(ctx, s.http, code)
		if err != nil {
			return nil, err
		}
		profile.Provider = resolved.Provider
		binding, err := s.bindOAuthProfile(ctx, appID, userID, profile)
		if err != nil {
			return nil, err
		}
		return &OAuthCallbackOutcome{Purpose: oauthPurposeBind, Provider: resolved.Provider, Binding: binding}, nil
	}

	if !resolved.AllowLogin {
		return nil, apperrors.New(40391, http.StatusForbidden, "该渠道仅开放账号绑定，未开放直接登录")
	}
	var app *appdomain.App
	if s.app != nil {
		app, err = s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return nil, err
		}
		// OAuth 回调阶段从 state 中恢复 device_id（浏览器 302 无法携带 device 名称），仅强制 deviceID
		if err := s.validateLoginDeviceID(app, deviceID); err != nil {
			return nil, err
		}
	}
	profile, err := p.ExchangeCode(ctx, s.http, code)
	if err != nil {
		return nil, err
	}
	profile.Provider = resolved.Provider
	login, err := s.loginWithOAuthProfile(ctx, app, appID, profile, deviceID, "", ip, userAgent, "oauth_callback", resolved.AllowRegister)
	if err != nil {
		return nil, err
	}
	return &OAuthCallbackOutcome{Purpose: oauthPurposeLogin, Provider: resolved.Provider, Login: login}, nil
}

// bindOAuthProfile 把第三方身份挂到已登录账号上。
//
// 冲突处理：同一 App 内一个第三方账号只能绑定一个用户 ——
// 直接 Upsert 会因 ON CONFLICT 把绑定"抢"到当前用户名下，因此必须先查重。
func (s *AuthService) bindOAuthProfile(ctx context.Context, appID, userID int64, profile authdomain.ProviderProfile) (*oauthdomain.Binding, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40410, http.StatusNotFound, "账号不存在")
	}
	if err := s.riskCheck(ctx, securitydomain.RiskEvalRequest{Scene: securitydomain.SceneOAuthBind, AppID: &appID, UserID: &userID,
		Account: user.Account, Extra: map[string]any{"provider": profile.Provider}}, "当前环境存在风险，暂时无法绑定第三方账号"); err != nil {
		return nil, err
	}
	boundUserID, err := s.pg.FindOAuthBinding(ctx, appID, profile.Provider, profile.ProviderUserID)
	if err != nil {
		return nil, err
	}
	if boundUserID > 0 && boundUserID != userID {
		return nil, apperrors.New(errCodeOAuthIdentityTaken, http.StatusConflict, "该第三方账号已绑定其他账号，请先解绑")
	}
	// 同一渠道一个账号只挂一个第三方身份：表的唯一键管不到这一侧，
	// 不拦的话换个 QQ 再绑一次，账号下就有两个 QQ，解绑、展示都对不上
	existing, err := s.pg.FindUserOAuthBinding(ctx, appID, userID, profile.Provider)
	if err != nil {
		return nil, err
	}
	if existing != "" && existing != profile.ProviderUserID {
		return nil, apperrors.New(errCodeOAuthProviderBound, http.StatusConflict, "当前账号已绑定该渠道的其他账号，请先解绑")
	}
	if err := s.pg.UpsertOAuthBinding(ctx, appID, userID, profile); err != nil {
		return nil, err
	}
	s.riskEmit(ctx, riskevents.Event{Type: riskevents.TypeOAuthBind, Scene: securitydomain.SceneOAuthBind, AppID: appID, UserID: userID,
		Account: user.Account, Props: identityProps(profile)})
	return &oauthdomain.Binding{
		AppID: appID, UserID: userID, Account: user.Account,
		Provider: profile.Provider, ProviderUserID: profile.ProviderUserID,
		UnionID: profile.UnionID, Nickname: profile.Nickname,
		Avatar: profile.Avatar, Email: profile.Email,
		CreatedAt: timeutil.NowUTC(), UpdatedAt: timeutil.NowUTC(),
	}, nil
}

func (s *AuthService) Refresh(ctx context.Context, token, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	refreshSession, err := s.validateRefreshToken(ctx, token)
	if err != nil {
		return nil, err
	}
	// 刷新是登录之外的第二条"换新会话"通道，治理判定必须同样覆盖，
	// 否则冻结期间客户端靠刷新令牌就能一直续命。
	if s.governance != nil {
		if err := s.governance.EnsureCapability(refreshSession.AppID, platformdomain.CapabilityAPI); err != nil {
			return nil, err
		}
	}
	if deviceID == "" {
		deviceID = devicedomain.NormalizeID(refreshSession.DeviceID)
	}
	// 设备绑定：会话里存的设备码要与这次上报的一致。两种情况不算换设备 ——
	// 存的是占位值（全零 OAID 等，修复前的客户端会发），或者存的正是客户端声明的旧设备码
	// （客户端换了设备码生成方式，见 devicedomain.HeaderPreviousID）。后者顺带迁移旧码下的绑定。
	if !devicedomain.Continues(ctx, refreshSession.DeviceID, deviceID) {
		s.handleRefreshReuse(ctx, refreshSession)
		return nil, apperrors.New(40104, http.StatusUnauthorized, "刷新令牌设备绑定校验失败")
	}
	migrateFrom := ""
	if stored := devicedomain.NormalizeID(refreshSession.DeviceID); stored != "" && stored != deviceID {
		migrateFrom = stored
	}
	if refreshSession.UsedAt != nil || strings.TrimSpace(refreshSession.ReplacedByToken) != "" {
		s.handleRefreshReuse(ctx, refreshSession)
		return nil, apperrors.New(40104, http.StatusUnauthorized, "刷新令牌已失效")
	}
	user, err := s.pg.GetUserByID(ctx, refreshSession.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	// 刷新是延续已有会话，按会话档判定：仅登录的封禁不打断已登录的设备
	if err := s.ensureUserSessionState(ctx, user); err != nil {
		return nil, err
	}
	// App 配置仅拉取一次，供签发与会话上限策略共用（原实现签发+策略各拉一次）
	var app *appdomain.App
	if s.app != nil {
		if app, err = s.app.GetApp(ctx, refreshSession.AppID); err != nil {
			return nil, err
		}
	}
	if err := s.revokeAccessSessionsByFamily(ctx, refreshSession.AppID, refreshSession.UserID, refreshSession.FamilyID, ""); err != nil {
		return nil, err
	}
	// 刷新请求自己没带设备信息时沿用旧会话的原始型号与平台
	if current := devicedomain.ClientFrom(ctx); current.Model == "" && current.Platform == "" && current.Extra.IsZero() {
		ctx = devicedomain.WithClient(ctx, devicedomain.Client{
			Model: refreshSession.DeviceModel, Platform: refreshSession.DevicePlatform, Extra: refreshSession.DeviceExtra.Value(),
		})
	}
	bundle, err := s.issueSessionBundle(ctx, app, user, refreshSession.Provider, "refresh", deviceID, refreshSession.Device, ip, userAgent, refreshSession.FamilyID)
	if err != nil {
		return nil, err
	}
	if migrateFrom != "" {
		s.migrateDeviceID(refreshSession.AppID, refreshSession.UserID, migrateFrom, deviceID)
	}
	if s.deviceLimit != nil {
		appID, userID, familyID := refreshSession.AppID, refreshSession.UserID, refreshSession.FamilyID
		client := devicedomain.ClientFrom(ctx)
		s.runDetached("refresh.device_ledger", 3*time.Second, func(actx context.Context) {
			s.deviceLimit.TouchRefresh(actx, appID, userID, deviceID, userAgent, familyID, ip, client)
		})
	}
	now := timeutil.NowUTC()
	refreshSession.UsedAt = &now
	refreshSession.RotatedAt = &now
	refreshSession.ReplacedByToken = bundle.RefreshSession.TokenID
	if err := s.sessions.UpdateRefreshSession(ctx, token, *refreshSession, time.Until(refreshSession.ExpiresAt)); err != nil {
		s.log.Warn("mark refresh token rotated failed", zap.Error(err))
	}
	return bundle.Result, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	session, err := s.ValidateAccessToken(ctx, token)
	if err != nil {
		return err
	}
	_ = s.sessions.DeleteSession(ctx, token)
	if session.RefreshFamilyID != "" {
		_ = s.sessions.RevokeRefreshFamily(ctx, session.AppID, session.UserID, session.RefreshFamilyID, s.cfg.JWT.RefreshTTL)
		_ = s.revokeRefreshFamilySessions(ctx, session.AppID, session.UserID, session.RefreshFamilyID)
	}
	return s.sessions.BlacklistToken(ctx, session.TokenID, time.Until(session.ExpiresAt))
}

func (s *AuthService) VerifyCurrentPassword(ctx context.Context, session *authdomain.Session, password string) error {
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.AppID != session.AppID {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if !verifyPassword(user.PasswordHash, password) {
		return apperrors.New(40106, http.StatusUnauthorized, "当前密码错误")
	}
	return nil
}

func (s *AuthService) ChangePassword(ctx context.Context, session *authdomain.Session, currentPassword, newPassword string) error {
	// 先取用户再校验策略（原顺序相反）：强度评估需要账号 / 昵称当上下文，
	// 才能识别「把密码改成自己的账号」。会话有效即意味着用户存在，
	// 这次前置读取不会白跑。
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.AppID != session.AppID {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if err := s.riskCheck(ctx, securitydomain.RiskEvalRequest{Scene: securitydomain.ScenePasswordChange, AppID: &session.AppID, UserID: &session.UserID,
		Account: user.Account}, "当前环境存在风险，暂时无法修改密码"); err != nil {
		return err
	}
	pctx := s.userPasswordContext(ctx, user)
	if err := s.validatePasswordPolicyWithContext(ctx, session.AppID, newPassword, pctx); err != nil {
		return err
	}
	if user.PasswordHash != "" && !verifyPassword(user.PasswordHash, currentPassword) {
		return apperrors.New(40106, http.StatusUnauthorized, "当前密码错误")
	}
	if user.PasswordHash != "" && verifyPassword(user.PasswordHash, newPassword) {
		return apperrors.New(40006, http.StatusBadRequest, "新密码不能与当前密码相同")
	}
	// 策略 preventReuse：命中最近 N 个历史密码即拒绝
	if s.app != nil {
		if err := s.app.EnsurePasswordNotReused(ctx, session.AppID, user.ID, newPassword); err != nil {
			return err
		}
	}

	passwordHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	changedAt := timeutil.NowUTC()
	var lifecycle PasswordLifecycle
	if s.app != nil {
		lifecycle = s.app.ResolvePasswordLifecycle(ctx, session.AppID, changedAt)
	}
	if err := s.pg.UpdateUserPassword(ctx, user.ID, passwordHash, changedAt, lifecycle.ExpiresAt); err != nil {
		return err
	}
	// 先落新密码再记历史：历史写失败只会少一条判重依据，
	// 反过来（先记历史后落密码）失败时会把没生效的密码算进历史。
	if err := s.pg.AppendPasswordHistory(ctx, user.ID, user.PasswordHash, lifecycle.HistoryKeep); err != nil {
		s.log.Warn("密码历史写入失败", zap.Int64("user_id", user.ID), zap.Error(err))
	}
	passwordAnalysis := AnalyzePasswordStrengthWithContext(newPassword, pctx)
	if err := s.pg.PatchUserSecurityState(ctx, user.ID, map[string]any{
		"password_changed_at":      changedAt,
		"password_strength_score":  passwordAnalysis.Score,
		"password_change_required": false,
	}); err != nil {
		return err
	}
	_ = s.sessions.DeleteSecurityStatus(ctx, session.AppID, session.UserID)
	_ = s.sessions.DeleteUserProfile(ctx, session.AppID, session.UserID)
	_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
		"user_id":    session.UserID,
		"appid":      session.AppID,
		"token_jti":  session.TokenID,
		"event_type": "password_changed",
		"changed_at": changedAt.Format(time.RFC3339),
	})
	s.riskEmit(ctx, riskevents.Event{Type: riskevents.TypePasswordChange, Scene: securitydomain.ScenePasswordChange, AppID: session.AppID,
		UserID: session.UserID, Account: user.Account})
	return nil
}

func (s *AuthService) ValidateAccessToken(ctx context.Context, token string) (*authdomain.Session, error) {
	claims, err := s.parseTokenClaims(token, sessiontoken.TypeAccess)
	if err != nil {
		return nil, err
	}
	tokenID := claims.TokenID
	reason, blacklisted, err := s.sessions.TokenBlacklistReason(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if blacklisted {
		return nil, revokedTokenError(reason, "Token 已失效")
	}
	session, err := s.sessions.GetSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperrors.New(40105, http.StatusUnauthorized, "会话不存在或已过期")
	}
	// 平台治理：应用被冻结 / 封禁后，已签发的会话必须当场失效。
	// 只在登录入口拦截是不够的 —— 已经登录的人可以拿着令牌一直用到过期。
	if s.governance != nil {
		if err := s.governance.EnsureCapability(session.AppID, platformdomain.CapabilityAPI); err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (s *AuthService) validateRefreshToken(ctx context.Context, token string) (*authdomain.RefreshSession, error) {
	claims, err := s.parseTokenClaims(token, sessiontoken.TypeRefresh)
	if err != nil {
		return nil, err
	}
	tokenID := claims.TokenID
	reason, blacklisted, err := s.sessions.TokenBlacklistReason(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if blacklisted {
		return nil, revokedTokenError(reason, "刷新令牌已失效")
	}
	session, err := s.sessions.GetRefreshSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperrors.New(40105, http.StatusUnauthorized, "刷新会话不存在或已过期")
	}
	if session.TokenID != tokenID {
		return nil, apperrors.New(40102, http.StatusUnauthorized, "刷新令牌无效")
	}
	reason, revoked, err := s.sessions.RefreshFamilyRevokedReason(ctx, session.AppID, session.UserID, session.FamilyID)
	if err != nil {
		return nil, err
	}
	if revoked {
		return nil, revokedTokenError(reason, "刷新令牌族已失效")
	}
	return session, nil
}

// revokedTokenError 按失效原因给出错误：被设备数控制挤下线、被设备管理下线的各有专门的码，
// 客户端据此提示「账号已在其他设备登录」而不是笼统的「登录已过期」。
func revokedTokenError(reason, fallback string) error {
	switch reason {
	case redisrepo.RevokeReasonDeviceLimit:
		return apperrors.New(errCodeDeviceLimitKicked, http.StatusUnauthorized, "账号已在其他设备登录，当前设备已退出登录")
	case redisrepo.RevokeReasonRemoved:
		return apperrors.New(errCodeDeviceRemoved, http.StatusUnauthorized, "当前设备已在设备管理中被移除，请重新登录")
	}
	return apperrors.New(40104, http.StatusUnauthorized, fallback)
}

// parseTokenClaims 校验令牌（PASETO v4.local；过渡期内也接受旧 HS256 JWT）并要求种类一致。
func (s *AuthService) parseTokenClaims(token string, expectedType string) (*sessiontoken.Claims, error) {
	codec, err := s.tokenCodec()
	if err != nil {
		return nil, apperrors.New(50000, http.StatusInternalServerError, "登录令牌密钥未配置")
	}
	claims, err := codec.Parse(token, expectedType)
	if err != nil {
		return nil, apperrors.New(40102, http.StatusUnauthorized, "Token 无效")
	}
	return claims, nil
}

// loginWithOAuthProfile 第三方身份 → 会话签发的统一收口。
// allowRegister=false 时，未绑定过的第三方身份不会自动开户，而是明确提示先绑定。
func (s *AuthService) loginWithOAuthProfile(ctx context.Context, app *appdomain.App, appID int64, profile authdomain.ProviderProfile, deviceID, device, ip, userAgent, loginType string, allowRegister bool) (*authdomain.LoginResult, error) {
	userID, err := s.pg.FindOAuthBinding(ctx, appID, profile.Provider, profile.ProviderUserID)
	if err != nil {
		return nil, err
	}
	var user *userdomain.User
	if userID > 0 {
		user, err = s.pg.GetUserByID(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	// 风控：第三方账号登录（含首次登录建号）。已绑定时带上那个用户，评估能看到账号自己的历史
	riskReq := securitydomain.RiskEvalRequest{Scene: securitydomain.SceneOAuthLogin, AppID: &appID, Account: profile.Nickname,
		IP: ip, DeviceID: deviceID, UserAgent: userAgent, Extra: map[string]any{"provider": profile.Provider, "newUser": user == nil}}
	if user != nil {
		uid := user.ID
		riskReq.UserID, riskReq.Account = &uid, user.Account
	}
	if err := s.riskCheck(ctx, riskReq, "当前环境存在风险，暂时无法使用第三方账号登录"); err != nil {
		return nil, err
	}
	if user == nil {
		if !allowRegister {
			return nil, apperrors.New(40393, http.StatusForbidden,
				"该第三方账号尚未绑定，且当前渠道未开放自动注册，请先用已有账号登录后绑定")
		}
		user, err = s.createOAuthUser(ctx, appID, profile, deviceID, device, ip, userAgent)
		if err != nil {
			return nil, err
		}
	} else if err := s.fillOAuthProfileBlanks(ctx, user.ID, profile); err != nil {
		return nil, err
	}
	if err := s.pg.UpsertOAuthBinding(ctx, appID, user.ID, profile); err != nil {
		return nil, err
	}
	s.riskEmit(ctx, riskevents.Event{Type: riskevents.TypeOAuthLogin, Scene: securitydomain.SceneOAuthLogin, AppID: appID, UserID: user.ID,
		Account: user.Account, IP: ip, DeviceID: deviceID, UA: userAgent, Props: identityProps(profile)})
	// 封禁/禁用校验在此执行一次，completeLogin 不再重复（原实现会触发两次 RefreshUserAccountBanState）
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	s.syncAdminUserSearch(appID, user.ID)
	return s.completeLogin(ctx, app, user, profile.Provider, loginType, deviceID, device, ip, userAgent)
}

func (s *AuthService) syncAdminUserSearch(appID int64, userID int64) {
	if s.search == nil || appID <= 0 || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.search.IndexUser(ctx, appID, userID); err != nil {
			s.log.Warn("sync admin user search failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

// completeLogin 凭据校验通过后的统一收口：MFA 拦截 → 最终签发。
// 契约：调用方必须已完成 ensureUserLoginState 校验（避免链路内对封禁状态的重复刷新）；
// app 允许为 nil（issueSessionBundle 会兜底拉取一次）。
func (s *AuthService) completeLogin(ctx context.Context, app *appdomain.App, user *userdomain.User, provider, loginType, deviceID, device, ip, userAgent string) (*authdomain.LoginResult, error) {
	if s.security != nil {
		challenge, err := s.security.MaybeCreateSecondFactorChallenge(ctx, user, provider, loginType, deviceID, ip, userAgent)
		if err != nil {
			return nil, err
		}
		if challenge != nil {
			if s.plugin != nil {
				uid := user.ID
				account := user.Account
				s.runDetached("login.mfa_hook", 5*time.Second, func(actx context.Context) {
					s.plugin.ExecuteHook(actx, HookAuthMFACreated, map[string]any{"userId": uid, "account": account}, plugindomain.HookMetadata{IP: ip, UserID: &uid})
				})
			}
			return challenge, nil
		}
	}
	return s.finalizeLogin(ctx, app, user, provider, loginType, deviceID, device, ip, userAgent)
}

// finalizeLogin 已通过全部认证关卡（含 MFA / Passkey）后的最终签发：
// 登录一致性校验 → 异步记录首次设备（SetNX 幂等、失败不影响主流程）→ 签发双令牌会话。
//
// 一致性校验放在这里而不是 completeLogin：Passkey 链路（VerifyPasskeyLogin）
// 与 MFA 二次验证链路（VerifySecondFactor）都绕过 completeLogin 直接进本函数，
// 只有 finalizeLogin 是全部登录方式的公共收口。
func (s *AuthService) finalizeLogin(ctx context.Context, app *appdomain.App, user *userdomain.User, provider, loginType, deviceID, device, ip, userAgent string) (*authdomain.LoginResult, error) {
	if err := s.enforceLoginConsistency(ctx, app, user, deviceID, ip); err != nil {
		return nil, err
	}
	// 记下用户语言：之后的封禁通知、申诉结果按它发
	rememberLocale(ctx, s.pg, user.ID)
	record := authdomain.FirstDevice{
		UserID:      user.ID,
		AppID:       user.AppID,
		DeviceID:    deviceID,
		Device:      device,
		IP:          ip,
		UserAgent:   userAgent,
		Provider:    provider,
		Scene:       "login",
		FirstSeenAt: timeutil.NowUTC(),
	}
	s.runDetached("login.first_device", 3*time.Second, func(actx context.Context) {
		s.recordFirstDevice(actx, record)
	})
	result, err := s.issueSession(ctx, app, user, provider, loginType, deviceID, device, ip, userAgent)
	if err == nil && result != nil && result.DeviceLimit == nil && s.deletion != nil {
		result.AccountDeletion = s.deletion.OnLogin(ctx, user)
	}
	// 全部登录方式（密码、短信、第三方、扫码、卡密、通行密钥、二次验证之后）都在这里收口；
	// 设备数超限待选择时还没有登录成功，不算
	if err == nil && result != nil && result.DeviceLimit == nil && s.loginObserver != nil {
		s.loginObserver(user.AppID, user.ID, provider)
	}
	return result, err
}

func (s *AuthService) VerifySecondFactor(ctx context.Context, challengeID string, code string, recoveryCode string) (*authdomain.LoginResult, error) {
	if s.security == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "双因子认证模块未启用")
	}
	user, challenge, err := s.security.VerifySecondFactorChallenge(ctx, challengeID, code, recoveryCode)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	// 经 finalizeLogin 收口：MFA 登录同样记录首次设备（修复此前 MFA 路径漏记的问题）
	result, err := s.finalizeLogin(ctx, nil, user, challenge.Provider, challenge.LoginType, challenge.DeviceID, "", challenge.IP, challenge.UserAgent)
	if err == nil && s.plugin != nil {
		uid := user.ID
		account := user.Account
		s.runDetached("login.mfa_verified_hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookAuthMFAVerified, map[string]any{"userId": uid, "account": account}, plugindomain.HookMetadata{IP: challenge.IP, UserID: &uid})
		})
	}
	return result, err
}

func (s *AuthService) BeginPasskeyLogin(ctx context.Context, appID int64, deviceID string) (*securitydomain.PasskeyLoginSession, *protocol.CredentialAssertion, error) {
	if s.security == nil {
		return nil, nil, apperrors.New(50321, http.StatusServiceUnavailable, "Passkey 模块未启用")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return nil, nil, err
		}
		// Passkey 流程在浏览器侧异步，device 名称由 verify 阶段重新提交；这里仅校验 deviceID
		if err := s.validateLoginDeviceID(app, deviceID); err != nil {
			return nil, nil, err
		}
	}
	return s.security.BeginPasskeyLogin(ctx, appID)
}

func (s *AuthService) VerifyPasskeyLogin(ctx context.Context, appID int64, challengeID string, payload []byte, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	if s.security == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "Passkey 模块未启用")
	}
	// 先做廉价的应用状态校验（命中缓存），再执行 WebAuthn 签名验证；app 同时供签发复用
	var app *appdomain.App
	if s.app != nil {
		var err error
		if app, err = s.app.EnsureLoginAllowed(ctx, appID); err != nil {
			return nil, err
		}
	}
	user, err := s.security.VerifyPasskeyLogin(ctx, appID, challengeID, payload)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	uid := user.ID
	if err := s.riskCheck(ctx, securitydomain.RiskEvalRequest{Scene: securitydomain.ScenePasskeyLogin, AppID: &appID, UserID: &uid,
		Account: user.Account, IP: ip, DeviceID: deviceID, UserAgent: userAgent}, "当前环境存在风险，暂时无法使用通行密钥登录"); err != nil {
		return nil, err
	}
	// Passkey 属强认证，按既有设计不再叠加二次验证，直接 finalize（并补记首次设备）
	return s.finalizeLogin(ctx, app, user, "passkey", "passkey", deviceID, "", ip, userAgent)
}

// ensureUserLoginState 换取新会话前的账号状态关卡：任何生效中的封禁都拦。
func (s *AuthService) ensureUserLoginState(ctx context.Context, user *userdomain.User) error {
	if user == nil {
		return apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	return enforceAccountBan(ctx, s.pg, s.log, user, userdomain.AccountBanCheckpointSignIn)
}

// ensureUserSessionState 延续已有会话（刷新令牌）时的账号状态关卡：只有全部访问的封禁拦。
func (s *AuthService) ensureUserSessionState(ctx context.Context, user *userdomain.User) error {
	if user == nil {
		return apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	return enforceAccountBan(ctx, s.pg, s.log, user, userdomain.AccountBanCheckpointSession)
}

// issueSession 新登录的签发：先过设备数关卡，再签发，最后执行关卡给出的处置。
// 全部登录方式都经 finalizeLogin 走到这里；刷新令牌延续已有设备，直接走 issueSessionBundle。
func (s *AuthService) issueSession(ctx context.Context, app *appdomain.App, user *userdomain.User, provider, loginType, deviceID, device, ip, userAgent string) (*authdomain.LoginResult, error) {
	return s.issueSessionChecked(ctx, app, user, provider, loginType, deviceID, device, ip, userAgent, nil)
}

func (s *AuthService) issueSessionChecked(ctx context.Context, app *appdomain.App, user *userdomain.User, provider, loginType, deviceID, device, ip, userAgent string, chosen []string) (*authdomain.LoginResult, error) {
	var admission *DeviceAdmission
	if s.deviceLimit != nil {
		if app == nil && s.app != nil {
			loaded, err := s.app.GetApp(ctx, user.AppID)
			if err != nil {
				return nil, err
			}
			app = loaded
		}
		var challenge *devicelimit.Challenge
		var err error
		admission, challenge, err = s.deviceLimit.Admit(ctx, app, user, DeviceLoginInput{
			Provider: provider, LoginType: loginType, DeviceID: deviceID, Device: device, IP: ip, UserAgent: userAgent,
			Client: devicedomain.ClientFrom(ctx), Chosen: chosen, CanAsk: devicelimit.ChallengeSupported(ctx),
		})
		if err != nil {
			return nil, err
		}
		if challenge != nil {
			// 不签发令牌：客户端凭 ticket 让用户选好要下线的设备，再调 /auth/device-limit/resolve
			return &authdomain.LoginResult{
				UserID: user.ID, PublicUserID: user.PublicID, Account: user.Account, Provider: provider,
				AuthenticationState: AuthStateDeviceLimitRequired, DeviceLimit: challenge,
			}, nil
		}
		defer admission.Release()
	}
	bundle, err := s.issueSessionBundle(ctx, app, user, provider, loginType, deviceID, device, ip, userAgent, "")
	if err != nil {
		return nil, err
	}
	if admission != nil {
		s.deviceLimit.Commit(ctx, admission, IssuedLogin{FamilyID: bundle.RefreshSession.FamilyID})
	}
	if previous := devicedomain.PreviousIDFrom(ctx); previous != "" && deviceID != "" {
		s.migrateDeviceID(user.AppID, user.ID, previous, deviceID)
	}
	return bundle.Result, nil
}

// AuthStateDeviceLimitRequired LoginResult.AuthenticationState：登录设备数已满，等用户选择下线哪台
const AuthStateDeviceLimitRequired = "device_limit_required"

// ResolveDeviceLimitChallenge 用户在选择页勾好了要下线的设备：下线它们并完成那次被挂起的登录。
//
// 登录现场（登录方式、设备、IP）取自暂存的凭证而不是这次请求 —— 选择页可能是在另一个网络环境下提交的，
// 但被签发的仍是当初那台设备的登录。账号与应用状态在这里重新判定一次：选择期间账号可能被封禁。
func (s *AuthService) ResolveDeviceLimitChallenge(ctx context.Context, appID int64, ticket string, chosen []string) (*authdomain.LoginResult, error) {
	if s.deviceLimit == nil {
		return nil, apperrors.New(errCodeDeviceLimitTicket, http.StatusNotFound, "设备选择已失效，请重新登录")
	}
	chosen = uniqueStrings(chosen)
	if len(chosen) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择要下线的设备")
	}
	pending, err := s.deviceLimit.LoadChallenge(ctx, appID, ticket)
	if err != nil {
		return nil, err
	}
	user, err := s.pg.GetUserByID(ctx, pending.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	var app *appdomain.App
	if s.app != nil {
		if app, err = s.app.EnsureLoginAllowed(ctx, appID); err != nil {
			return nil, err
		}
	}
	ctx = devicedomain.WithClient(ctx, devicedomain.Client{Model: pending.Model, Platform: pending.Platform, Extra: pending.Extra.Value()})
	result, err := s.issueSessionChecked(ctx, app, user, pending.Provider, pending.LoginType, pending.DeviceID, pending.Device,
		pending.IP, pending.UserAgent, chosen)
	if err != nil {
		return nil, err
	}
	s.deviceLimit.FinishChallenge(ctx, ticket)
	if result != nil && s.deletion != nil {
		result.AccountDeletion = s.deletion.OnLogin(ctx, user)
	}
	return result, nil
}

// signSessionToken 签发访问/刷新令牌：PASETO v4.local，载荷加密，客户端读不到内容（pkg/sessiontoken）。
func (s *AuthService) signSessionToken(tokenType string, user *userdomain.User, tokenID, familyID string, issuedAt, expiresAt time.Time) (string, error) {
	codec, err := s.tokenCodec()
	if err != nil {
		return "", err
	}
	return codec.Issue(tokenType, tokenID, familyID, user.AppID, issuedAt, expiresAt)
}

type issuedSessionBundle struct {
	Result         *authdomain.LoginResult
	AccessSession  authdomain.Session
	RefreshSession authdomain.RefreshSession
}

func (s *AuthService) issueSessionBundle(ctx context.Context, app *appdomain.App, user *userdomain.User, provider, loginType, deviceID, device, ip, userAgent, refreshFamilyID string) (*issuedSessionBundle, error) {
	if s.app != nil {
		if app == nil {
			// 兜底：调用方未传 app 时拉取一次（MFA 验证等无法提前持有 app 的路径）
			loaded, err := s.app.GetApp(ctx, user.AppID)
			if err != nil {
				return nil, err
			}
			app = loaded
		}
		// 签发阶段仅做 deviceID 兜底校验（宽松版）。
		// device 名称的严格校验已在各登录入口完成；MFA / Passkey / Web-OAuth 回调
		// 链路在签发阶段天然无法携带 device 名称，此前在此重复执行严格校验会把
		// 这些合法流程在 loginCheckDevice 开启时误拦（40026）。
		if err := s.validateLoginDeviceID(app, deviceID); err != nil {
			return nil, err
		}
	}
	now := timeutil.NowUTC()
	accessExpiresAt := now.Add(s.cfg.JWT.TTL)
	refreshExpiresAt := now.Add(s.cfg.JWT.RefreshTTL)
	accessTokenID := uuid.NewString()
	refreshTokenID := uuid.NewString()
	refreshFamilyID = strings.TrimSpace(refreshFamilyID)
	if refreshFamilyID == "" {
		refreshFamilyID = uuid.NewString()
	}
	signedAccess, err := s.signSessionToken("access", user, accessTokenID, refreshFamilyID, now, accessExpiresAt)
	if err != nil {
		return nil, err
	}
	signedRefresh, err := s.signSessionToken("refresh", user, refreshTokenID, refreshFamilyID, now, refreshExpiresAt)
	if err != nil {
		return nil, err
	}
	// 原始型号与平台由登录入口挂在上下文上（刷新时由 Refresh 从旧刷新会话带过来）
	client := devicedomain.ClientFrom(ctx)
	session := authdomain.Session{
		UserID:          user.ID,
		AppID:           user.AppID,
		Account:         user.Account,
		TokenID:         accessTokenID,
		RefreshFamilyID: refreshFamilyID,
		SessionVersion:  1,
		DeviceID:        deviceID,
		Device:          device,
		DeviceModel:     client.Model,
		DevicePlatform:  client.Platform,
		DeviceExtra:     client.Extra.Ref(),
		IP:              ip,
		UserAgent:       userAgent,
		ExpiresAt:       accessExpiresAt,
		IssuedAt:        now,
		Provider:        provider,
	}
	refreshSession := authdomain.RefreshSession{
		UserID:         user.ID,
		AppID:          user.AppID,
		Account:        user.Account,
		TokenID:        refreshTokenID,
		FamilyID:       refreshFamilyID,
		SessionVersion: 1,
		DeviceID:       deviceID,
		Device:         device,
		DeviceModel:    client.Model,
		DevicePlatform: client.Platform,
		DeviceExtra:    client.Extra.Ref(),
		IP:             ip,
		UserAgent:      userAgent,
		Provider:       provider,
		ExpiresAt:      refreshExpiresAt,
		IssuedAt:       now,
	}
	// 访问/刷新会话并行写入 Redis；任一失败则两者全部回滚，杜绝半状态会话
	writeGroup, wctx := errgroup.WithContext(ctx)
	writeGroup.Go(func() error {
		return s.sessions.SetSession(wctx, signedAccess, session, time.Until(accessExpiresAt))
	})
	writeGroup.Go(func() error {
		return s.sessions.SetRefreshSession(wctx, signedRefresh, refreshSession, time.Until(refreshExpiresAt))
	})
	if err := writeGroup.Wait(); err != nil {
		_ = s.sessions.DeleteSession(ctx, signedAccess)
		_ = s.sessions.DeleteRefreshSession(ctx, signedRefresh)
		return nil, err
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectAuthLoginAuditRequested, map[string]any{
		"user_id":    user.ID,
		"appid":      user.AppID,
		"login_type": loginType,
		"provider":   provider,
		"token_jti":  accessTokenID,
		"ip":         ip,
		"device_id":  deviceID,
		"user_agent": userAgent,
		// 整个载荷落进登录记录的 metadata，用户接口据此按字典还原 deviceInfo
		"device":          device,
		"device_model":    client.Model,
		"device_platform": client.Platform,
		"device_extra":    client.Extra.Ref(),
	})
	if s.risk != nil {
		fp := DeviceFingerprint(ctx)
		s.risk.Report(ctx, riskevents.Event{
			Type: riskevents.TypeLoginSuccess, Scene: securitydomain.SceneLogin, AppID: user.AppID, UserID: user.ID, Account: user.Account,
			IP: ip, DeviceID: deviceID, UA: userAgent, Device: fp, Props: map[string]any{"loginType": loginType, "provider": provider},
		})
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
		"user_id":    user.ID,
		"appid":      user.AppID,
		"token_jti":  accessTokenID,
		"event_type": "issued",
		"provider":   provider,
		"login_type": loginType,
		"ip":         ip,
		"device_id":  deviceID,
		"user_agent": userAgent,
	})
	// 标记「必须修改密码」（如导入用户的统一密码场景），登录结果直接携带，客户端无需额外请求。
	// 密码策略的 maxAge 在这里生效：password_expires_at 已过即等同于被要求改密 ——
	// 用登录时推导而不是后台批量刷标记，过期判定永远与当前时间一致，
	// 也不会因为定时任务漏跑而让过期密码继续可用。
	passwordChangeRequired := false
	if state, stateErr := s.pg.GetUserSecurityStateByUserID(ctx, user.ID); stateErr == nil && state != nil {
		if state.PasswordChangeRequired != nil {
			passwordChangeRequired = *state.PasswordChangeRequired
		}
		if !passwordChangeRequired && state.PasswordExpiresAt != nil && state.PasswordExpiresAt.Before(timeutil.NowUTC()) {
			passwordChangeRequired = true
		}
	}
	return &issuedSessionBundle{
		Result: &authdomain.LoginResult{
			AccessToken:            signedAccess,
			RefreshToken:           signedRefresh,
			ExpiresAt:              accessExpiresAt,
			RefreshExpiresAt:       refreshExpiresAt,
			TokenType:              "Bearer",
			UserID:                 user.ID,
			PublicUserID:           user.PublicID,
			Account:                user.Account,
			Provider:               provider,
			PasswordChangeRequired: passwordChangeRequired,
		},
		AccessSession:  session,
		RefreshSession: refreshSession,
	}, nil
}

// validateLoginPolicy 若应用开启"登录设备检查"(LoginCheckDevice)，则强制要求 deviceID + device 双字段均非空。
// 未开启则不做校验（deviceID/device 可以为空，依赖 handler 的 header/UA 回落）。
// 适用于：密码登录 / 密码注册 / 移动端 OAuth / issueSessionBundle 等能拿到完整设备信息的入口。
func (s *AuthService) validateLoginPolicy(app *appdomain.App, deviceID, device string) error {
	if s.app == nil {
		return nil
	}
	policy := s.app.ResolvePolicy(app)
	if !policy.LoginCheckDevice {
		return nil
	}
	if strings.TrimSpace(deviceID) == "" {
		return apperrors.New(40024, http.StatusBadRequest, "当前应用开启登录设备检查，必须提供 deviceId")
	}
	if strings.TrimSpace(device) == "" {
		return apperrors.New(40026, http.StatusBadRequest, "当前应用开启登录设备检查，必须提供 device")
	}
	return nil
}

// validateLoginDeviceID 只校验 deviceID（宽松版）。
// 用于浏览器 302 链路（OAuth Web 起始 / 回调 / Passkey Begin）及 issueSessionBundle 签发兜底——
// 这些阶段无法携带 device 名称，但 deviceID 可以通过 state/payload 继承。
// device 名称的严格校验（validateLoginPolicy）仅在能拿到完整设备信息的登录入口执行一次。
func (s *AuthService) validateLoginDeviceID(app *appdomain.App, deviceID string) error {
	if s.app == nil {
		return nil
	}
	policy := s.app.ResolvePolicy(app)
	if !policy.LoginCheckDevice {
		return nil
	}
	if strings.TrimSpace(deviceID) == "" {
		return apperrors.New(40024, http.StatusBadRequest, "当前应用开启登录设备检查，必须提供 deviceId")
	}
	return nil
}

// InspectLoginBaseline 读取某用户的登录绑定基线（管理端展示用）。
func (s *AuthService) InspectLoginBaseline(ctx context.Context, appID, userID int64) (*appdomain.LoginBaseline, error) {
	if s.consistency == nil {
		return nil, nil
	}
	return s.consistency.Inspect(ctx, appID, userID)
}

// ResetLoginBaseline 重置某用户的登录绑定：下次登录按首次处理并重建基线。
// 这是应用开启设备/IP/属地强绑定后唯一的解绑出口。
func (s *AuthService) ResetLoginBaseline(ctx context.Context, appID, userID int64) error {
	if s.consistency == nil {
		return nil
	}
	return s.consistency.Reset(ctx, appID, userID)
}

// enforceLoginConsistency 按应用策略校验设备绑定 / 登录 IP / 登录属地。
// app 允许为 nil（MFA 二次验证链路不带 app），此时按 user.AppID 补拉一次。
func (s *AuthService) enforceLoginConsistency(ctx context.Context, app *appdomain.App, user *userdomain.User, deviceID, ip string) error {
	if s.consistency == nil || s.app == nil || user == nil {
		return nil
	}
	if app == nil {
		loaded, err := s.app.GetApp(ctx, user.AppID)
		if err != nil {
			return err
		}
		app = loaded
	}
	return s.consistency.Enforce(ctx, s.app.ResolvePolicy(app), user.AppID, user.ID, deviceID, ip)
}

// admitRegister 注册 IP 准入（docs/register-ip.md）。放行时返回票：建号成功调 Commit、没建成调 Release；
// 两者在 nil 票上都是空操作，未接入注册 IP 控制时返回 nil 票。
func (s *AuthService) admitRegister(ctx context.Context, app *appdomain.App, method, ip, account, deviceID, userAgent string) (*RegisterIPTicket, error) {
	if s.registerIP == nil || app == nil {
		return nil, nil
	}
	return s.registerIP.Admit(ctx, RegisterIPRequest{
		AppID: app.ID, App: app, IP: ip, Method: method, Account: account, DeviceID: deviceID, UserAgent: userAgent,
	})
}

func (s *AuthService) handleRefreshReuse(ctx context.Context, session *authdomain.RefreshSession) {
	if session == nil {
		return
	}
	if err := s.sessions.RevokeRefreshFamily(ctx, session.AppID, session.UserID, session.FamilyID, s.cfg.JWT.RefreshTTL); err != nil {
		s.log.Warn("revoke refresh family failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.Error(err))
	}
	if err := s.revokeRefreshFamilySessions(ctx, session.AppID, session.UserID, session.FamilyID); err != nil {
		s.log.Warn("cleanup refresh sessions failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.Error(err))
	}
	if err := s.revokeAccessSessionsByFamily(ctx, session.AppID, session.UserID, session.FamilyID, ""); err != nil {
		s.log.Warn("cleanup access sessions failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.Error(err))
	}
}

func (s *AuthService) revokeRefreshFamilySessions(ctx context.Context, appID int64, userID int64, familyID string) error {
	if familyID == "" {
		return nil
	}
	items, err := s.sessions.ListIndexedRefreshSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Session.FamilyID != familyID {
			continue
		}
		if err := s.sessions.DeleteRefreshSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		ttl := time.Until(item.Session.ExpiresAt)
		if ttl > 0 {
			if err := s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *AuthService) revokeAccessSessionsByFamily(ctx context.Context, appID int64, userID int64, familyID string, exceptTokenID string) error {
	if familyID == "" {
		return nil
	}
	items, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Session.RefreshFamilyID != familyID {
			continue
		}
		if exceptTokenID != "" && item.Session.TokenID == exceptTokenID {
			continue
		}
		if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		ttl := time.Until(item.Session.ExpiresAt)
		if ttl > 0 {
			if err := s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyCredential 登录态凭据校验（时序安全版）：
// 账号无密码（OAuth 创建的账号）时执行哑 bcrypt 比较再返回失败，
// 使其与「密码错误」耗时一致，防止时序探测区分账号类型。
func (s *AuthService) verifyCredential(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(s.timingDummyHash, []byte(password))
		return false
	}
	return verifyPassword(hash, password)
}

func verifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(password)) == 1
}

func hashPassword(password string) (string, error) {
	// bcrypt 硬上限 72 字节：应用密码策略可放宽至 128 位，超限时返回明确的
	// 业务错误码，而非把 bcrypt.ErrPasswordTooLong 透传成 500
	if len(password) > 72 {
		return "", apperrors.New(40008, http.StatusBadRequest, "密码长度不能超过 72 个字符")
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeAccount(account string) string {
	return strings.TrimSpace(account)
}

func registerFlightKey(appID int64, account string) string {
	return fmt.Sprintf("%d:%s", appID, account)
}

func validateAccount(account string) error {
	if account == "" {
		return apperrors.New(40005, http.StatusBadRequest, "账号不能为空")
	}
	if len(account) < 3 || len(account) > 64 {
		return apperrors.New(40005, http.StatusBadRequest, "账号长度必须在 3 到 64 个字符之间")
	}
	return nil
}

func validatePasswordStrength(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return apperrors.New(40007, http.StatusBadRequest, "密码长度不能少于 8 位")
	}
	if len(password) > 72 {
		return apperrors.New(40008, http.StatusBadRequest, "密码长度不能超过 72 位")
	}
	return nil
}

func (s *AuthService) validatePasswordPolicy(ctx context.Context, appID int64, password string) error {
	return s.validatePasswordPolicyWithContext(ctx, appID, password, PasswordContext{})
}

func (s *AuthService) validatePasswordPolicyWithContext(ctx context.Context, appID int64, password string, pctx PasswordContext) error {
	if s.app != nil {
		return s.app.ValidatePasswordWithAppPolicyContext(ctx, appID, password, pctx)
	}
	return validatePasswordStrength(password)
}

// registerPasswordContext 从注册入参里凑出用于强度评估的用户上下文。
//
// 这些串会被当成一份临时字典交给 zxcvbn：账号叫 zhangsan 而密码是
// "Zhangsan2024" 时，它会被识别成字典命中而不是随机串 —— 这类口令在撞库里
// 是最先被试的一批，靠字符类规则永远拦不住。
func registerPasswordContext(input PasswordRegisterInput, app *appdomain.App) PasswordContext {
	pctx := PasswordContext{
		Account:  input.Account,
		Nickname: input.Nickname,
	}
	if app != nil {
		pctx.AppName = app.Name
	}
	// Profile 是自由 map（由各应用的注册表单 schema 决定键名），
	// 邮箱 / 手机号取到就用，取不到不影响其余判定
	pctx.Email = profileString(input.Profile, "email")
	pctx.Phone = profileString(input.Profile, "phone")
	if pctx.Phone == "" {
		pctx.Phone = profileString(input.Profile, "mobile")
	}
	return pctx
}

// userPasswordContext 为已存在的用户凑出强度评估上下文。
//
// 档案读失败时只降级为「只带账号」而不报错：改密不该因为读不到昵称而失败，
// 账号本身已经覆盖了最主要的一类弱口令。
func (s *AuthService) userPasswordContext(ctx context.Context, user *userdomain.User) PasswordContext {
	if user == nil {
		return PasswordContext{}
	}
	pctx := PasswordContext{Account: user.Account}
	profile, err := s.pg.GetUserProfileByUserID(ctx, user.ID)
	if err != nil || profile == nil {
		return pctx
	}
	pctx.Nickname = profile.Nickname
	pctx.Email = profile.Email
	pctx.Phone = profile.Phone
	return pctx
}

func profileString(profile map[string]any, key string) string {
	if profile == nil {
		return ""
	}
	for rawKey, value := range profile {
		if !strings.EqualFold(strings.TrimSpace(rawKey), key) {
			continue
		}
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

func (s *AuthService) upsertUserProfileWithInviteCode(ctx context.Context, profile userdomain.Profile) (string, error) {
	profile.InviteCode = strings.TrimSpace(profile.InviteCode)
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		if profile.InviteCode == "" {
			inviteCode, err := s.generateGlobalInviteCode(ctx)
			if err != nil {
				return "", err
			}
			profile.InviteCode = inviteCode
		}
		if err := s.pg.UpsertUserProfile(ctx, profile); err != nil {
			if pgrepo.IsUniqueViolation(err) {
				profile.InviteCode = ""
				continue
			}
			return "", err
		}
		return profile.InviteCode, nil
	}
	return "", fmt.Errorf("generate invite code exceeded max retries")
}

func (s *AuthService) generateGlobalInviteCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		inviteCode, err := randomInviteCode(inviteCodeLength)
		if err != nil {
			return "", err
		}
		exists, err := s.pg.HasInviteCode(ctx, inviteCode)
		if err != nil {
			return "", err
		}
		if !exists {
			return inviteCode, nil
		}
	}
	return "", fmt.Errorf("generate invite code exceeded max retries")
}

func randomInviteCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid invite code length")
	}
	buffer := make([]byte, length)
	randomBytes := make([]byte, length)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	for i := 0; i < length; i++ {
		buffer[i] = inviteCodeCharset[int(randomBytes[i])%len(inviteCodeCharset)]
	}
	return string(buffer), nil
}

// loginCandidateLimit 一个邮箱 / 手机号最多认几个账号（逐个比对密码，bcrypt 次数的上限）。
const loginCandidateLimit = 5

// resolveLoginCandidates 按应用允许的登录标识（接入策略 identifiers）把输入认成账号。
//
// 顺序：账号名 → 用户 ID（16 位对外 ID）→ 邮箱 → 手机号。账号名永远优先且总是可用 ——
// 老账号可能就叫一个邮箱或一串数字，不能因为新开了标识就登不上。自助设的用户名不许纯数字、
// 不含 @（userdomain.ValidateUsername），所以新账号不会和另外几种标识撞在一起。
// 邮箱、手机号可能挂在多个账号上，全部返回，由密码决定是哪一个。
func (s *AuthService) resolveLoginCandidates(ctx context.Context, appID int64, identifier string) ([]*userdomain.User, error) {
	if identifier == "" {
		return nil, nil
	}
	user, err := s.pg.GetUserByAppAndAccount(ctx, appID, identifier)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return []*userdomain.User{user}, nil
	}
	identifiers, err := s.loginIdentifiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	switch {
	case strings.Contains(identifier, "@"):
		if slices.Contains(identifiers, "email") {
			ids, err = s.pg.ListUserIDsByProfileEmail(ctx, appID, identifier, loginCandidateLimit)
		}
	case isPublicUserIDText(identifier):
		if slices.Contains(identifiers, "id") {
			publicID, _ := strconv.ParseInt(identifier, 10, 64)
			var id int64
			id, err = s.pg.FindUserIDByPublicID(ctx, appID, publicID)
			if id > 0 {
				ids = []int64{id}
			}
		}
	case looksLikePhone(identifier):
		if slices.Contains(identifiers, "phone") {
			ids, err = s.pg.ListUserIDsByProfilePhone(ctx, appID, normalizePhone(identifier), loginCandidateLimit)
		}
	}
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	users := make([]*userdomain.User, 0, len(ids))
	for _, id := range ids {
		loaded, err := s.pg.GetUserByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if loaded != nil && loaded.AppID == appID {
			users = append(users, loaded)
		}
	}
	return users, nil
}

// loginIdentifiers 应用接入策略里开放的登录标识；没配置过时用默认策略。
func (s *AuthService) loginIdentifiers(ctx context.Context, appID int64) ([]string, error) {
	policy, err := s.pg.GetAppAuthProtocolPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return defaultAuthProtocolPolicy(appID).Identifiers, nil
	}
	return policy.Identifiers, nil
}

// isPublicUserIDText 16 位对外用户 ID 的文本形式（[10^15, 2^53−1]，见 docs/user-public-id.md）。
func isPublicUserIDText(value string) bool {
	if len(value) != 16 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id >= 1_000_000_000_000_000 && id <= 1<<53-1
}
