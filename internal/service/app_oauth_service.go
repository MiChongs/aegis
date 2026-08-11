package service

import (
	"aegis/pkg/egress"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"aegis/internal/config"
	oauthdomain "aegis/internal/domain/oauth"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

const (
	oauthScopeMaxCount   = 32
	oauthScopeMaxLen     = 64
	oauthParamMaxCount   = 16
	oauthParamMaxLen     = 256
	oauthProbeTimeout    = 5 * time.Second
	oauthMaxProvidersPer = 32
)

var oauthSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

// AppOAuthService 应用级第三方登录渠道的配置中心。
//
// 职责边界：
//   - 渠道配置的增删改查、密钥加解密、连通性自检；
//   - 向 AuthService 提供运行期解析（Resolve），含平台级 .env 兜底；
//   - 绑定关系的查询与解绑（登录/注册链路本身仍在 AuthService）。
//
// 密钥安全：client_secret 以 AES-GCM 密文落库，密钥派生自 SECURITY_MASTER_KEY；
// 任何出网响应都只暴露 ClientSecretSet / ClientSecretHint，不回传明文。
type AppOAuthService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	key        []byte
	platform   map[string]config.OAuthProviderConfig
	httpClient *http.Client
}

func NewAppOAuthService(log *zap.Logger, pg *pgrepo.Repository, cfg config.Config) *AppOAuthService {
	if log == nil {
		log = zap.NewNop()
	}
	// 与 AuthProtocolService 一致的派生方式：主密钥不同用途各自派生，互不复用
	digest := sha256.Sum256([]byte("aegis.app-oauth.master\x00" + cfg.Security.MasterKey))
	platform := make(map[string]config.OAuthProviderConfig, len(cfg.OAuth))
	for name, item := range cfg.OAuth {
		if strings.TrimSpace(item.ClientID) == "" {
			continue
		}
		if item.Kind == "" {
			item.Kind = name
		}
		platform[name] = item
	}
	return &AppOAuthService{
		log:        log,
		pg:         pg,
		key:        digest[:],
		platform:   platform,
		httpClient: egress.NewClient(egress.Profile{Name: "oauth.probe", Timeout: oauthProbeTimeout}),
	}
}

// Templates 返回内置渠道模板目录（静态数据，无 IO）。
func (s *AppOAuthService) Templates() []oauthdomain.Template {
	result := make([]oauthdomain.Template, len(oauthTemplates))
	copy(result, oauthTemplates)
	return result
}

// List 管理端列表：应用级配置在前，平台级兜底渠道追加在后（Source=platform）。
// 平台级条目 ID 为 0，管理端保存后即转为应用级覆盖。
func (s *AppOAuthService) List(ctx context.Context, appID int64) ([]oauthdomain.Provider, error) {
	items, err := s.pg.ListAppOAuthProviders(ctx, appID)
	if err != nil {
		return nil, err
	}
	counts, err := s.pg.CountAppOAuthBindingsByProvider(ctx, appID)
	if err != nil {
		// 统计失败不应阻塞配置读取
		s.log.Warn("count oauth bindings failed", zap.Int64("appid", appID), zap.Error(err))
		counts = map[string]int64{}
	}
	configured := make(map[string]bool, len(items))
	result := make([]oauthdomain.Provider, 0, len(items)+len(s.platform))
	for index := range items {
		item := items[index]
		configured[item.Provider] = true
		s.decorate(&item, counts[item.Provider])
		result = append(result, item)
	}
	for _, name := range sortedPlatformNames(s.platform) {
		if configured[name] {
			continue
		}
		item := s.platformProvider(appID, name, len(result))
		s.decorate(&item, counts[name])
		result = append(result, item)
	}
	return result, nil
}

// Get 读取单个渠道；应用级不存在时回落平台级，均不存在返回 404。
func (s *AppOAuthService) Get(ctx context.Context, appID int64, provider string) (*oauthdomain.Provider, error) {
	slug := normalizeOAuthSlug(provider)
	item, err := s.pg.GetAppOAuthProvider(ctx, appID, slug)
	if err != nil {
		return nil, err
	}
	if item == nil {
		platform, ok := s.platform[slug]
		if !ok {
			return nil, apperrors.New(40490, http.StatusNotFound, "第三方登录渠道不存在")
		}
		fallback := s.platformProvider(appID, platform.Name, 0)
		item = &fallback
	}
	counts, err := s.pg.CountAppOAuthBindingsByProvider(ctx, appID)
	if err != nil {
		counts = map[string]int64{}
	}
	s.decorate(item, counts[item.Provider])
	return item, nil
}

