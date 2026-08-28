package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	platformdomain "aegis/internal/domain/platform"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// AIProviderService 管理 AI 供应商通道（系统级 + 应用级）并对内提供
// 「带链路回退的统一调用面」：Agent、脚本 SDK、兼容网关全部经它取通道发请求，
// 谁也不直接持有某一家的密钥。
type AIProviderService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
	// masterKey 派生自 SECURITY_MASTER_KEY，与邮件 / OAuth / NotifyHub 同构。
	masterKey []byte
	client    *aiLLMClient
	// governance 平台治理判定：被限制接口的应用不得再消耗 AI 通道。可选注入。
	governance *PlatformGovernanceService
}

func NewAIProviderService(log *zap.Logger, pg *pgrepo.Repository, masterKey string) *AIProviderService {
	digest := sha256.Sum256([]byte("aegis.ai.master\x00" + masterKey))
	return &AIProviderService{
		log:       log,
		pg:        pg,
		masterKey: digest[:],
		client:    newAILLMClient(log),
	}
}

// SetGovernanceService 注入平台治理服务（bootstrap 中调用）。
func (s *AIProviderService) SetGovernanceService(g *PlatformGovernanceService) { s.governance = g }

// ProviderCatalog 供应商自述目录。
func (s *AIProviderService) ProviderCatalog() []aidomain.ProviderMeta {
	return aidomain.Providers()
}

// ── 配置管理面 ──

func (s *AIProviderService) ListConfigs(ctx context.Context, appID int64) ([]aidomain.Config, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	items, err := s.pg.ListAIProviderConfigs(ctx, appID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		s.sanitizeConfig(&items[i])
	}
	return items, nil
}

func (s *AIProviderService) Detail(ctx context.Context, appID int64, id int64) (*aidomain.Config, error) {
	item, err := s.loadConfig(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	sanitized := item.Clone()
	s.sanitizeConfig(&sanitized)
	return &sanitized, nil
}

// loadConfig 取出配置**并解密密钥**，仅供内部调用链路使用。
func (s *AIProviderService) loadConfig(ctx context.Context, appID int64, id int64) (*aidomain.Config, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetAIProviderConfigByID(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40510, http.StatusNotFound, "AI 供应商配置不存在")
	}
	s.decryptSecrets(item)
	return item, nil
}

