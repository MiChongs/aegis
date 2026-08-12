package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	platformdomain "aegis/internal/domain/platform"
	plugindomain "aegis/internal/domain/plugin"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/egress"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/receipt"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type PaymentService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	client     *resty.Client
	providers  map[string]paymentProvider
	plugin     *PluginService
	receiptCfg config.PaymentReceiptConfig
	// receipts 凭证渲染器。字体在构造时解析一次（可能读十几兆的字体文件），
	// 之后每份凭证只是把内存里的字节交给 PDF 引擎。
	receipts  *receipt.Renderer
	closeCh   chan struct{}
	closeOnce sync.Once
	closed    chan struct{}
	// governance 平台治理判定：应用被冻结时不允许再产生新的资金流水
	governance *PlatformGovernanceService
	// platform 平台设置，凭证抬头取品牌名用
	platform *PlatformSettingsService
	// email 凭证邮件出口；nil 表示未接入，凭证只能下载不能寄送
	email *EmailService
	// apps 应用级交易设置（是否支付成功自动寄送凭证）
	apps *AppService
	// consoleBaseURL 控制台对外地址（CONSOLE_BASE_URL），用于拼支付结果页地址。
	// 留空时同步跳转不下发，渠道展示它自己的结果页 —— 与其把用户送到一个
	// 猜出来的域名，不如让渠道兜底。
	consoleBaseURL string
}

// SetConsoleBaseURL 注入控制台对外地址（bootstrap 中调用）。
func (s *PaymentService) SetConsoleBaseURL(baseURL string) {
	s.consoleBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (s *PaymentService) SetPluginService(p *PluginService) { s.plugin = p }

// SetGovernanceService 注入平台治理服务（bootstrap 中调用）。
func (s *PaymentService) SetGovernanceService(g *PlatformGovernanceService) { s.governance = g }

// SetPlatformSettingsService 注入平台设置服务，凭证抬头用它取品牌名。
func (s *PaymentService) SetPlatformSettingsService(p *PlatformSettingsService) { s.platform = p }

// SetEmailService 注入邮件出口，用于寄送凭证。
func (s *PaymentService) SetEmailService(e *EmailService) { s.email = e }

// SetAppService 注入应用服务，用于读取应用级交易设置。
func (s *PaymentService) SetAppService(a *AppService) { s.apps = a }

func NewPaymentService(log *zap.Logger, pg *pgrepo.Repository, receiptCfg config.PaymentReceiptConfig) *PaymentService {
	// 所有 REST 渠道共用这一个 resty 客户端：把它的 transport 换成出海网关，
	// Stripe / Paddle / Lemon Squeezy / Square / Razorpay / Coinbase 一并生效。
	client := resty.New().
		SetRetryCount(2).
		SetTimeout(10 * time.Second).
		SetTransport(egress.NewTransport(egress.Profile{Name: "payment.gateway"}))
	s := &PaymentService{
		log:        log,
		pg:         pg,
		client:     client,
		providers:  make(map[string]paymentProvider),
		receiptCfg: receiptCfg,
		closeCh:    make(chan struct{}),
		closed:     make(chan struct{}),
	}

	s.registerBuiltinProviders(client)
	s.initReceiptRenderer()
	s.startReceiptCleaner()
	return s
}

// registerBuiltinProviders 注册全部内置支付渠道。
// 单独成函数以便测试复用同一份清单，避免测试与生产注册列表漂移。
func (s *PaymentService) registerBuiltinProviders(client *resty.Client) {
	s.registerProvider(newEpayProvider(client))
	s.registerProvider(newRainbowEpayProvider(client))
	s.registerProvider(newXunhupayProvider(client))
	s.registerProvider(newPayjsProvider(client))
	s.registerProvider(newQRPayProvider(client))
	s.registerProvider(newVMQPayProvider(client))
	s.registerProvider(newAlipayNativeProvider(client))
	s.registerProvider(newWechatNativeProvider(client))
	s.registerProvider(newStripeProvider(client))
	s.registerProvider(newPaypalProvider(client))
	s.registerProvider(newPaddleProvider(client))
	s.registerProvider(newLemonSqueezyProvider(client))
	s.registerProvider(newRazorpayProvider(client))
	s.registerProvider(newCoinbaseProvider(client))
	s.registerProvider(newSquareProvider(client))
	s.registerProvider(newBalanceProvider())
}

func (s *PaymentService) Close(context.Context) {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		<-s.closed
	})
}

func (s *PaymentService) registerProvider(p paymentProvider) {
	s.providers[p.Name()] = p
}

func (s *PaymentService) resolveProvider(method string) (paymentProvider, error) {
	p, ok := s.providers[strings.TrimSpace(method)]
	if !ok {
		return nil, apperrors.New(40079, http.StatusBadRequest, "不支持的支付方式: "+method)
	}
	return p, nil
}

// methodOrder 渠道市场的稳定展示顺序：内部通道 → 国内直连 → 聚合 → 国际 → 加密货币。
// map 迭代顺序随机，若直接返回会导致控制台渠道列表每次刷新都跳动。
var methodOrder = []string{
	paymentdomain.MethodBalance,
	paymentdomain.MethodAlipayNative,
	paymentdomain.MethodWechatNative,
	paymentdomain.MethodEpay,
	paymentdomain.MethodRainbowEpay,
	paymentdomain.MethodXunhupay,
	paymentdomain.MethodPayjs,
	paymentdomain.MethodQRPay,
	paymentdomain.MethodVMQPay,
	paymentdomain.MethodStripe,
	paymentdomain.MethodPaypal,
	paymentdomain.MethodPaddle,
	paymentdomain.MethodLemonSqueezy,
	paymentdomain.MethodSquare,
	paymentdomain.MethodRazorpay,
	paymentdomain.MethodCoinbase,
}