// Save 新建或整体更新一个渠道配置。
//
// 约定：
//   - ClientSecret 留空 = 保持原密钥不变（编辑表单不回填密钥）；
//   - ClearClientSecret=true 显式清空；
//   - Enabled=true 时校验配置完整性，缺项会连同具体缺什么一并返回，避免"保存成功但登录报错"。
func (s *AppOAuthService) Save(ctx context.Context, appID int64, input oauthdomain.SaveInput) (*oauthdomain.Provider, error) {
	slug := normalizeOAuthSlug(input.Provider)
	if !oauthSlugPattern.MatchString(slug) {
		return nil, apperrors.New(40090, http.StatusBadRequest,
			"渠道标识只能包含小写字母、数字、连字符和下划线，长度 2-32")
	}
	existing, err := s.pg.GetAppOAuthProvider(ctx, appID, slug)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		current, err := s.pg.ListAppOAuthProviders(ctx, appID)
		if err != nil {
			return nil, err
		}
		if len(current) >= oauthMaxProvidersPer {
			return nil, apperrors.New(40091, http.StatusBadRequest,
				fmt.Sprintf("单个应用最多配置 %d 个第三方登录渠道", oauthMaxProvidersPer))
		}
	}

	record, err := s.buildRecord(appID, slug, input, existing)
	if err != nil {
		return nil, err
	}
	if record.Enabled {
		if issues := configIssues(record); len(issues) > 0 {
			return nil, apperrors.New(40092, http.StatusBadRequest,
				"渠道配置不完整，无法启用："+strings.Join(issues, "；"))
		}
	}
	saved, err := s.pg.UpsertAppOAuthProvider(ctx, *record)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, apperrors.New(50090, http.StatusInternalServerError, "第三方登录渠道保存失败")
	}
	counts, err := s.pg.CountAppOAuthBindingsByProvider(ctx, appID)
	if err != nil {
		counts = map[string]int64{}
	}
	s.decorate(saved, counts[saved.Provider])
	return saved, nil
}

// SetEnabled 单独切换启用状态（列表页一键开关）。
// 启用前同样做完整性校验；渠道尚未落库时先由平台级模板物化成应用级记录。
func (s *AppOAuthService) SetEnabled(ctx context.Context, appID int64, provider string, enabled bool) (*oauthdomain.Provider, error) {
	slug := normalizeOAuthSlug(provider)
	existing, err := s.pg.GetAppOAuthProvider(ctx, appID, slug)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		platform, ok := s.platform[slug]
		if !ok {
			return nil, apperrors.New(40490, http.StatusNotFound, "第三方登录渠道不存在")
		}
		materialized := s.platformProvider(appID, platform.Name, 0)
		materialized.ClientSecret, err = encryptSecret(s.key, platform.ClientSecret)
		if err != nil {
			return nil, err
		}
		materialized.Enabled = enabled
		if enabled {
			if issues := configIssues(&materialized); len(issues) > 0 {
				return nil, apperrors.New(40092, http.StatusBadRequest,
					"渠道配置不完整，无法启用："+strings.Join(issues, "；"))
			}
		}
		saved, err := s.pg.UpsertAppOAuthProvider(ctx, materialized)
		if err != nil {
			return nil, err
		}
		s.decorate(saved, 0)
		return saved, nil
	}
	if enabled {
		if issues := configIssues(existing); len(issues) > 0 {
			return nil, apperrors.New(40092, http.StatusBadRequest,
				"渠道配置不完整，无法启用："+strings.Join(issues, "；"))
		}
	}
	if _, err := s.pg.SetAppOAuthProviderEnabled(ctx, appID, slug, enabled); err != nil {
		return nil, err
	}
	return s.Get(ctx, appID, slug)
}

// Delete 删除应用级渠道配置。已产生的用户绑定保留，重新配置同名渠道后可继续使用。
func (s *AppOAuthService) Delete(ctx context.Context, appID int64, provider string) error {
	slug := normalizeOAuthSlug(provider)
	affected, err := s.pg.DeleteAppOAuthProvider(ctx, appID, slug)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40490, http.StatusNotFound, "第三方登录渠道不存在")
	}
	return nil
}

