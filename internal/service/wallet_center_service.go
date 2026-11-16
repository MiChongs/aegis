package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	walletdomain "aegis/internal/domain/wallet"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"github.com/shopspring/decimal"
)

// 钱包中心错误码（40140–40152，见 docs/wallet.md）
const (
	errCodeWalletRechargeClosed   = 40140
	errCodeWalletRechargeRange    = 40141
	errCodeWalletPackageOnly      = 40142
	errCodeWalletPackageNotFound  = 40143
	errCodeWalletPackagePrice     = 40144
	errCodeWalletExchangeClosed   = 40145
	errCodeWalletExchangeRange    = 40146
	errCodeWalletExchangeDaily    = 40147
	errCodeWalletCashInsufficient = 40148
	errCodeWalletBonusInsuffic    = 40149
	errCodeWalletExchangeNoPoints = 40151
	errCodeWalletPackageLimit     = 40152
)

// walletSettingsKey 钱包设置在应用 settings 里的键
const walletSettingsKey = "wallet"

// SetAppService 注入应用服务（读写应用设置，bootstrap 中调用）。
func (s *WalletService) SetAppService(apps *AppService) { s.apps = apps }

// resolveWalletSettings 从应用 settings 读钱包设置，缺的项用默认值补齐。
func resolveWalletSettings(app *appdomain.App) walletdomain.Settings {
	out := walletdomain.DefaultSettings()
	if app == nil {
		return out
	}
	raw := lookupMap(app.Settings, walletSettingsKey)
	if raw == nil {
		return out
	}
	if v, ok := raw["rechargeEnabled"].(bool); ok {
		out.RechargeEnabled = v
	}
	if v, ok := raw["customAmountEnabled"].(bool); ok {
		out.CustomAmountEnabled = v
	}
	if v, ok := raw["exchangeEnabled"].(bool); ok {
		out.ExchangeEnabled = v
	}
	readDecimal := func(key string, dst *decimal.Decimal) {
		switch v := raw[key].(type) {
		case string:
			if d, err := decimal.NewFromString(strings.TrimSpace(v)); err == nil {
				*dst = d
			}
		case float64:
			*dst = decimal.NewFromFloat(v).Round(2)
		}
	}
	readDecimal("rechargeMin", &out.RechargeMin)
	readDecimal("rechargeMax", &out.RechargeMax)
	readDecimal("exchangeMin", &out.ExchangeMin)
	readDecimal("exchangeMax", &out.ExchangeMax)
	readDecimal("exchangeDailyMax", &out.ExchangeDailyMax)
	if v, ok := raw["notice"].(string); ok {
		out.Notice = v
	}
	return out
}