// AvailableMethods 返回所有已注册支付方式的完整描述（含配置字段 schema），
// 顺序稳定。控制台的渠道市场与动态配置表单完全由该结果驱动。
func (s *PaymentService) AvailableMethods() []paymentdomain.ProviderMeta {
	items := make([]paymentdomain.ProviderMeta, 0, len(s.providers))
	seen := make(map[string]bool, len(s.providers))
	for _, method := range methodOrder {
		if p, ok := s.providers[method]; ok {
			items = append(items, p.Describe())
			seen[method] = true
		}
	}
	// 兜底：methodOrder 未覆盖的渠道按标识排序追加，避免新增后遗漏
	rest := make([]string, 0)
	for method := range s.providers {
		if !seen[method] {
			rest = append(rest, method)
		}
	}
	sort.Strings(rest)
	for _, method := range rest {
		items = append(items, s.providers[method].Describe())
	}
	return items
}

// MethodMeta 返回单个渠道的描述信息
func (s *PaymentService) MethodMeta(method string) (paymentdomain.ProviderMeta, error) {
	p, err := s.resolveProvider(method)
	if err != nil {
		return paymentdomain.ProviderMeta{}, err
	}
	return p.Describe(), nil
}

// enforceAmountLimits 网关层统一的单笔限额校验。
//
// 限额字段（minAmount / maxAmount）是所有渠道的通用约定，此前仅易支付与虎皮椒在各自
// CreateOrder 内自行校验，其余 9 个渠道配置了限额也不会生效。这里在下单主链路上统一拦截，
// 任何渠道（含后续新增的）都自动获得限额保护。
func enforceAmountLimits(p paymentProvider, config *paymentdomain.Config, amount decimal.Decimal) error {
	if config == nil {
		return nil
	}
	min := configFloat(config.ConfigData, "minAmount")
	max := configFloat(config.ConfigData, "maxAmount")
	if min <= 0 && max <= 0 {
		return nil
	}
	return checkAmountRange(amount, min, max, providerLabel(p))
}

// ── 配置管理 ──

func (s *PaymentService) ListConfigs(ctx context.Context, appID int64, paymentMethod string, enabledOnly bool) ([]paymentdomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListPaymentConfigs(ctx, appID, paymentMethod, enabledOnly)
}

func (s *PaymentService) Detail(ctx context.Context, appID int64, configID int64) (*paymentdomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetPaymentConfigByID(ctx, appID, configID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "支付配置不存在")
	}
	return item, nil
}