// Reorder 调整登录页展示顺序。
func (s *AppOAuthService) Reorder(ctx context.Context, appID int64, providers []string) ([]oauthdomain.Provider, error) {
	normalized := make([]string, 0, len(providers))
	seen := make(map[string]bool, len(providers))
	for _, item := range providers {
		slug := normalizeOAuthSlug(item)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		normalized = append(normalized, slug)
	}
	if len(normalized) == 0 {
		return nil, apperrors.New(40093, http.StatusBadRequest, "排序列表不能为空")
	}
	if err := s.pg.ReorderAppOAuthProviders(ctx, appID, normalized); err != nil {
		return nil, err
	}
	return s.List(ctx, appID)
}

// PublicProviders 登录页可见的渠道（已启用且配置完整），不含任何凭据。
func (s *AppOAuthService) PublicProviders(ctx context.Context, appID int64) ([]oauthdomain.PublicProvider, error) {
	// 一次取全量（含停用）：既要挑出启用项，也要知道哪些平台渠道已被应用级停用
	items, err := s.pg.ListAppOAuthProviders(ctx, appID)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]bool, len(items))
	result := make([]oauthdomain.PublicProvider, 0, len(items)+len(s.platform))
	for index := range items {
		item := items[index]
		configured[item.Provider] = true
		if !item.Enabled || len(configIssues(&item)) > 0 {
			continue
		}
		result = append(result, publicView(&item))
	}
	// 未在应用级登记的平台渠道保持既有可用性（升级前已经能登录，不因新表而消失）；
	// 一旦应用级建了同名记录（哪怕是停用状态），就以应用级为准，不再回落。
	for _, name := range sortedPlatformNames(s.platform) {
		if configured[name] {
			continue
		}
		item := s.platformProvider(appID, name, len(result))
		if len(configIssues(&item)) > 0 {
			continue
		}
		result = append(result, publicView(&item))
	}
	return result, nil
}