func (s *WalletService) loadApp(ctx context.Context, appID int64) (*appdomain.App, error) {
	if s.apps != nil {
		return s.apps.GetApp(ctx, appID)
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return app, nil
}

// walletConfig 一次读出钱包设置、兑换比例与币种。
func (s *WalletService) walletConfig(ctx context.Context, appID int64) (walletdomain.Settings, appdomain.CommerceSettings, error) {
	app, err := s.loadApp(ctx, appID)
	if err != nil {
		return walletdomain.Settings{}, appdomain.CommerceSettings{}, err
	}
	return resolveWalletSettings(app), resolveCommerceSettings(app), nil
}

// walletDayStart 「今天」的起点（平台默认时区），每日兑换上限按它算。
func walletDayStart(now time.Time) time.Time {
	loc := timeutil.DefaultLocation()
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// ── 用户侧 ──

// Overview 钱包页一次取齐：钱包（含本金 / 赠送拆分）、币种、充值选项与档位、兑换额度、积分、说明。
func (s *WalletService) Overview(ctx context.Context, session *authdomain.Session) (*walletdomain.Overview, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	settings, commerce, err := s.walletConfig(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	w, err := s.GetMyWallet(ctx, session)
	if err != nil {
		return nil, err
	}
	used, err := s.pg.WalletExchangedSince(ctx, session.UserID, session.AppID, walletDayStart(time.Now()))
	if err != nil {
		return nil, err
	}
	integral, err := s.pg.GetUserIntegralBalance(ctx, session.UserID, session.AppID)
	if err != nil && !errors.Is(err, pgrepo.ErrUserNotFound) {
		return nil, err
	}
	packages, err := s.userPackages(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	return &walletdomain.Overview{
		Wallet:   *w,
		Currency: commerce.WalletCurrency,
		Recharge: walletdomain.RechargeOptions{
			Enabled:             settings.RechargeEnabled,
			CustomAmountEnabled: settings.CustomAmountEnabled,
			Min:                 settings.RechargeMin,
			Max:                 settings.RechargeMax,
			Packages:            packages,
		},
		Exchange:        walletdomain.BuildExchangeQuota(settings, int64(commerce.IntegralPerCurrency), w.CashBalance, used),
		IntegralBalance: integral,
		Notice:          settings.Notice,
	}, nil
}

// userPackages 用户可见的档位：上架、未过期（未开始的也列，标明不可买）；带已买次数与可买状态。
func (s *WalletService) userPackages(ctx context.Context, appID, userID int64) ([]walletdomain.PackageView, error) {
	items, err := s.pg.ListWalletPackages(ctx, appID, true)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []walletdomain.PackageView{}, nil
	}
	counts, err := s.pg.WalletPackagePurchaseCounts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]walletdomain.PackageView, 0, len(items))
	for _, p := range items {
		reason := p.Availability(now, counts[p.ID])
		if reason == walletdomain.PackageEnded {
			continue
		}
		out = append(out, walletdomain.PackageView{RechargePackage: p, Purchased: counts[p.ID], Unavailable: reason})
	}
	return out, nil
}

// ExchangeRequest 用户发起的兑换。
type WalletExchangeRequest struct {
	Amount         string
	All            bool
	IdempotencyKey string
	ClientIP       string
	UserAgent      string
}

// Exchange 充值余额兑换积分。比例服务端定（与积分直购同一比例），客户端只说兑多少本金或「全部」。
func (s *WalletService) Exchange(ctx context.Context, session *authdomain.Session, req WalletExchangeRequest) (*walletdomain.ExchangeResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		return nil, apperrors.New(40082, http.StatusBadRequest, "必须提供幂等键 idempotencyKey，防止重复兑换")
	}
	if len(key) > 64 {
		return nil, apperrors.New(40082, http.StatusBadRequest, "幂等键最长 64 个字符")
	}
	settings, commerce, err := s.walletConfig(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.ExchangeEnabled {
		return nil, apperrors.New(errCodeWalletExchangeClosed, http.StatusForbidden, "余额兑换积分暂未开放")
	}
	amount := decimal.Zero
	if !req.All {
		amount, err = decimal.NewFromString(strings.TrimSpace(req.Amount))
		if err != nil || !amount.IsPositive() {
			return nil, apperrors.New(40080, http.StatusBadRequest, "兑换金额必须大于 0")
		}
		if !amount.Equal(amount.Round(2)) {
			return nil, apperrors.New(40086, http.StatusBadRequest, "兑换金额最多保留两位小数")
		}
	}
	result, err := s.pg.ExchangeWalletToIntegral(ctx, walletdomain.ExchangeInput{
		UserID: session.UserID, AppID: session.AppID,
		Amount: amount, All: req.All,
		Rate:     int64(commerce.IntegralPerCurrency),
		Settings: settings, DayStart: walletDayStart(time.Now()),
		IdempotencyKey: "wex:" + walletIdemKey(session.AppID, session.UserID, key),
		ClientIP:       req.ClientIP, UserAgent: req.UserAgent,
	})
	if err != nil {
		return nil, s.translateExchangeError(err, settings)
	}
	return result, nil
}

func (s *WalletService) translateExchangeError(err error, settings walletdomain.Settings) error {
	switch {
	case errors.Is(err, walletdomain.ErrExchangeTooSmall):
		return apperrors.New(errCodeWalletExchangeRange, http.StatusBadRequest,
			"单笔至少兑换 "+settings.ExchangeMin.StringFixed(2))
	case errors.Is(err, walletdomain.ErrExchangeTooLarge):
		return apperrors.New(errCodeWalletExchangeRange, http.StatusBadRequest,
			"单笔最多兑换 "+settings.ExchangeMax.StringFixed(2))
	case errors.Is(err, walletdomain.ErrExchangeDailyLimit):
		return apperrors.New(errCodeWalletExchangeDaily, http.StatusBadRequest,
			"超出今日兑换额度（每日 "+settings.ExchangeDailyMax.StringFixed(2)+"）")
	case errors.Is(err, walletdomain.ErrExchangeNoPoints):
		return apperrors.New(errCodeWalletExchangeNoPoints, http.StatusBadRequest, "兑换金额过小，不足 1 积分")
	case errors.Is(err, pgrepo.ErrIdempotencyConflict):
		return apperrors.New(40921, http.StatusConflict, "幂等键已被其他操作使用")
	}
	_, err = s.translateWalletError(nil, err)
	return err
}

// ── 管理端：设置 ──

// AdminGetSettings 读取应用钱包设置（附带兑换比例与币种，便于控制台一屏看全）。
func (s *WalletService) AdminGetSettings(ctx context.Context, appID int64) (*WalletSettingsView, error) {
	settings, commerce, err := s.walletConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	return &WalletSettingsView{Settings: settings, Rate: commerce.IntegralPerCurrency, Currency: commerce.WalletCurrency}, nil
}

// WalletSettingsView 控制台钱包设置。Rate、Currency 只读，在「交易设置」里改。
type WalletSettingsView struct {
	walletdomain.Settings
	Rate     int    `json:"rate"`
	Currency string `json:"currency"`
}

// AdminUpdateSettings 保存钱包设置。
func (s *WalletService) AdminUpdateSettings(ctx context.Context, appID int64, input walletdomain.Settings) (*WalletSettingsView, error) {
	if s.apps == nil {
		return nil, errors.New("wallet: app service not wired")
	}
	if err := input.Validate(); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	app, err := s.apps.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings[walletSettingsKey] = map[string]any{
		"rechargeEnabled":     input.RechargeEnabled,
		"customAmountEnabled": input.CustomAmountEnabled,
		"rechargeMin":         input.RechargeMin.StringFixed(2),
		"rechargeMax":         input.RechargeMax.StringFixed(2),
		"exchangeEnabled":     input.ExchangeEnabled,
		"exchangeMin":         input.ExchangeMin.StringFixed(2),
		"exchangeMax":         input.ExchangeMax.StringFixed(2),
		"exchangeDailyMax":    input.ExchangeDailyMax.StringFixed(2),
		"notice":              input.Notice,
	}
	if _, err := s.apps.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	commerce := resolveCommerceSettings(app)
	return &WalletSettingsView{Settings: input, Rate: commerce.IntegralPerCurrency, Currency: commerce.WalletCurrency}, nil
}

// ── 管理端：充值档位 ──

// AdminListPackages 全部档位（含下架）+ 销售统计。
func (s *WalletService) AdminListPackages(ctx context.Context, appID int64) ([]walletdomain.AdminPackage, error) {
	items, err := s.pg.ListWalletPackages(ctx, appID, false)
	if err != nil {
		return nil, err
	}
	stats, err := s.pg.WalletPackageStats(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]walletdomain.AdminPackage, 0, len(items))
	for _, p := range items {
		stat := stats[p.ID]
		if stat.Amount.IsZero() {
			stat.Amount = decimal.Zero
		}
		out = append(out, walletdomain.AdminPackage{RechargePackage: p, Stat: stat})
	}
	return out, nil
}