func (s *AIProviderService) Save(ctx context.Context, mutation aidomain.ConfigMutation) (*aidomain.Config, error) {
	if err := s.ensureScope(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetAIProviderConfigByID(ctx, mutation.AppID, mutation.ID)
	if err != nil {
		return nil, err
	}
	if mutation.ID > 0 && current == nil {
		return nil, apperrors.New(40510, http.StatusNotFound, "AI 供应商配置不存在")
	}

	item := aidomain.Config{
		ID:        mutation.ID,
		AppID:     mutation.AppID,
		Name:      "default",
		Provider:  aidomain.ProviderOpenAI,
		Enabled:   true,
		IsDefault: mutation.ID == 0,
		Settings:  map[string]string{},
	}
	if current != nil {
		s.decryptSecrets(current)
		item = current.Clone()
	}

	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Provider != nil {
		item.Provider = strings.ToLower(strings.TrimSpace(*mutation.Provider))
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.IsDefault != nil {
		item.IsDefault = *mutation.IsDefault
	}
	if mutation.Shared != nil {
		item.Shared = *mutation.Shared
	}
	if mutation.Priority != nil {
		item.Priority = *mutation.Priority
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	// 共享开关只对平台级配置有意义，应用级一律当没写（与邮件同一条约定）。
	if !item.IsPlatform() {
		item.Shared = false
	}
	if item.Settings == nil {
		item.Settings = map[string]string{}
	}
	if mutation.ReplaceSettings {
		item.Settings = map[string]string{}
	}
	for key, value := range mutation.Settings {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			delete(item.Settings, key)
			continue
		}
		item.Settings[key] = value
	}

	if item.Name == "" {
		return nil, apperrors.New(40512, http.StatusBadRequest, "配置名称不能为空")
	}
	meta, ok := aidomain.ProviderByKey(item.Provider)
	if !ok {
		return nil, apperrors.New(40511, http.StatusBadRequest, "不支持的 AI 供应商："+item.Provider)
	}

	// 密钥处理：留空即不修改，显式 ClearSecrets 才清空。
	if item.SecretsCipher == nil {
		item.SecretsCipher = map[string]string{}
	}
	for key, plain := range mutation.Secrets {
		key = strings.TrimSpace(key)
		plain = strings.TrimSpace(plain)
		if key == "" || plain == "" {
			continue
		}
		cipherText, err := encryptSecret(s.masterKey, plain)
		if err != nil {
			return nil, fmt.Errorf("加密密钥失败：%w", err)
		}
		item.SecretsCipher[key] = cipherText
	}
	for _, key := range mutation.ClearSecrets {
		delete(item.SecretsCipher, strings.TrimSpace(key))
	}

	// 必填字段校验依据目录声明，而不是每家一段 if。
	for _, field := range meta.Fields {
		if !field.Required {
			continue
		}
		if field.Secret {
			if !item.HasSecret(field.Key) {
				return nil, apperrors.New(40512, http.StatusBadRequest,
					fmt.Sprintf("%s 未配置：%s", field.Label, meta.Name))
			}
			continue
		}
		if item.Setting(field.Key) == "" {
			return nil, apperrors.New(40512, http.StatusBadRequest,
				fmt.Sprintf("%s 不能为空：%s", field.Label, meta.Name))
		}
	}
	if base := item.Setting(aidomain.KeyBaseURL); base != "" {
		// 「只填站点地址」也接受：缺协议头按 https 补全后落库（内网 http 需显式写明）。
		if !strings.Contains(base, "://") {
			base = "https://" + base
			item.Settings[aidomain.KeyBaseURL] = base
		}
		parsed, err := url.Parse(base)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, apperrors.New(40512, http.StatusBadRequest, "端点地址必须是完整的 http(s) URL")
		}
	}

	saved, err := s.pg.UpsertAIProviderConfig(ctx, item)
	if err != nil {
		if strings.Contains(err.Error(), "uq_ai_provider_configs_scope_name") {
			return nil, apperrors.New(40512, http.StatusBadRequest, "同名配置已存在："+item.Name)
		}
		return nil, err
	}
	sanitized := saved.Clone()
	s.sanitizeConfig(&sanitized)
	return &sanitized, nil
}

func (s *AIProviderService) Delete(ctx context.Context, appID int64, id int64) error {
	if err := s.ensureScope(ctx, appID); err != nil {
		return err
	}
	deleted, err := s.pg.DeleteAIProviderConfig(ctx, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40510, http.StatusNotFound, "AI 供应商配置不存在")
	}
	return nil
}

// TestConfig 连通性测试：用该通道发一个最小的对话请求，报「通没通、多少毫秒、什么型号」。
func (s *AIProviderService) TestConfig(ctx context.Context, appID int64, id int64, model string) (map[string]any, error) {
	config, err := s.loadConfig(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = config.DefaultModel()
	}
	if model == "" {
		return nil, apperrors.New(40514, http.StatusBadRequest, "请先在配置里填写可用型号，或在请求里指定 model")
	}
	testCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	started := time.Now()
	response, err := s.client.Chat(testCtx, *config, aidomain.ChatRequest{
		Model:     model,
		Messages:  []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, "ping，请只回复 pong")},
		MaxTokens: 512,
	})
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		var upstream *aiUpstreamError
		if errors.As(err, &upstream) {
			return map[string]any{
				"ok": false, "elapsedMs": elapsed, "model": model, "error": upstream.Message,
				"status": upstream.Status,
			}, nil
		}
		return map[string]any{"ok": false, "elapsedMs": elapsed, "model": model, "error": err.Error()}, nil
	}
	return map[string]any{
		"ok": true, "elapsedMs": elapsed, "model": model,
		"reply": strings.TrimSpace(response.Text),
		"usage": response.Usage,
	}, nil
}

// ── 链路解析与统一调用面 ──

// resolvedChannel 链路里的一条可用通道（密钥已解密）。
type resolvedChannel struct {
	Config    aidomain.Config
	Meta      aidomain.ProviderMeta
	Inherited bool
}