// Resolve 运行期解析：AuthService 在授权/回调链路上调用。
//
// 顺序：应用级（必须 enabled）→ 平台级 .env 兜底。
// 两者都没有时返回"未开启该渠道"，而不是笼统的"不支持的提供商"。
func (s *AppOAuthService) Resolve(ctx context.Context, appID int64, provider string) (*oauthdomain.Resolved, error) {
	slug := normalizeOAuthSlug(provider)
	if slug == "" {
		return nil, apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	item, err := s.pg.GetAppOAuthProvider(ctx, appID, slug)
	if err != nil {
		return nil, err
	}
	if item != nil {
		if !item.Enabled {
			return nil, apperrors.New(40390, http.StatusForbidden, "当前应用未开启该第三方登录渠道")
		}
		secret, err := s.revealSecret(item.ClientSecret)
		if err != nil {
			s.log.Error("decrypt oauth client secret failed",
				zap.Int64("appid", appID), zap.String("provider", slug), zap.Error(err))
			return nil, apperrors.New(50091, http.StatusInternalServerError, "第三方登录渠道密钥解密失败")
		}
		return &oauthdomain.Resolved{
			Provider: item.Provider, Kind: item.Kind, DisplayName: item.DisplayName,
			Source:     oauthdomain.SourceApp,
			AllowLogin: item.AllowLogin, AllowRegister: item.AllowRegister, AllowBind: item.AllowBind,
			ClientID: item.ClientID, ClientSecret: secret, RedirectURL: item.RedirectURL,
			AuthURL: item.AuthURL, TokenURL: item.TokenURL, UserInfoURL: item.UserInfoURL,
			Scopes: item.Scopes, TokenAuthStyle: item.TokenAuthStyle,
			UserInfoAuthStyle: item.UserInfoAuthStyle,
			ProfileMapping:    item.ProfileMapping, ExtraAuthParams: item.ExtraAuthParams,
		}, nil
	}
	platform, ok := s.platform[slug]
	if !ok {
		return nil, apperrors.New(40390, http.StatusForbidden, "当前应用未开启该第三方登录渠道")
	}
	return &oauthdomain.Resolved{
		Provider: platform.Name, Kind: platform.Kind, DisplayName: platform.Name,
		Source:     oauthdomain.SourcePlatform,
		AllowLogin: true, AllowRegister: true, AllowBind: true,
		ClientID: platform.ClientID, ClientSecret: platform.ClientSecret,
		RedirectURL: platform.RedirectURL, AuthURL: platform.AuthURL,
		TokenURL: platform.TokenURL, UserInfoURL: platform.UserInfoURL,
		Scopes: platform.Scopes, TokenAuthStyle: platform.TokenAuthStyle,
		UserInfoAuthStyle: platform.UserInfoAuthStyle,
		ProfileMapping:    platform.ProfileMapping, ExtraAuthParams: platform.ExtraAuthParams,
	}, nil
}

// ProviderConfig 把解析结果转成适配器可用的配置。
func ProviderConfig(resolved *oauthdomain.Resolved) config.OAuthProviderConfig {
	return config.OAuthProviderConfig{
		Name: resolved.Provider, Kind: resolved.Kind,
		ClientID: resolved.ClientID, ClientSecret: resolved.ClientSecret,
		RedirectURL: resolved.RedirectURL, AuthURL: resolved.AuthURL,
		TokenURL: resolved.TokenURL, UserInfoURL: resolved.UserInfoURL,
		Scopes: resolved.Scopes, TokenAuthStyle: resolved.TokenAuthStyle,
		UserInfoAuthStyle: resolved.UserInfoAuthStyle,
		ProfileMapping:    resolved.ProfileMapping, ExtraAuthParams: resolved.ExtraAuthParams,
	}
}

// Test 渠道自检：先做配置完整性校验，再并发探测三个端点的可达性，
// 并给出一条可直接点击验证的示例授权链接。
func (s *AppOAuthService) Test(ctx context.Context, appID int64, provider string) (*oauthdomain.TestResult, error) {
	item, err := s.Get(ctx, appID, provider)
	if err != nil {
		return nil, err
	}
	result := &oauthdomain.TestResult{
		Provider:  item.Provider,
		Ready:     item.Ready,
		Issues:    item.Issues,
		Warnings:  item.Warnings,
		CheckedAt: timeutil.NowUTC(),
		Endpoints: []oauthdomain.Endpoint{},
	}
	targets := []struct {
		name string
		url  string
	}{
		{"授权端点", item.AuthURL},
		{"令牌端点", item.TokenURL},
		{"用户信息端点", item.UserInfoURL},
	}
	for _, target := range targets {
		if strings.TrimSpace(target.url) == "" {
			continue
		}
		result.Endpoints = append(result.Endpoints, s.probeEndpoint(ctx, target.name, target.url))
	}
	if item.Ready {
		resolved := &oauthdomain.Resolved{
			Provider: item.Provider, Kind: item.Kind, ClientID: item.ClientID,
			RedirectURL: item.RedirectURL, AuthURL: item.AuthURL, Scopes: item.Scopes,
			ExtraAuthParams: item.ExtraAuthParams,
		}
		result.AuthorizeURL = NewOAuthProvider(ProviderConfig(resolved)).AuthURL("aegis-config-test")
	}
	return result, nil
}

// probeEndpoint 只判断"能否建立 HTTP 会话"：多数端点缺参数会返回 4xx，
// 那依然说明域名解析、网络与 TLS 是通的，因此 4xx 也计为可达。
func (s *AppOAuthService) probeEndpoint(ctx context.Context, name, target string) oauthdomain.Endpoint {
	endpoint := oauthdomain.Endpoint{Name: name, URL: target}
	probeCtx, cancel := context.WithTimeout(ctx, oauthProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target, nil)
	if err != nil {
		endpoint.Message = "地址格式无效"
		return endpoint
	}
	request.Header.Set("Accept", "application/json")
	start := time.Now()
	response, err := s.httpClient.Do(request)
	endpoint.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		endpoint.Message = "无法连接：" + shortProbeErr(err)
		return endpoint
	}
	defer response.Body.Close()
	endpoint.Reachable = true
	endpoint.Status = response.StatusCode
	if response.StatusCode >= 500 {
		endpoint.Message = fmt.Sprintf("服务端返回 HTTP %d，稍后重试", response.StatusCode)
	}
	return endpoint
}