// AdminSavePackage 新建或更新档位。
func (s *WalletService) AdminSavePackage(ctx context.Context, appID int64, p walletdomain.RechargePackage) (*walletdomain.RechargePackage, error) {
	p.AppID = appID
	if err := p.Validate(); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	saved, err := s.pg.SaveWalletPackage(ctx, p)
	if errors.Is(err, pgrepo.ErrWalletPackageNotFound) {
		return nil, apperrors.New(errCodeWalletPackageNotFound, http.StatusNotFound, "充值档位不存在")
	}
	return saved, err
}

// AdminDeletePackage 删除档位。
func (s *WalletService) AdminDeletePackage(ctx context.Context, appID, id int64) error {
	err := s.pg.DeleteWalletPackage(ctx, appID, id)
	if errors.Is(err, pgrepo.ErrWalletPackageNotFound) {
		return apperrors.New(errCodeWalletPackageNotFound, http.StatusNotFound, "充值档位不存在")
	}
	return err
}

// AdminReorderPackages 调整档位顺序。
func (s *WalletService) AdminReorderPackages(ctx context.Context, appID int64, ids []int64) error {
	if len(ids) == 0 || len(ids) > 200 {
		return apperrors.New(40000, http.StatusBadRequest, "排序列表必须包含 1–200 个档位")
	}
	return s.pg.ReorderWalletPackages(ctx, appID, ids)
}