func (s *PaymentService) Save(ctx context.Context, mutation paymentdomain.ConfigMutation) (*paymentdomain.Config, error) {
	if _, err := s.requireApp(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetPaymentConfigByID(ctx, mutation.AppID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := paymentdomain.Config{
		ID:            mutation.ID,
		AppID:         mutation.AppID,
		PaymentMethod: "epay",
		ConfigName:    "default",
		ConfigData:    map[string]any{},
		Enabled:       true,
		IsDefault:     mutation.ID == 0,
	}
	if current != nil {
		item = *current
	}
	if mutation.PaymentMethod != nil {
		item.PaymentMethod = strings.TrimSpace(*mutation.PaymentMethod)
	}
	if mutation.ConfigName != nil {
		item.ConfigName = strings.TrimSpace(*mutation.ConfigName)
	}
	if mutation.ConfigData != nil {
		item.ConfigData = mutation.ConfigData
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.IsDefault != nil {
		item.IsDefault = *mutation.IsDefault
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if item.ConfigName == "" {
		return nil, apperrors.New(40070, http.StatusBadRequest, "支付配置名称不能为空")
	}
	if item.PaymentMethod == "" {
		return nil, apperrors.New(40071, http.StatusBadRequest, "支付方式不能为空")
	}

	// 通过 Provider 验证配置
	provider, err := s.resolveProvider(item.PaymentMethod)
	if err != nil {
		return nil, err
	}
	if err := provider.ValidateConfig(item.ConfigData); err != nil {
		return nil, err
	}

	return s.pg.UpsertPaymentConfig(ctx, item)
}

func (s *PaymentService) Delete(ctx context.Context, appID int64, configID int64) error {
	deleted, err := s.pg.DeletePaymentConfig(ctx, appID, configID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40470, http.StatusNotFound, "支付配置不存在")
	}
	return nil
}

func (s *PaymentService) TestConfig(ctx context.Context, appID int64, configID int64) (map[string]any, error) {
	config, err := s.Detail(ctx, appID, configID)
	if err != nil {
		return nil, err
	}
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "message": "不支持的支付方式: " + config.PaymentMethod}, nil
	}
	return provider.TestConnection(ctx, config.ConfigData)
}

func (s *PaymentService) InitDefaultEpayConfig(ctx context.Context, appID int64, cfg paymentdomain.EpayConfig) (*paymentdomain.Config, error) {
	name := "default"
	method := paymentdomain.MethodEpay
	isDefault := true
	enabled := true
	return s.Save(ctx, paymentdomain.ConfigMutation{
		AppID:         appID,
		PaymentMethod: &method,
		ConfigName:    &name,
		ConfigData: map[string]any{
			"pid":            cfg.PID,
			"key":            cfg.Key,
			"apiUrl":         cfg.APIURL,
			"sitename":       cfg.SiteName,
			"notifyUrl":      cfg.NotifyURL,
			"returnUrl":      cfg.ReturnURL,
			"signType":       cfg.SignType,
			"supportedTypes": cfg.SupportedTypes,
			"expireMinutes":  cfg.ExpireMinutes,
			"minAmount":      cfg.MinAmount,
			"maxAmount":      cfg.MaxAmount,
			"allowedIPs":     cfg.AllowedIPs,
			"verifyIP":       cfg.VerifyIP,
		},
		Enabled:   &enabled,
		IsDefault: &isDefault,
	})
}

// ── 订单流程 ──

// resolveNotifyURL 异步通知地址的三级取值：下单请求 → 渠道配置 → 平台默认。
//
// 平台默认这一级此前**不存在**，而配置表单上一直写着「留空则使用平台默认回调地址」。
// 后果是留空时 `notify_url` 空着发给上游：用户付了钱，渠道无处通知，订单永远停在
// 待支付，权益也永远不会发放。这类故障不报任何错，只有对账时才发现 ——
// 比直接拒绝下单危险得多。
//
// 路径取自渠道自己的 `Describe().CallbackPath`，与控制台展示的那条同源，
// 不另建一张 method → 路径的表。`API_BASE_URL` 没配时返回空串、退回原来的行为
// （微信当场报错，其余渠道由上游拒绝），绝不拿一个猜出来的域名去兑现承诺 ——
// 错的回调地址比没有更难查。
//
// 顺序不能反：管理员在渠道里填了地址，就该盖过平台默认值。
func (s *PaymentService) resolveNotifyURL(provider paymentProvider, config *paymentdomain.Config, requested string) string {
	if resolved := pickString(requested, configString(config.ConfigData, "notifyUrl")); resolved != "" {
		return resolved
	}
	base := strings.TrimRight(strings.TrimSpace(s.receiptCfg.PublicBaseURL), "/")
	path := strings.TrimSpace(provider.Describe().CallbackPath)
	if base == "" || path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// resolveReturnURL 同步跳转地址的三级取值：下单请求 → 渠道配置 → 平台结果页。
//
// 平台默认指向控制台自带的 `/pay/result` —— 那一页只凭渠道的签名 query 展示订单状态，
// 不需要登录态，正是给这种「刚付完款、被浏览器甩回来」的场景准备的。
//
// `CONSOLE_BASE_URL` 没配时**不下发** return_url，由渠道展示它自己的结果页。
// 这里绝不用 API_BASE_URL 顶替：那是后端地址，`/pay/result` 在它上面是 404，
// 把刚付过钱的用户送到一个 404 比不跳转糟得多。
func (s *PaymentService) resolveReturnURL(config *paymentdomain.Config, requested string) string {
	if resolved := pickString(requested, configString(config.ConfigData, "returnUrl")); resolved != "" {
		return resolved
	}
	if s.consoleBaseURL == "" {
		return ""
	}
	return s.consoleBaseURL + PaymentResultPath
}

// PaymentResultPath 控制台支付结果页的路径。改这里必须同步改
// aegis-console 的 src/app/pay/result/，以及渠道配置表单里 returnUrl 的占位提示。
const PaymentResultPath = "/pay/result"

func (s *PaymentService) CreateOrder(ctx context.Context, session *authdomain.Session, subject string, body string, amount string, providerType string, configName string, notifyURL string, returnURL string, metadata map[string]any, clientIP string) (*paymentdomain.PaymentPayload, *paymentdomain.Order, error) {
	if session == nil {
		return nil, nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	// 平台治理：冻结中的应用不能再收钱。放在最前面判 ——
	// 一旦订单落库就会牵扯履约与退款，事后清理的代价远高于当场拒绝。
	if s.governance != nil {
		if err := s.governance.EnsureCapability(session.AppID, platformdomain.CapabilityPayment); err != nil {
			return nil, nil, err
		}
	}

	// 查找配置（优先按 configName 精确匹配，否则取默认）
	config, err := s.pg.GetPaymentConfig(ctx, session.AppID, "", configName)
	if err != nil {
		return nil, nil, err
	}
	if config == nil || !config.Enabled {
		return nil, nil, apperrors.New(40471, http.StatusNotFound, "未找到可用支付配置")
	}

	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return nil, nil, err
	}

	parsedAmount, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || !parsedAmount.IsPositive() {
		return nil, nil, apperrors.New(40072, http.StatusBadRequest, "支付金额无效")
	}
	if strings.TrimSpace(subject) == "" {
		return nil, nil, apperrors.New(40079, http.StatusBadRequest, "商品名称不能为空")
	}
	// 网关层统一限额校验：覆盖全部渠道（含余额支付），下单前拦截超限金额
	if err := enforceAmountLimits(provider, config, parsedAmount); err != nil {
		return nil, nil, err
	}

	// 履约 purpose 处理：校验参数合法性并把套餐/积分快照固化进订单 metadata，
	// 价格以服务端配置为准（防客户端改价），支付成功后按快照自动履约
	metadata, err = s.prepareFulfillmentMetadata(ctx, session.AppID, parsedAmount, metadata)
	if err != nil {
		return nil, nil, err
	}
	// 余额支付约束：余额充值订单不允许用余额支付（自我抵消且会造成累计值虚增）
	if config.PaymentMethod == paymentdomain.MethodBalance &&
		metaString(metadata, paymentdomain.MetaKeyPurpose) == paymentdomain.PurposeWalletRecharge {
		return nil, nil, apperrors.New(40092, http.StatusBadRequest, "余额充值订单不能使用余额支付")
	}

	orderNo := fmt.Sprintf("P%d%s%s", session.AppID, time.Now().UTC().Format("20060102150405"), randomDigits(6))
	expireAt := time.Now().Add(30 * time.Minute)

	order, err := s.pg.CreatePaymentOrder(ctx, paymentdomain.OrderMutation{
		AppID:    session.AppID,
		UserID:   &session.UserID,
		ConfigID: config.ID,
		OrderNo:  orderNo,
		Subject:  strings.TrimSpace(subject),
		Body:     strings.TrimSpace(body),
		Amount:   parsedAmount,
		// 币种在此固化。商户随时可能改渠道的计价货币，配置改了不该让
		// 已经收过的那笔钱在凭证上变成另一种货币。
		Currency:      s.resolveConfigCurrency(config.PaymentMethod, config.ConfigData),
		PaymentMethod: config.PaymentMethod,
		ProviderType:  strings.TrimSpace(providerType),
		ClientIP:      clientIP,
		NotifyURL:     pickString(notifyURL, ""),
		ReturnURL:     pickString(returnURL, ""),
		Metadata:      metadata,
		ExpireAt:      &expireAt,
	})
	if err != nil {
		return nil, nil, err
	}

	// 余额支付：内部通道，无需外部下单——同事务完成扣款、支付确认与履约后直接返回
	if config.PaymentMethod == paymentdomain.MethodBalance {
		payload, payErr := s.payOrderWithBalance(ctx, order)
		if payErr != nil {
			return nil, nil, payErr
		}
		// 回读订单：手里这份是状态翻转**之前**的快照，直接返回会让客户端
		// 收到「支付成功 + status: pending」这种自相矛盾的响应，
		// 顺带把凭证区块算成「账单」而不是「收据」。
		order = s.reloadOrderAfterPayment(ctx, order)
		// 余额支付在同一个事务里就完成了扣款与确认，此处即是「首次确认」
		go s.autoEmailReceipt(session.AppID, order.OrderNo)
		if s.plugin != nil {
			appID := session.AppID
			uid := session.UserID
			go s.plugin.ExecuteHook(context.Background(), HookPaymentCreated, map[string]any{"orderId": order.ID}, plugindomain.HookMetadata{AppID: &appID, UserID: &uid})
			go s.plugin.ExecuteHook(context.Background(), HookPaymentCompleted, map[string]any{"orderId": order.ID}, plugindomain.HookMetadata{AppID: &appID, UserID: &uid})
		}
		return payload, order, nil
	}

	payload, err := provider.CreateOrder(ctx, config.ConfigData, PaymentOrderRequest{
		AppID:        session.AppID,
		OrderNo:      orderNo,
		Subject:      order.Subject,
		Body:         order.Body,
		Amount:       parsedAmount,
		ProviderType: strings.TrimSpace(providerType),
		NotifyURL:    s.resolveNotifyURL(provider, config, order.NotifyURL),
		ReturnURL:    s.resolveReturnURL(config, order.ReturnURL),
		ClientIP:     clientIP,
		Metadata:     metadata,
		ExpireAt:     &expireAt,
	})
	if err != nil {
		return nil, nil, err
	}
	if s.plugin != nil {
		appID := session.AppID
		go s.plugin.ExecuteHook(context.Background(), HookPaymentCreated, map[string]any{
			"orderId": order.ID,
		}, plugindomain.HookMetadata{AppID: &appID, UserID: &session.UserID})
	}
	return payload, order, nil
}

// payOrderWithBalance 余额支付：解析履约指令后交由仓储层单事务执行
func (s *PaymentService) payOrderWithBalance(ctx context.Context, order *paymentdomain.Order) (*paymentdomain.PaymentPayload, error) {
	instr, hasInstr, err := s.buildFulfillmentInstruction(order)
	if err != nil {
		return nil, apperrors.New(40093, http.StatusBadRequest, "订单履约参数无效："+err.Error())
	}
	var instrPtr *paymentdomain.FulfillmentInstruction
	if hasInstr {
		instrPtr = &instr
	}
	walletTxn, err := s.pg.PayPaymentOrderWithWallet(ctx, order, instrPtr)
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrInsufficientBalance):
			return nil, apperrors.New(40083, http.StatusBadRequest, "余额不足，请先充值")
		case errors.Is(err, pgrepo.ErrOrderNotPayable):
			return nil, apperrors.New(40094, http.StatusConflict, "订单不可支付（已支付或已过期）")
		case errors.Is(err, pgrepo.ErrUserNotFound):
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		default:
			return nil, err
		}
	}
	s.log.Info("payment order paid with balance",
		zap.String("orderNo", order.OrderNo), zap.String("walletTxn", walletTxn.TransactionNo))
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      order.OrderNo,
		Message:      "余额支付成功",
		ProviderType: paymentdomain.MethodBalance,
		FormData: map[string]any{
			"paid":                true,
			"walletTransactionNo": walletTxn.TransactionNo,
			"balanceAfter":        walletTxn.BalanceAfter,
		},
	}, nil
}