// ListBindings 管理端分页查询绑定记录，并补齐渠道展示名与图标。
func (s *AppOAuthService) ListBindings(ctx context.Context, query oauthdomain.BindingQuery) (*oauthdomain.BindingPage, error) {
	page, err := s.pg.ListOAuthBindings(ctx, query)
	if err != nil {
		return nil, err
	}
	s.attachProviderMeta(ctx, query.AppID, page.Items)
	return page, nil
}

// ListUserBindings 单个用户的绑定明细。
func (s *AppOAuthService) ListUserBindings(ctx context.Context, appID, userID int64) ([]oauthdomain.Binding, error) {
	items, err := s.pg.ListUserOAuthBindings(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	s.attachProviderMeta(ctx, appID, items)
	return items, nil
}

// Unbind 解绑第三方账号。
//
// 安全约束：解除最后一个可用登录方式会让账号彻底登不进来，
// 因此当账号没有密码且这是最后一个绑定时拒绝，并提示先设置密码。
func (s *AppOAuthService) Unbind(ctx context.Context, appID, userID int64, provider string, force bool) error {
	slug := normalizeOAuthSlug(provider)
	if slug == "" {
		return apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	if !force {
		total, err := s.pg.CountUserOAuthBindings(ctx, appID, userID)
		if err != nil {
			return err
		}
		if total <= 1 {
			hasPassword, err := s.pg.UserHasPassword(ctx, userID)
			if err != nil {
				return err
			}
			if !hasPassword {
				return apperrors.New(40094, http.StatusBadRequest,
					"该账号没有设置密码，解绑最后一个第三方登录后将无法登录，请先设置密码")
			}
		}
	}
	affected, err := s.pg.DeleteOAuthBinding(ctx, appID, userID, slug)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40491, http.StatusNotFound, "未找到该第三方账号绑定")
	}
	return nil
}

// ── 内部辅助 ──