// ── 充值下单校验（PaymentService 调用）──

// rechargeSnapshot 充值订单下单时固化的档位快照。
type rechargeSnapshot struct {
	PackageID     int64
	PackageTitle  string
	Bonus         decimal.Decimal
	BonusIntegral int64
}

// validateRecharge 充值下单校验：开放与否、档位（在售、价格一致、限购）或自定义金额范围。
// packageID 为 0 表示自定义金额。
func (s *WalletService) validateRecharge(ctx context.Context, appID, userID int64, amount decimal.Decimal, packageID int64) (*rechargeSnapshot, error) {
	settings, _, err := s.walletConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !settings.RechargeEnabled {
		return nil, apperrors.New(errCodeWalletRechargeClosed, http.StatusForbidden, "余额充值暂未开放")
	}
	if packageID <= 0 {
		if !settings.CustomAmountEnabled {
			return nil, apperrors.New(errCodeWalletPackageOnly, http.StatusBadRequest, "请选择充值档位")
		}
		if amount.LessThan(settings.RechargeMin) || amount.GreaterThan(settings.RechargeMax) {
			return nil, apperrors.New(errCodeWalletRechargeRange, http.StatusBadRequest,
				"充值金额需在 "+settings.RechargeMin.StringFixed(2)+"–"+settings.RechargeMax.StringFixed(2)+" 之间")
		}
		return &rechargeSnapshot{Bonus: decimal.Zero}, nil
	}
	p, err := s.pg.GetWalletPackage(ctx, appID, packageID)
	if errors.Is(err, pgrepo.ErrWalletPackageNotFound) {
		return nil, apperrors.New(errCodeWalletPackageNotFound, http.StatusNotFound, "充值档位不存在或已下架")
	}
	if err != nil {
		return nil, err
	}
	counts, err := s.pg.WalletPackagePurchaseCounts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	switch p.Availability(time.Now(), counts[p.ID]) {
	case walletdomain.PackageAvailable:
	case walletdomain.PackageLimitReached:
		return nil, apperrors.New(errCodeWalletPackageLimit, http.StatusConflict, "该档位每人限购 "+strconv.Itoa(p.PerUserLimit)+" 次，已达上限")
	default:
		return nil, apperrors.New(errCodeWalletPackageNotFound, http.StatusNotFound, "充值档位不存在或已下架")
	}
	if !p.Amount.Equal(amount) {
		return nil, apperrors.New(errCodeWalletPackagePrice, http.StatusBadRequest, "支付金额与充值档位价格不一致")
	}
	return &rechargeSnapshot{PackageID: p.ID, PackageTitle: p.Title, Bonus: p.BonusAmount, BonusIntegral: p.BonusIntegral}, nil
}