// reloadOrderAfterPayment 支付确认后回读订单。
//
// 读失败时退回原快照而不是报错：钱已经扣了、权益也发了，
// 为了一次查库抖动把整个下单请求判失败，会让客户端以为支付没成功而重试。
func (s *PaymentService) reloadOrderAfterPayment(ctx context.Context, order *paymentdomain.Order) *paymentdomain.Order {
	fresh, err := s.pg.GetPaymentOrderByOrderNo(ctx, order.OrderNo)
	if err != nil || fresh == nil {
		if err != nil {
			s.log.Warn("reload order after balance payment failed",
				zap.String("orderNo", order.OrderNo), zap.Error(err))
		}
		return order
	}
	return fresh
}

func (s *PaymentService) QueryOrder(ctx context.Context, orderNo string) (*paymentdomain.Order, error) {
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	return order, nil
}

func (s *PaymentService) GetUserOrder(ctx context.Context, session *authdomain.Session, orderNo string) (*paymentdomain.Order, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	order, err := s.pg.GetPaymentOrderByOrderNoForUser(ctx, session.AppID, session.UserID, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	// 查单兜底：已支付订单若因回调期故障尚未履约，在用户查单时补偿执行（幂等）
	if order.Status == "paid" {
		if err := s.ensureOrderFulfilled(ctx, order); err != nil {
			s.log.Warn("lazy fulfillment on user query failed",
				zap.String("orderNo", order.OrderNo), zap.Error(err))
		}
	}
	return order, nil
}

func (s *PaymentService) ListUserOrders(ctx context.Context, session *authdomain.Session, query paymentdomain.OrderListQuery) (*paymentdomain.OrderListResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.pg.ListPaymentOrdersByUser(ctx, session.AppID, session.UserID, query.Status, page, limit)
	if err != nil {
		return nil, err
	}
	return &paymentdomain.OrderListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPaymentTotalPages(total, limit),
	}, nil
}