// buildRecord 把入参归一化成待落库记录，含密钥处理与模板兜底。
func (s *AppOAuthService) buildRecord(appID int64, slug string, input oauthdomain.SaveInput, existing *oauthdomain.Provider) (*oauthdomain.Provider, error) {
	template, hasTemplate := oauthTemplateIndex[slug]
	kind := normalizeOAuthKind(input.Kind)
	if kind == "" {
		switch {
		case existing != nil:
			kind = existing.Kind
		case hasTemplate:
			kind = template.Kind
		default:
			kind = oauthdomain.KindGeneric
		}
	}

	record := &oauthdomain.Provider{
		AppID: appID, Provider: slug, Kind: kind,
		DisplayName: strings.TrimSpace(input.DisplayName),
		Icon:        strings.TrimSpace(input.Icon),
		Color:       strings.TrimSpace(input.Color),
		Enabled:     input.Enabled,
		ClientID:    strings.TrimSpace(input.ClientID),
		RedirectURL: strings.TrimSpace(input.RedirectURL),
		AuthURL:     strings.TrimSpace(input.AuthURL),
		TokenURL:    strings.TrimSpace(input.TokenURL),
		UserInfoURL: strings.TrimSpace(input.UserInfoURL),
		Remark:      truncateRunes(strings.TrimSpace(input.Remark), 255),
	}
	if record.DisplayName == "" {
		if hasTemplate {
			record.DisplayName = template.Name
		} else {
			record.DisplayName = slug
		}
	}
	if len([]rune(record.DisplayName)) > 64 {
		return nil, apperrors.New(40095, http.StatusBadRequest, "渠道展示名称不能超过 64 个字符")
	}
	if record.Icon == "" && hasTemplate {
		record.Icon = template.Icon
	}
	if record.Color == "" && hasTemplate {
		record.Color = template.Color
	}
	// 端点留空时按模板兜底，让"只填 ClientID/Secret"成为最常见路径
	if record.AuthURL == "" && hasTemplate {
		record.AuthURL = template.AuthURL
	}
	if record.TokenURL == "" && hasTemplate {
		record.TokenURL = template.TokenURL
	}
	if record.UserInfoURL == "" && hasTemplate {
		record.UserInfoURL = template.UserInfoURL
	}
	for label, target := range map[string]string{
		"授权端点": record.AuthURL, "令牌端点": record.TokenURL,
		"用户信息端点": record.UserInfoURL, "回调地址": record.RedirectURL,
	} {
		if err := validateOAuthURL(label, target); err != nil {
			return nil, err
		}
	}

	scopes, err := normalizeOAuthScopes(input.Scopes)
	if err != nil {
		return nil, err
	}
	if scopes == nil && hasTemplate && existing == nil {
		scopes = append([]string(nil), template.Scopes...)
	}
	if scopes == nil {
		scopes = []string{}
	}
	record.Scopes = scopes

	record.TokenAuthStyle = normalizeTokenAuthStyle(input.TokenAuthStyle)
	if input.TokenAuthStyle == "" {
		switch {
		case existing != nil:
			record.TokenAuthStyle = normalizeTokenAuthStyle(existing.TokenAuthStyle)
		case hasTemplate && template.TokenAuthStyle != "":
			record.TokenAuthStyle = normalizeTokenAuthStyle(template.TokenAuthStyle)
		}
	}
	record.UserInfoAuthStyle = normalizeUserInfoAuthStyle(input.UserInfoAuthStyle)
	if input.UserInfoAuthStyle == "" {
		switch {
		case existing != nil:
			record.UserInfoAuthStyle = normalizeUserInfoAuthStyle(existing.UserInfoAuthStyle)
		case hasTemplate && template.UserInfoAuthStyle != "":
			record.UserInfoAuthStyle = normalizeUserInfoAuthStyle(template.UserInfoAuthStyle)
		}
	}

	mapping, err := normalizeOAuthPairs(input.ProfileMapping, allowedMappingKeys)
	if err != nil {
		return nil, err
	}
	record.ProfileMapping = mapping
	params, err := normalizeOAuthPairs(input.ExtraAuthParams, nil)
	if err != nil {
		return nil, err
	}
	record.ExtraAuthParams = params

	record.AllowLogin = pickBool(input.AllowLogin, existing != nil && existing.AllowLogin, existing == nil)
	record.AllowRegister = pickBool(input.AllowRegister, existing != nil && existing.AllowRegister, existing == nil)
	record.AllowBind = pickBool(input.AllowBind, existing != nil && existing.AllowBind, existing == nil)
	if !record.AllowLogin && !record.AllowBind {
		return nil, apperrors.New(40096, http.StatusBadRequest, "登录与绑定至少需要开启一项")
	}

	switch {
	case input.SortOrder != nil:
		record.SortOrder = *input.SortOrder
	case existing != nil:
		record.SortOrder = existing.SortOrder
	}

	// 密钥：显式清空 > 传入新值 > 沿用旧密文
	switch {
	case input.ClearClientSecret:
		record.ClientSecret = ""
	case strings.TrimSpace(input.ClientSecret) != "":
		cipher, err := encryptSecret(s.key, strings.TrimSpace(input.ClientSecret))
		if err != nil {
			return nil, err
		}
		record.ClientSecret = cipher
	case existing != nil:
		record.ClientSecret = existing.ClientSecret
	default:
		// 首次创建且未填密钥时，若平台级已有同名渠道则继承其密钥，降低迁移成本
		if platform, ok := s.platform[slug]; ok && platform.ClientSecret != "" {
			cipher, err := encryptSecret(s.key, platform.ClientSecret)
			if err != nil {
				return nil, err
			}
			record.ClientSecret = cipher
			if record.ClientID == "" {
				record.ClientID = platform.ClientID
			}
		}
	}
	return record, nil
}

// decorate 补齐出网响应用的派生字段，并抹掉密钥明文/密文。
func (s *AppOAuthService) decorate(item *oauthdomain.Provider, bindings int64) {
	if item == nil {
		return
	}
	item.Bindings = bindings
	item.Issues = configIssues(item)
	item.Ready = len(item.Issues) == 0
	item.Warnings = configWarnings(item)
	if item.Source == "" {
		item.Source = oauthdomain.SourceApp
	}
	if item.ProfileMapping == nil {
		item.ProfileMapping = map[string]string{}
	}
	if item.ExtraAuthParams == nil {
		item.ExtraAuthParams = map[string]string{}
	}
	if item.Scopes == nil {
		item.Scopes = []string{}
	}
	item.ClientSecretSet = strings.TrimSpace(item.ClientSecret) != ""
	if item.ClientSecretSet {
		item.ClientSecretHint = "已配置"
	}
	// 密文不出网：出网结构里的 ClientSecret 带 json:"-"，此处再显式清空防止误用
	item.ClientSecret = ""
}