// ResolveChain 解析某应用可用的完整供应商链路：
// 自有启用配置按 priority 排队；一条都没有时回落到平台级已共享的通道。
// requestedModel 非空时跳过声明了型号清单却不含它的通道。
func (s *AIProviderService) ResolveChain(ctx context.Context, appID int64, requestedModel string) ([]resolvedChannel, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	own, err := s.pg.ListEnabledAIProviderConfigs(ctx, appID)
	if err != nil {
		return nil, err
	}
	inherited := false
	pool := own
	if len(pool) == 0 && appID != aidomain.PlatformAppID {
		shared, err := s.pg.ListSharedPlatformAIConfigs(ctx)
		if err != nil {
			return nil, err
		}
		pool = shared
		inherited = true
	}

	chain := make([]resolvedChannel, 0, len(pool))
	for i := range pool {
		config := pool[i]
		meta, ok := aidomain.ProviderByKey(config.Provider)
		if !ok {
			continue
		}
		if requestedModel != "" && !config.HasModel(requestedModel) {
			continue
		}
		s.decryptSecrets(&config)
		chain = append(chain, resolvedChannel{Config: config, Meta: meta, Inherited: inherited})
	}
	if len(chain) == 0 {
		return nil, apperrors.New(40513, http.StatusFailedDependency,
			"没有可用的 AI 通道：请先在应用或平台配置里接入至少一家供应商")
	}
	return chain, nil
}

// ChannelOverview 控制台「当前生效链路」视图：每条通道用哪家、什么型号、是否继承。
func (s *AIProviderService) ChannelOverview(ctx context.Context, appID int64) ([]aidomain.Resolution, error) {
	chain, err := s.ResolveChain(ctx, appID, "")
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) && appErr.Code == 40513 {
			return []aidomain.Resolution{}, nil
		}
		return nil, err
	}
	out := make([]aidomain.Resolution, 0, len(chain))
	for _, channel := range chain {
		scope := aidomain.ScopeApp
		if channel.Config.IsPlatform() {
			scope = aidomain.ScopePlatform
		}
		out = append(out, aidomain.Resolution{
			ConfigID:   channel.Config.ID,
			ConfigName: channel.Config.Name,
			Provider:   channel.Config.Provider,
			Protocol:   channel.Meta.Protocol,
			Scope:      scope,
			Inherited:  channel.Inherited,
			Model:      channel.Config.DefaultModel(),
			Models:     channel.Config.Models(),
		})
	}
	return out, nil
}

// chatArgs 统一调用面的入参。ConfigID 非 0 时钉死某条通道（不回退）。
type aiChatArgs struct {
	AppID    int64
	ConfigID int64
	Request  aidomain.ChatRequest
}

// Chat 非流式：带链路回退。返回实际使用的通道与结论。
func (s *AIProviderService) Chat(ctx context.Context, args aiChatArgs) (*aidomain.ChatResponse, *resolvedChannel, error) {
	return s.dispatch(ctx, args, nil)
}

// ChatStream 流式：带链路回退（只在**第一个增量到达之前**回退 ——
// 流已经开始再换供应商，调用方会看到两份开头）。
func (s *AIProviderService) ChatStream(ctx context.Context, args aiChatArgs,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error) {
	return s.dispatch(ctx, args, onEvent)
}