func (s *PaymentService) QueryEpayRemoteOrder(ctx context.Context, orderNo string) (map[string]any, error) {
	return s.QueryRemoteOrder(ctx, orderNo)
}

// QueryRemoteOrder 向上游查询订单状态（通用）
func (s *PaymentService) QueryRemoteOrder(ctx context.Context, orderNo string) (map[string]any, error) {
	order, err := s.QueryOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, apperrors.New(40473, http.StatusNotFound, "支付配置不存在")
	}
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return nil, err
	}
	return provider.QueryRemoteOrder(ctx, config.ConfigData, orderNo)
}

// HandleEpayCallback 处理易支付回调（兼容旧路由）
func (s *PaymentService) HandleEpayCallback(ctx context.Context, callbackData map[string]string, callbackMethod string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return s.HandleCallback(ctx, paymentdomain.MethodEpay, callbackData, callbackMethod, clientIP)
}

// VerifyPaymentReturn 校验同步跳转参数并返回可展示的订单状态。
//
// 与 HandleCallback 的分工是这套设计的关键：
//
//	异步通知（渠道 → 服务端）：唯一能改变订单状态的入口
//	同步跳转（渠道 → 用户浏览器）：只读，回答「这笔单现在什么状态」
//
// 结果页绝不能拿浏览器带回来的 trade_status 当结论去翻转订单 —— 那串 query
// 停在用户手里，可以重放、可以只走一半。真正的钱到账与否只有服务端收到的
// 那次通知说了算，本函数因此**没有任何写操作**。
//
// 定位顺序：out_trade_no 找到订单 → 用**订单自己的**那份渠道配置验签。
// 渠道不从 URL 上取 —— 订单号已经唯一确定了它是哪家渠道的单，让调用方再声明一次
// 只是多一处能配错的地方，还得防着「拿 A 渠道的密钥验 B 渠道的单」。
//
// 先查库再验签是必然的：不知道是哪笔订单就不知道该用谁的密钥。查库本身不泄露
// 任何东西 —— 验不过就什么都不返回。
func (s *PaymentService) VerifyPaymentReturn(ctx context.Context, params map[string]string) (*paymentdomain.ReturnView, error) {
	orderNo := strings.TrimSpace(params["out_trade_no"])
	if orderNo == "" {
		return nil, apperrors.New(40074, http.StatusBadRequest, "缺少订单号")
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, apperrors.New(40473, http.StatusNotFound, "支付配置不存在")
	}
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return nil, err
	}
	verifier, ok := provider.(paymentReturnVerifier)
	if !ok {
		return nil, apperrors.New(40097, http.StatusBadRequest, "该支付渠道不支持同步跳转校验，请回到应用内查看订单状态")
	}

	signedOrderNo, err := verifier.VerifyReturn(config.ConfigData, params)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(signedOrderNo) != order.OrderNo {
		return nil, apperrors.New(40095, http.StatusBadRequest, "回跳订单号不一致")
	}

	return &paymentdomain.ReturnView{
		OrderNo:       order.OrderNo,
		Subject:       order.Subject,
		Amount:        order.Amount,
		Currency:      order.Currency,
		PaymentMethod: order.PaymentMethod,
		ProviderType:  order.ProviderType,
		Status:        order.Status,
		PaidAt:        order.PaidAt,
		Pending:       order.Status == "pending",
	}, nil
}