// revealSecret 解密落库密文；兼容历史明文（升级期容错）。
func (s *AppOAuthService) revealSecret(cipher string) (string, error) {
	cipher = strings.TrimSpace(cipher)
	if cipher == "" {
		return "", nil
	}
	plaintext, err := decryptSecret(s.key, cipher)
	if err != nil {
		return "", err
	}
	return plaintext, nil
}

// platformProvider 把平台级 .env 配置投影成渠道视图（ID=0，Source=platform）。
func (s *AppOAuthService) platformProvider(appID int64, name string, order int) oauthdomain.Provider {
	platform := s.platform[name]
	template := oauthTemplateIndex[name]
	displayName := template.Name
	if displayName == "" {
		displayName = name
	}
	return oauthdomain.Provider{
		AppID: appID, Provider: name, Kind: platform.Kind,
		DisplayName: displayName, Icon: template.Icon, Color: template.Color,
		Enabled: true, ClientID: platform.ClientID, ClientSecret: platform.ClientSecret,
		RedirectURL: platform.RedirectURL, AuthURL: platform.AuthURL,
		TokenURL: platform.TokenURL, UserInfoURL: platform.UserInfoURL,
		Scopes: platform.Scopes, TokenAuthStyle: oauthdomain.TokenAuthAuto,
		UserInfoAuthStyle: oauthdomain.UserInfoAuthHeader,
		ProfileMapping:    map[string]string{}, ExtraAuthParams: map[string]string{},
		AllowLogin: true, AllowRegister: true, AllowBind: true,
		SortOrder: order, Source: oauthdomain.SourcePlatform,
		Remark: "来自平台级 .env 配置（OAUTH_" + strings.ToUpper(name) + "_*），保存后即转为应用级配置",
	}
}

// attachProviderMeta 给绑定记录补上渠道展示名与图标，前端无需再查一次配置。
func (s *AppOAuthService) attachProviderMeta(ctx context.Context, appID int64, items []oauthdomain.Binding) {
	if len(items) == 0 {
		return
	}
	meta := make(map[string]oauthdomain.Provider, 8)
	if configured, err := s.pg.ListAppOAuthProviders(ctx, appID); err == nil {
		for _, item := range configured {
			meta[item.Provider] = item
		}
	}
	for index := range items {
		slug := items[index].Provider
		if item, ok := meta[slug]; ok {
			items[index].DisplayName = item.DisplayName
			items[index].Icon = item.Icon
			continue
		}
		if template, ok := oauthTemplateIndex[slug]; ok {
			items[index].DisplayName = template.Name
			items[index].Icon = template.Icon
			continue
		}
		items[index].DisplayName = slug
	}
}

func publicView(item *oauthdomain.Provider) oauthdomain.PublicProvider {
	return oauthdomain.PublicProvider{
		Provider: item.Provider, DisplayName: item.DisplayName,
		Icon: item.Icon, Color: item.Color,
		AllowLogin: item.AllowLogin, AllowBind: item.AllowBind, SortOrder: item.SortOrder,
	}
}

// configIssues 列出阻碍该渠道正常工作的缺失项，供前端逐条提示。
func configIssues(item *oauthdomain.Provider) []string {
	issues := make([]string, 0, 4)
	if strings.TrimSpace(item.ClientID) == "" {
		issues = append(issues, "缺少 ClientID")
	}
	if strings.TrimSpace(item.ClientSecret) == "" && !item.ClientSecretSet {
		issues = append(issues, "缺少 ClientSecret")
	}
	if strings.TrimSpace(item.RedirectURL) == "" {
		issues = append(issues, "缺少回调地址")
	}
	if strings.TrimSpace(item.AuthURL) == "" {
		issues = append(issues, "缺少授权端点")
	}
	if strings.TrimSpace(item.TokenURL) == "" {
		issues = append(issues, "缺少令牌端点")
	}
	if strings.TrimSpace(item.UserInfoURL) == "" {
		issues = append(issues, "缺少用户信息端点")
	}
	if len(issues) == 0 {
		return nil
	}
	return issues
}