func (s *AIProviderService) dispatch(ctx context.Context, args aiChatArgs,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error) {
	if err := s.ensureUsable(ctx, args.AppID); err != nil {
		return nil, nil, err
	}

	var chain []resolvedChannel
	if args.ConfigID > 0 {
		config, err := s.loadConfig(ctx, args.AppID, args.ConfigID)
		if err != nil {
			// 应用作用域找不到时再看平台共享 —— 会话里记住的可能是共享通道的 id。
			if args.AppID != aidomain.PlatformAppID {
				if shared, sharedErr := s.loadSharedConfig(ctx, args.ConfigID); sharedErr == nil {
					config = shared
					err = nil
				}
			}
			if err != nil {
				return nil, nil, err
			}
		}
		if !config.Enabled {
			return nil, nil, apperrors.New(40513, http.StatusFailedDependency, "指定的 AI 通道已停用")
		}
		meta, ok := aidomain.ProviderByKey(config.Provider)
		if !ok {
			return nil, nil, apperrors.New(40511, http.StatusBadRequest, "不支持的 AI 供应商："+config.Provider)
		}
		chain = []resolvedChannel{{Config: *config, Meta: meta, Inherited: config.IsPlatform() && args.AppID != aidomain.PlatformAppID}}
	} else {
		resolved, err := s.ResolveChain(ctx, args.AppID, args.Request.Model)
		if err != nil {
			return nil, nil, err
		}
		chain = resolved
	}

	var lastErr error
	for i := range chain {
		channel := &chain[i]
		request := args.Request
		if strings.TrimSpace(request.Model) == "" {
			request.Model = channel.Config.DefaultModel()
		}
		if strings.TrimSpace(request.Model) == "" {
			lastErr = apperrors.New(40514, http.StatusBadRequest,
				fmt.Sprintf("通道「%s」没有可用型号：请在配置里填写", channel.Config.Name))
			continue
		}

		streamStarted := false
		wrapped := onEvent
		if onEvent != nil {
			wrapped = func(event aidomain.StreamEvent) error {
				streamStarted = true
				return onEvent(event)
			}
		}

		var response *aidomain.ChatResponse
		var err error
		if onEvent != nil {
			response, err = s.client.ChatStream(ctx, channel.Config, request, wrapped)
		} else {
			response, err = s.client.Chat(ctx, channel.Config, request)
		}
		if err == nil {
			return response, channel, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return nil, nil, err
		}
		var upstream *aiUpstreamError
		retryable := errors.As(err, &upstream) && upstream.Retryable
		if !retryable || streamStarted {
			return nil, channel, err
		}
		s.log.Warn("AI 通道调用失败，尝试链路里的下一条",
			zap.Int64("appid", args.AppID), zap.String("config", channel.Config.Name),
			zap.String("provider", channel.Config.Provider), zap.Error(err))
	}
	if lastErr == nil {
		lastErr = apperrors.New(40513, http.StatusFailedDependency, "没有可用的 AI 通道")
	}
	return nil, nil, lastErr
}

// GatewayChat 兼容网关的非流式调用面：应用作用域 + 链路回退。
func (s *AIProviderService) GatewayChat(ctx context.Context, appID int64,
	request aidomain.ChatRequest) (*aidomain.ChatResponse, error) {
	response, _, err := s.Chat(ctx, aiChatArgs{AppID: appID, Request: request})
	return response, err
}

// GatewayChatStream 兼容网关的流式调用面。
func (s *AIProviderService) GatewayChatStream(ctx context.Context, appID int64,
	request aidomain.ChatRequest, onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	response, _, err := s.ChatStream(ctx, aiChatArgs{AppID: appID, Request: request}, onEvent)
	return response, err
}

// loadSharedConfig 按 id 取一条平台级已共享的通道（供会话续用）。
func (s *AIProviderService) loadSharedConfig(ctx context.Context, id int64) (*aidomain.Config, error) {
	shared, err := s.pg.ListSharedPlatformAIConfigs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range shared {
		if shared[i].ID == id {
			s.decryptSecrets(&shared[i])
			return &shared[i], nil
		}
	}
	return nil, apperrors.New(40510, http.StatusNotFound, "AI 供应商配置不存在")
}

// ensureUsable 作用域存在 + 平台治理放行（接口限制的应用不得再消耗 AI 通道）。
func (s *AIProviderService) ensureUsable(ctx context.Context, appID int64) error {
	if err := s.ensureScope(ctx, appID); err != nil {
		return err
	}
	if appID != aidomain.PlatformAppID && s.governance != nil {
		if err := s.governance.EnsureCapability(appID, platformdomain.CapabilityAPI); err != nil {
			return err
		}
	}
	return nil
}

// ── 密钥处理 ──

func (s *AIProviderService) sanitizeConfig(item *aidomain.Config) {
	if item == nil {
		return
	}
	set := make(map[string]bool, 2)
	if meta, ok := aidomain.ProviderByKey(item.Provider); ok {
		for _, key := range meta.SecretKeys() {
			set[key] = item.HasSecret(key)
		}
	}
	for key, cipherText := range item.SecretsCipher {
		if _, known := set[key]; !known && strings.TrimSpace(cipherText) != "" {
			set[key] = true
		}
	}
	item.SecretSet = set
	item.Secrets = nil
	item.SecretsCipher = nil
}

func (s *AIProviderService) decryptSecrets(item *aidomain.Config) {
	if item == nil {
		return
	}
	if item.Secrets == nil {
		item.Secrets = map[string]string{}
	}
	for key, cipherText := range item.SecretsCipher {
		if strings.TrimSpace(cipherText) == "" {
			continue
		}
		plain, err := decryptSecret(s.masterKey, cipherText)
		if err != nil {
			s.log.Error("decrypt ai secret failed",
				zap.Int64("appid", item.AppID), zap.String("config", item.Name),
				zap.String("field", key), zap.Error(err))
			continue
		}
		item.Secrets[key] = plain
	}
}