// HandleCallback 处理通用支付回调
func (s *PaymentService) HandleCallback(ctx context.Context, method string, callbackData map[string]string, callbackMethod string, clientIP string) (*paymentdomain.CallbackResult, error) {
	// 订单定位链：表单字段 out_trade_no（易支付系/支付宝）
	// → 提供商从原始 Webhook 报文自提（Stripe / PayPal，其回调地址为平台级配置无法携带参数）
	orderNo := strings.TrimSpace(callbackData["out_trade_no"])
	if orderNo == "" {
		if p, ok := s.providers[strings.TrimSpace(method)]; ok {
			if extractor, ok := p.(callbackOrderExtractor); ok {
				orderNo = strings.TrimSpace(extractor.ExtractOrderNo(callbackData))
			}
		}
	}

	// config-first 流程（微信支付等加密通知）：报文本身无法预读订单号，
	// 由回调路径段 /callback/:method/:appid 提供应用标识 → 取该应用的方法默认配置
	// → 先验签解密得到订单号，再定位订单做交叉校验
	var preResult *paymentdomain.CallbackResult
	var config *paymentdomain.Config
	if orderNo == "" {
		appIDRaw := strings.TrimSpace(callbackData["__app_id"])
		if appIDRaw == "" {
			return nil, apperrors.New(40074, http.StatusBadRequest, "缺少订单号")
		}
		appID, err := parseInt64(appIDRaw)
		if err != nil || appID <= 0 {
			return nil, apperrors.New(40074, http.StatusBadRequest, "回调应用标识无效")
		}
		config, err = s.pg.GetPaymentConfig(ctx, appID, strings.TrimSpace(method), "")
		if err != nil {
			return nil, err
		}
		if config == nil {
			return nil, apperrors.New(40473, http.StatusNotFound, "支付配置不存在")
		}
		p, perr := s.resolveProvider(config.PaymentMethod)
		if perr != nil {
			return nil, perr
		}
		res, herr := p.HandleCallback(ctx, config.ConfigData, callbackData, clientIP)
		if herr != nil {
			_ = s.pg.CreatePaymentCallbackLog(ctx, appID, nil, method, callbackMethod, clientIP, mapStringAny(callbackData), "verify_failed", herr.Error())
			return nil, herr
		}
		preResult = res
		orderNo = strings.TrimSpace(res.OrderNo)
	}
	if orderNo == "" {
		return nil, apperrors.New(40074, http.StatusBadRequest, "缺少订单号")
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	// config-first 流程的归属校验：订单必须属于路径段声明的应用
	if config != nil && order.AppID != config.AppID {
		_ = s.pg.CreatePaymentCallbackLog(ctx, config.AppID, &order.ID, method, callbackMethod, clientIP, mapStringAny(callbackData), "app_mismatch", "回调应用与订单归属不一致")
		return nil, apperrors.New(40095, http.StatusBadRequest, "回调应用与订单归属不一致")
	}
	if config == nil {
		config, err = s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
		if err != nil {
			return nil, err
		}
		if config == nil {
			return nil, apperrors.New(40473, http.StatusNotFound, "支付配置不存在")
		}
	}
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, mapStringAny(callbackData), "unsupported_method", "不支持的支付方式")
		return nil, err
	}

	// config-first 流程已完成验签则复用结果，否则此处执行验签
	result := preResult
	if result == nil {
		result, err = provider.HandleCallback(ctx, config.ConfigData, callbackData, clientIP)
		if err != nil {
			_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, mapStringAny(callbackData), "verify_failed", err.Error())
			return nil, err
		}
	}

	// 验签通过后的交叉校验：回调中的订单号 / 金额必须与本地订单一致，
	// 防止「用 A 订单的合法回调骗 B 订单发货」与小额支付大额订单
	if result.OrderNo != "" && result.OrderNo != order.OrderNo {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, result.RawData, "order_mismatch", "回调订单号与本地订单不一致")
		return nil, apperrors.New(40095, http.StatusBadRequest, "回调订单号不一致")
	}
	if result.Paid && result.Amount.IsPositive() && !result.Amount.Equal(order.Amount) {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, result.RawData, "amount_mismatch",
			fmt.Sprintf("回调金额 %s 与订单金额 %s 不一致", result.Amount.StringFixed(2), order.Amount.StringFixed(2)))
		return nil, apperrors.New(40096, http.StatusBadRequest, "回调金额与订单不一致")
	}

	// 更新订单状态
	if result.Paid {
		// 幂等确认支付：仅首次回调真正翻转状态，重复回调直接跳过写入
		firstTime, err := s.pg.MarkPaymentOrderPaid(ctx, order.ID, result.ProviderOrderNo, result.TradeStatus, result.RawData)
		if err != nil {
			return nil, err
		}
		// 履约与「首次确认」解耦：每次已支付回调都尝试履约，
		// 仓储层条件抢占保证恰好执行一次；若上次履约失败回滚，本次回调即是重试
		if err := s.ensureOrderFulfilled(ctx, order); err != nil {
			s.log.Error("payment order fulfillment failed",
				zap.String("orderNo", order.OrderNo), zap.Int64("orderId", order.ID), zap.Error(err))
			// 返回错误让支付平台按其重试策略再次通知，直至履约成功
			return nil, apperrors.New(50080, http.StatusInternalServerError, "订单履约处理失败，请稍后重试")
		}
		if firstTime {
			// 凭证寄送与插件钩子都挂在「首次确认」上：回调会重复到达，
			// 挂在每次回调上会让用户每收到一次重放就多一封收据。
			go s.autoEmailReceipt(order.AppID, order.OrderNo)
			if s.plugin != nil {
				appID := order.AppID
				go s.plugin.ExecuteHook(context.Background(), HookPaymentCompleted, map[string]any{
					"orderId": order.ID,
				}, plugindomain.HookMetadata{AppID: &appID})
			}
		}
	} else {
		if err := s.pg.MarkPaymentOrderCallbackFailed(ctx, order.ID, result.TradeStatus, result.RawData); err != nil {
			return nil, err
		}
	}

	result.OrderNo = order.OrderNo
	result.Amount = order.Amount
	_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, result.RawData, result.TradeStatus, "ok")
	return result, nil
}

// ── 履约（purpose）──