// configWarnings 不阻断启用、但值得提醒的配置问题。
func configWarnings(item *oauthdomain.Provider) []string {
	warnings := make([]string, 0, 2)
	if redirect := strings.TrimSpace(item.RedirectURL); redirect != "" {
		if parsed, err := url.Parse(redirect); err == nil && parsed.Scheme == "http" &&
			!isLoopbackHost(parsed.Hostname()) {
			warnings = append(warnings, "回调地址未使用 HTTPS，授权码可能在传输中泄露")
		}
	}
	if item.Kind == oauthdomain.KindGeneric && len(item.Scopes) == 0 {
		warnings = append(warnings, "未设置 scope，部分服务商会拒绝授权或不返回用户信息")
	}
	if !item.AllowRegister && item.AllowLogin {
		warnings = append(warnings, "已关闭自动注册，未绑定过的用户将无法通过该渠道登录")
	}
	if len(warnings) == 0 {
		return nil
	}
	return warnings
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func normalizeOAuthSlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeOAuthKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case oauthdomain.KindQQ:
		return oauthdomain.KindQQ
	case oauthdomain.KindWechat:
		return oauthdomain.KindWechat
	case oauthdomain.KindWeibo:
		return oauthdomain.KindWeibo
	case oauthdomain.KindGitHub:
		return oauthdomain.KindGitHub
	case oauthdomain.KindMicrosoft:
		return oauthdomain.KindMicrosoft
	case oauthdomain.KindGeneric:
		return oauthdomain.KindGeneric
	default:
		return ""
	}
}

func normalizeTokenAuthStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case oauthdomain.TokenAuthParams:
		return oauthdomain.TokenAuthParams
	case oauthdomain.TokenAuthBasic:
		return oauthdomain.TokenAuthBasic
	default:
		return oauthdomain.TokenAuthAuto
	}
}

func normalizeUserInfoAuthStyle(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), oauthdomain.UserInfoAuthQuery) {
		return oauthdomain.UserInfoAuthQuery
	}
	return oauthdomain.UserInfoAuthHeader
}

var allowedMappingKeys = map[string]bool{
	oauthdomain.MappingID: true, oauthdomain.MappingNickname: true,
	oauthdomain.MappingAvatar: true, oauthdomain.MappingEmail: true,
	oauthdomain.MappingUnionID: true,
}

// normalizeOAuthPairs 清洗 K/V 配置：去空、限长、限量；allowed 非空时限制键集合。
func normalizeOAuthPairs(input map[string]string, allowed map[string]bool) (map[string]string, error) {
	result := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if len(key) > oauthParamMaxLen || len(value) > oauthParamMaxLen {
			return nil, apperrors.New(40097, http.StatusBadRequest,
				fmt.Sprintf("配置项 %s 长度不能超过 %d 个字符", key, oauthParamMaxLen))
		}
		if allowed != nil && !allowed[key] {
			return nil, apperrors.New(40097, http.StatusBadRequest, "不支持的字段映射键："+key)
		}
		result[key] = value
	}
	if len(result) > oauthParamMaxCount {
		return nil, apperrors.New(40097, http.StatusBadRequest,
			fmt.Sprintf("配置项不能超过 %d 条", oauthParamMaxCount))
	}
	return result, nil
}

// normalizeOAuthScopes 去重去空并限量；输入为 nil 时返回 nil（表示"未提供"）。
func normalizeOAuthScopes(input []string) ([]string, error) {
	if input == nil {
		return nil, nil
	}
	result := make([]string, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		if len(item) > oauthScopeMaxLen {
			return nil, apperrors.New(40098, http.StatusBadRequest,
				fmt.Sprintf("单个 scope 不能超过 %d 个字符", oauthScopeMaxLen))
		}
		seen[item] = true
		result = append(result, item)
	}
	if len(result) > oauthScopeMaxCount {
		return nil, apperrors.New(40098, http.StatusBadRequest,
			fmt.Sprintf("scope 数量不能超过 %d 个", oauthScopeMaxCount))
	}
	return result, nil
}

func validateOAuthURL(label, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return apperrors.New(40099, http.StatusBadRequest, label+"必须是合法的 http/https 地址")
	}
	return nil
}

// pickBool 三段取值：显式传入 > 沿用旧值 > 新建默认。
func pickBool(input *bool, existing bool, isNew bool) bool {
	if input != nil {
		return *input
	}
	if isNew {
		return true
	}
	return existing
}

func sortedPlatformNames(platform map[string]config.OAuthProviderConfig) []string {
	names := make([]string, 0, len(platform))
	for name := range platform {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