func (s *AIProviderService) ensureScope(ctx context.Context, appID int64) error {
	if appID == aidomain.PlatformAppID {
		return nil
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return err
	}
	if app == nil {
		return apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return nil
}

// ── 技能 ──

// ListSkills 某作用域可用的全部技能：内置 + 平台自定义 + 应用自定义。
// 应用作用域下平台技能同样可见 —— 技能是提示词包，没有租户数据，共享是安全的。
func (s *AIProviderService) ListSkills(ctx context.Context, appID int64) ([]aidomain.Skill, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	items := builtinSkills()
	platform, err := s.pg.ListAISkills(ctx, aidomain.PlatformAppID)
	if err != nil {
		return nil, err
	}
	items = append(items, platform...)
	if appID != aidomain.PlatformAppID {
		own, err := s.pg.ListAISkills(ctx, appID)
		if err != nil {
			return nil, err
		}
		items = append(items, own...)
	}
	return items, nil
}

func (s *AIProviderService) SaveSkill(ctx context.Context, mutation aidomain.SkillMutation) (*aidomain.Skill, error) {
	if err := s.ensureScope(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	item := aidomain.Skill{ID: mutation.ID, AppID: mutation.AppID, Enabled: true}
	if mutation.ID > 0 {
		current, err := s.pg.GetAISkillByID(ctx, mutation.AppID, mutation.ID)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, apperrors.New(40515, http.StatusNotFound, "技能不存在")
		}
		item = *current
	}
	if mutation.Key != nil {
		item.Key = strings.TrimSpace(*mutation.Key)
	}
	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.Content != nil {
		item.Content = *mutation.Content
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if item.Key == "" || item.Name == "" {
		return nil, apperrors.New(40512, http.StatusBadRequest, "技能标识与名称不能为空")
	}
	if strings.HasPrefix(item.Key, "builtin:") {
		return nil, apperrors.New(40512, http.StatusBadRequest, "builtin: 前缀保留给内置技能")
	}
	if len(item.Content) > 64<<10 {
		return nil, apperrors.New(40512, http.StatusBadRequest, "技能内容过长（上限 64KB）")
	}
	saved, err := s.pg.UpsertAISkill(ctx, item)
	if err != nil {
		if strings.Contains(err.Error(), "uq_ai_skills_scope_key") {
			return nil, apperrors.New(40512, http.StatusBadRequest, "同标识技能已存在："+item.Key)
		}
		return nil, err
	}
	return saved, nil
}

func (s *AIProviderService) DeleteSkill(ctx context.Context, appID int64, id int64) error {
	if err := s.ensureScope(ctx, appID); err != nil {
		return err
	}
	deleted, err := s.pg.DeleteAISkill(ctx, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40515, http.StatusNotFound, "技能不存在")
	}
	return nil
}

// ResolveSkillContents 把技能键翻成注入系统提示词的内容段。
// 未知键静默跳过 —— 会话里记住的技能可能已被删除，不该让整轮对话失败。
func (s *AIProviderService) ResolveSkillContents(ctx context.Context, appID int64, keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	available, err := s.ListSkills(ctx, appID)
	if err != nil {
		s.log.Warn("list ai skills failed", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	index := make(map[string]aidomain.Skill, len(available))
	for _, skill := range available {
		index[skill.Key] = skill
	}
	sections := make([]string, 0, len(keys))
	for _, key := range keys {
		skill, ok := index[strings.TrimSpace(key)]
		if !ok || !skill.Enabled || strings.TrimSpace(skill.Content) == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("## 技能：%s\n\n%s", skill.Name, strings.TrimSpace(skill.Content)))
	}
	return sections
}

// ── MCP 服务器 ──

func (s *AIProviderService) ListMCPServers(ctx context.Context, appID int64) ([]aidomain.MCPServer, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	items, err := s.pg.ListAIMCPServers(ctx, appID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].HeadersSet = strings.TrimSpace(items[i].HeadersCipher) != ""
		items[i].HeadersCipher = ""
	}
	return items, nil
}

// ListUsableMCPServers Agent 用：应用自有 + 平台级，密钥已解密。
func (s *AIProviderService) ListUsableMCPServers(ctx context.Context, appID int64) ([]aidomain.MCPServer, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	items, err := s.pg.ListAIMCPServers(ctx, appID)
	if err != nil {
		return nil, err
	}
	if appID != aidomain.PlatformAppID {
		platform, err := s.pg.ListAIMCPServers(ctx, aidomain.PlatformAppID)
		if err != nil {
			return nil, err
		}
		items = append(items, platform...)
	}
	usable := make([]aidomain.MCPServer, 0, len(items))
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		s.decryptMCPHeaders(&item)
		usable = append(usable, item)
	}
	return usable, nil
}