// prepareFulfillmentMetadata 下单阶段处理履约意图：
//   - 无 purpose：普通商品订单，原样放行（不参与自动履约）
//   - wallet_recharge：支付金额即入账金额，无需附加参数
//   - vip_purchase：校验套餐存在且在售、金额与服务端价格一致，并把套餐快照固化进 metadata
//   - integral_purchase：按应用配置兑换率（settings.integralPerCurrency，默认 100/单位金额）
//     由服务端计算并固化发放数量，客户端无法指定
func (s *PaymentService) prepareFulfillmentMetadata(ctx context.Context, appID int64, amount decimal.Decimal, metadata map[string]any) (map[string]any, error) {
	purpose := metaString(metadata, paymentdomain.MetaKeyPurpose)
	if purpose == "" {
		return metadata, nil
	}
	out := make(map[string]any, len(metadata)+4)
	for k, v := range metadata {
		out[k] = v
	}
	switch purpose {
	case paymentdomain.PurposeWalletRecharge:
		// 金额 1:1 入账，无附加快照
	case paymentdomain.PurposeVipPurchase:
		planID := metaInt64(metadata, paymentdomain.MetaKeyVipPlanID)
		if planID <= 0 {
			return nil, apperrors.New(40084, http.StatusBadRequest, "VIP 直购订单必须携带 vipPlanId")
		}
		plan, err := s.pg.GetVipPlan(ctx, appID, planID)
		if err != nil {
			return nil, err
		}
		if plan == nil || !plan.IsActive {
			return nil, apperrors.New(40480, http.StatusNotFound, "套餐不存在或已下架")
		}
		// 试用套餐 0 元：让它进支付链路等于开了一条"下个 0 元单就发试用"的路，
		// 且完全绕过一人一次的资格判定。与余额购买那条闸门是同一件事。
		if plan.IsTrial() {
			return nil, apperrors.New(errCodeTrialPlanNotPurchase, http.StatusForbidden,
				"试用套餐只能领取，不能购买")
		}
		if !plan.Price.Equal(amount) {
			return nil, apperrors.New(40089, http.StatusBadRequest, "支付金额与套餐价格不一致")
		}
		out[paymentdomain.MetaKeyVipPlanID] = plan.ID
		out[paymentdomain.MetaKeyVipPlanName] = plan.Name
		out[paymentdomain.MetaKeyVipDays] = plan.DurationDays
		out[paymentdomain.MetaKeyVipBonus] = plan.BonusIntegral
		out[paymentdomain.MetaKeyVipFeatures] = vipdomain.NormalizeFeatureTags(plan.Features)
	case paymentdomain.PurposeIntegralPurchase:
		app, err := s.pg.GetAppByID(ctx, appID)
		if err != nil {
			return nil, err
		}
		if app == nil {
			return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
		}
		rate := int64(resolveCommerceSettings(app).IntegralPerCurrency)
		integralAmount := amount.Mul(decimal.NewFromInt(rate)).IntPart()
		if integralAmount <= 0 {
			return nil, apperrors.New(40090, http.StatusBadRequest, "支付金额过小，无法兑换积分")
		}
		out[paymentdomain.MetaKeyIntegralAmount] = integralAmount
	default:
		return nil, apperrors.New(40091, http.StatusBadRequest, "不支持的订单用途: "+purpose)
	}
	return out, nil
}

// buildFulfillmentInstruction 从订单 metadata 快照解析履约指令。
// 返回 (指令, 是否需要履约, 错误)；无 purpose 的普通商品订单返回 hasInstr=false。
func (s *PaymentService) buildFulfillmentInstruction(order *paymentdomain.Order) (paymentdomain.FulfillmentInstruction, bool, error) {
	var instr paymentdomain.FulfillmentInstruction
	if order == nil {
		return instr, false, nil
	}
	purpose := metaString(order.Metadata, paymentdomain.MetaKeyPurpose)
	if purpose == "" {
		return instr, false, nil
	}
	if order.UserID == nil || *order.UserID <= 0 {
		return instr, false, fmt.Errorf("order %s has fulfillment purpose %q but no user", order.OrderNo, purpose)
	}
	instr.Purpose = purpose
	switch purpose {
	case paymentdomain.PurposeWalletRecharge:
		instr.WalletAmount = order.Amount
	case paymentdomain.PurposeVipPurchase:
		planID := metaInt64(order.Metadata, paymentdomain.MetaKeyVipPlanID)
		instr.VipPlanID = &planID
		instr.VipPlanName = metaString(order.Metadata, paymentdomain.MetaKeyVipPlanName)
		instr.VipFeatures = metaStringSlice(order.Metadata, paymentdomain.MetaKeyVipFeatures)
		instr.VipDays = int(metaInt64(order.Metadata, paymentdomain.MetaKeyVipDays))
		instr.VipBonus = metaInt64(order.Metadata, paymentdomain.MetaKeyVipBonus)
		if instr.VipDays <= 0 {
			return instr, false, fmt.Errorf("order %s vip snapshot invalid: durationDays=%d", order.OrderNo, instr.VipDays)
		}
	case paymentdomain.PurposeIntegralPurchase:
		instr.IntegralAmount = metaInt64(order.Metadata, paymentdomain.MetaKeyIntegralAmount)
		if instr.IntegralAmount <= 0 {
			return instr, false, fmt.Errorf("order %s integral snapshot invalid", order.OrderNo)
		}
	default:
		return instr, false, fmt.Errorf("order %s has unknown purpose %q", order.OrderNo, purpose)
	}
	return instr, true, nil
}

// ensureOrderFulfilled 按订单 metadata 快照执行履约（幂等，可安全重复调用）。
// 无 purpose 的普通订单直接返回 nil。
func (s *PaymentService) ensureOrderFulfilled(ctx context.Context, order *paymentdomain.Order) error {
	instr, hasInstr, err := s.buildFulfillmentInstruction(order)
	if err != nil {
		return err
	}
	if !hasInstr {
		return nil
	}
	fulfilled, err := s.pg.FulfillPaymentOrder(ctx, order, instr)
	if err != nil {
		return err
	}
	if fulfilled {
		s.log.Info("payment order fulfilled",
			zap.String("orderNo", order.OrderNo), zap.String("purpose", instr.Purpose),
			zap.Int64("userId", *order.UserID), zap.Int64("appid", order.AppID))
	}
	return nil
}