func (s *AIProviderService) SaveMCPServer(ctx context.Context, mutation aidomain.MCPServerMutation) (*aidomain.MCPServer, error) {
	if err := s.ensureScope(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	item := aidomain.MCPServer{ID: mutation.ID, AppID: mutation.AppID, Enabled: true}
	if mutation.ID > 0 {
		current, err := s.pg.GetAIMCPServerByID(ctx, mutation.AppID, mutation.ID)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, apperrors.New(40516, http.StatusNotFound, "MCP 服务器不存在")
		}
		item = *current
	}
	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.URL != nil {
		item.URL = strings.TrimSpace(*mutation.URL)
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if item.Name == "" {
		return nil, apperrors.New(40512, http.StatusBadRequest, "MCP 服务器名称不能为空")
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, apperrors.New(40512, http.StatusBadRequest, "MCP 地址必须是完整的 http(s) URL")
	}
	if mutation.ClearHeaders {
		item.HeadersCipher = ""
	} else if mutation.Headers != nil {
		compact := map[string]string{}
		for key, value := range mutation.Headers {
			key = strings.TrimSpace(key)
			if key == "" || strings.TrimSpace(value) == "" {
				continue
			}
			compact[key] = value
		}
		if len(compact) > 0 {
			encoded, err := json.Marshal(compact)
			if err != nil {
				return nil, err
			}
			cipherText, err := encryptSecret(s.masterKey, string(encoded))
			if err != nil {
				return nil, fmt.Errorf("加密请求头失败：%w", err)
			}
			item.HeadersCipher = cipherText
		} else {
			item.HeadersCipher = ""
		}
	}

	saved, err := s.pg.UpsertAIMCPServer(ctx, item)
	if err != nil {
		if strings.Contains(err.Error(), "uq_ai_mcp_servers_scope_name") {
			return nil, apperrors.New(40512, http.StatusBadRequest, "同名 MCP 服务器已存在："+item.Name)
		}
		return nil, err
	}
	saved.HeadersSet = strings.TrimSpace(saved.HeadersCipher) != ""
	saved.HeadersCipher = ""
	return saved, nil
}

func (s *AIProviderService) DeleteMCPServer(ctx context.Context, appID int64, id int64) error {
	if err := s.ensureScope(ctx, appID); err != nil {
		return err
	}
	deleted, err := s.pg.DeleteAIMCPServer(ctx, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40516, http.StatusNotFound, "MCP 服务器不存在")
	}
	return nil
}

// TestMCPServer 连通性测试：initialize + tools/list，报「通没通、几个工具」。
func (s *AIProviderService) TestMCPServer(ctx context.Context, appID int64, id int64) (map[string]any, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetAIMCPServerByID(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40516, http.StatusNotFound, "MCP 服务器不存在")
	}
	s.decryptMCPHeaders(item)

	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	client := newAIMCPClient(s.log, *item)
	defer client.Close()
	tools, err := client.ListTools(testCtx)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		return map[string]any{"ok": false, "elapsedMs": elapsed, "error": err.Error()}, nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return map[string]any{"ok": true, "elapsedMs": elapsed, "toolCount": len(tools), "tools": names}, nil
}

func (s *AIProviderService) decryptMCPHeaders(item *aidomain.MCPServer) {
	item.Headers = map[string]string{}
	cipherText := strings.TrimSpace(item.HeadersCipher)
	if cipherText == "" {
		return
	}
	plain, err := decryptSecret(s.masterKey, cipherText)
	if err != nil {
		s.log.Error("decrypt mcp headers failed",
			zap.Int64("appid", item.AppID), zap.String("server", item.Name), zap.Error(err))
		return
	}
	_ = json.Unmarshal([]byte(plain), &item.Headers)
}