// metaString / metaInt64 从 JSON 反序列化的 metadata 中安全取值
// metaStringSlice 从订单 metadata 里取一组字符串。
//
// 元数据经过一次 JSON 往返（落库时 marshal、读出来 unmarshal），
// `[]string` 回来是 `[]any` —— 直接类型断言成 `[]string` 会永远失败，
// 而失败的表现是"功能快照悄悄丢了"，用户付了钱却少一项权益。
func metaStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	switch value := m[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func metaInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

// ── 管理端订单查询 ──

// AdminOrderQuery 管理端订单筛选条件
type AdminOrderQuery struct {
	Status  string
	Method  string
	Keyword string
	UserID  int64
	Page    int
	Limit   int
}

// AdminListOrders 管理端按应用分页查询订单
func (s *PaymentService) AdminListOrders(ctx context.Context, appID int64, query AdminOrderQuery) (*paymentdomain.OrderListResult, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	items, total, err := s.pg.ListPaymentOrdersByApp(ctx, appID, query.Status, query.Method, query.Keyword, query.UserID, page, limit)
	if err != nil {
		return nil, err
	}
	return &paymentdomain.OrderListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPaymentTotalPages(total, limit),
	}, nil
}

// AdminOrderStats 管理端订单资金面板（交易概览首屏）。
func (s *PaymentService) AdminOrderStats(ctx context.Context, appID int64, start *time.Time, end *time.Time) (*paymentdomain.OrderStats, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.PaymentOrderStats(ctx, appID, start, end)
}

// AdminTransactionTrend 管理端交易趋势（实收 / 退款 / 钱包出入，粒度由跨度自动决定）。
func (s *PaymentService) AdminTransactionTrend(ctx context.Context, appID int64, start *time.Time, end *time.Time) (*paymentdomain.Trend, error) {
	return s.pg.PaymentTrend(ctx, appID, start, end)
}

// AdminOrderDetail 管理端订单详情：订单 + 履约状态 + 回调日志
func (s *PaymentService) AdminOrderDetail(ctx context.Context, appID int64, orderNo string) (map[string]any, error) {
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order == nil || order.AppID != appID {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	detail := map[string]any{"order": order}
	if status, fulfilledAt, err := s.pg.GetPaymentOrderFulfillment(ctx, order.ID); err == nil {
		detail["fulfillment_status"] = status
		detail["fulfilled_at"] = fulfilledAt
	}
	if logs, err := s.pg.ListPaymentCallbackLogsByOrder(ctx, appID, order.ID, 20); err == nil {
		detail["callback_logs"] = logs
	} else {
		s.log.Warn("list payment callback logs failed", zap.String("orderNo", orderNo), zap.Error(err))
	}
	return detail, nil
}

// ── 辅助函数 ──

func (s *PaymentService) requireApp(ctx context.Context, appID int64) (appNameHolder, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return appNameHolder{}, err
	}
	if app == nil {
		return appNameHolder{}, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return appNameHolder{Name: app.Name}, nil
}

func (s *PaymentService) startReceiptCleaner() {
	go func() {
		defer close(s.closed)
		s.cleanupExpiredReceipts()
		s.expireOverdueOrders()
		ticker := time.NewTicker(s.receiptCleanupInterval())
		defer ticker.Stop()
		// 过期订单清扫：pending 超过有效期 → expired，保证订单终态完整
		expireTicker := time.NewTicker(time.Minute)
		defer expireTicker.Stop()
		// 未结算退款单补偿：上游返回「受理中」或结算写入中断时，靠轮询收敛到终态
		refundTicker := time.NewTicker(2 * time.Minute)
		defer refundTicker.Stop()
		for {
			select {
			case <-s.closeCh:
				return
			case <-ticker.C:
				s.cleanupExpiredReceipts()
			case <-expireTicker.C:
				s.expireOverdueOrders()
			case <-refundTicker.C:
				s.syncPendingRefunds()
			}
		}
	}()
}

// expireOverdueOrders 批量关闭超时未支付订单
func (s *PaymentService) expireOverdueOrders() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	count, err := s.pg.ExpirePaymentOrders(ctx)
	if err != nil {
		s.log.Warn("expire overdue payment orders failed", zap.Error(err))
		return
	}
	if count > 0 {
		s.log.Info("expired overdue payment orders", zap.Int64("count", count))
	}
}

func randomDigits(length int) string {
	const digits = "0123456789"
	buf := make([]byte, length)
	for i := range buf {
		var b [1]byte
		_, _ = rand.Read(b[:])
		buf[i] = digits[int(b[0])%len(digits)]
	}
	return string(buf)
}

// decodeProviderConfig 通用配置解码辅助
func decodeProviderConfig[T any](data map[string]any) (*T, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, apperrors.New(40077, http.StatusBadRequest, "支付配置序列化失败")
	}
	var cfg T
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, apperrors.New(40077, http.StatusBadRequest, "支付配置解析失败")
	}
	return &cfg, nil
}

func calcPaymentTotalPages(total int64, limit int) int {
	if limit <= 0 {
		return 0
	}
	pages := int(total / int64(limit))
	if total%int64(limit) != 0 {
		pages++
	}
	if pages == 0 {
		return 1
	}
	return pages
}
