package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	functiondomain "aegis/internal/domain/appfunction"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	functionNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	functionVersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$`)
	// 能力即授权：script 运行时只会绑定这里声明过的 SDK 命名空间，
	// 未声明的能力在脚本里根本不存在。
	allowedCapabilities = map[string]struct{}{
		CapUserRead:         {},
		CapPointsWrite:      {},
		CapVipWrite:         {},
		CapKVRead:           {},
		CapKVWrite:          {},
		CapNotificationSend: {},
		CapAuditWrite:       {},
		CapHTTPFetch:        {},

		// 000056 时期的旧能力名。当时 effects 只校验不执行，声明了也没有实际作用；
		// 保留它们仅为让存量函数仍能通过更新校验，新函数请使用上面的能力名。
		"storage.read":      {},
		"storage.write":     {},
		"user.profile.read": {},
		"user.tag.write":    {},
	}
)

type AppFunctionService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	sandbox    *AppFunctionSandbox
	script     *AppFunctionScriptExecutor
	http       *AppFunctionHTTPExecutor
	sdk        ScriptSDKDeps
	concurrent sync.Map
}

func NewAppFunctionService(log *zap.Logger, pg *pgrepo.Repository, rootSecret string) *AppFunctionService {
	executor := NewAppFunctionHTTPExecutor(rootSecret)
	return &AppFunctionService{
		log: log, pg: pg,
		sandbox: NewAppFunctionSandbox(),
		script:  NewAppFunctionScriptExecutor(),
		http:    executor,
		sdk:     ScriptSDKDeps{Log: log, PG: pg, HTTP: executor},
	}
}

// SetScriptDeps 注入脚本 SDK 需要的业务服务。
//
// 单独提供而不是塞进构造函数，是因为这些服务与 AppFunctionService 之间存在
// 构造顺序上的相互依赖，由 bootstrap 在全部服务就绪后统一装配。
func (s *AppFunctionService) SetScriptDeps(
	points *PointsService,
	vip *VipService,
	notifications *NotificationService,
	audit *AuditService,
) {
	s.sdk.Points = points
	s.sdk.Vip = vip
	s.sdk.Notifications = notifications
	s.sdk.Audit = audit
}

func (s *AppFunctionService) SigningPublicKey() string {
	return s.http.PublicKey()
}

func (s *AppFunctionService) CreateKey(ctx context.Context, appID int64, name string, createdBy *int64) (*functiondomain.CreatedKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 96 {
		return nil, apperrors.New(40085, http.StatusBadRequest, "密钥名称长度必须在 1 到 96 之间")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	secret := "afk_" + base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(secret))
	item, err := s.pg.CreateAppFunctionKey(ctx, functiondomain.Key{
		AppID: appID, Name: name, KeyPrefix: secret[:12], KeyHash: digest[:], CreatedBy: createdBy,
	})
	if err != nil {
		return nil, err
	}
	return &functiondomain.CreatedKey{Key: *item, Secret: secret}, nil
}

func (s *AppFunctionService) ListKeys(ctx context.Context, appID int64) ([]functiondomain.Key, error) {
	return s.pg.ListAppFunctionKeys(ctx, appID)
}

func (s *AppFunctionService) RevokeKey(ctx context.Context, appID, keyID int64) error {
	count, err := s.pg.RevokeAppFunctionKey(ctx, appID, keyID)
	if err != nil {
		return err
	}
	if count == 0 {
		return apperrors.New(40492, http.StatusNotFound, "函数密钥不存在或已撤销")
	}
	return nil
}

func (s *AppFunctionService) AuthenticateKey(ctx context.Context, appID int64, secret string) (*functiondomain.Key, error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, "afk_") || len(secret) < 40 {
		return nil, apperrors.New(40190, http.StatusUnauthorized, "函数密钥无效")
	}
	digest := sha256.Sum256([]byte(secret))
	item, err := s.pg.GetActiveAppFunctionKeyByHash(ctx, appID, digest[:])
	if err != nil {
		return nil, err
	}
	if item == nil || subtle.ConstantTimeCompare(item.KeyHash, digest[:]) != 1 {
		return nil, apperrors.New(40190, http.StatusUnauthorized, "函数密钥无效")
	}
	s.pg.TouchAppFunctionKey(ctx, appID, item.ID)
	return item, nil
}

func (s *AppFunctionService) CreateFunction(ctx context.Context, input functiondomain.CreateFunctionInput) (*functiondomain.Function, error) {
	input.Name = strings.ToLower(strings.TrimSpace(input.Name))
	input.Description = strings.TrimSpace(input.Description)
	if !functionNamePattern.MatchString(input.Name) {
		return nil, apperrors.New(40090, http.StatusBadRequest, "函数名只能包含小写字母、数字、点、下划线和连字符")
	}
	if input.Runtime != functiondomain.RuntimeWASM && input.Runtime != functiondomain.RuntimeHTTP {
		return nil, apperrors.New(40091, http.StatusBadRequest, "函数运行时仅支持 wasm 或 http")
	}
	if err := validateCapabilities(input.Capabilities); err != nil {
		return nil, err
	}
	input.TimeoutMs = clamp(input.TimeoutMs, 10, 30000, 500)
	input.MaxRequestBytes = clamp(input.MaxRequestBytes, 1, 1<<20, 64<<10)
	input.MaxResponseBytes = clamp(input.MaxResponseBytes, 1, 1<<20, 64<<10)
	return s.pg.CreateAppFunction(ctx, input)
}

func (s *AppFunctionService) ListFunctions(ctx context.Context, appID int64) ([]functiondomain.Function, error) {
	return s.pg.ListAppFunctions(ctx, appID)
}

func (s *AppFunctionService) GetFunction(ctx context.Context, appID int64, name string) (*functiondomain.Function, error) {
	item, err := s.pg.GetAppFunction(ctx, appID, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40490, http.StatusNotFound, "应用函数不存在")
	}
	return item, nil
}

func (s *AppFunctionService) UpdateFunction(ctx context.Context, appID int64, name string, input functiondomain.UpdateFunctionInput) (*functiondomain.Function, error) {
	if input.Status != nil && *input.Status != functiondomain.StatusDraft &&
		*input.Status != functiondomain.StatusActive && *input.Status != functiondomain.StatusDisabled {
		return nil, apperrors.New(40092, http.StatusBadRequest, "函数状态无效")
	}
	if input.Capabilities != nil {
		if err := validateCapabilities(input.Capabilities); err != nil {
			return nil, err
		}
	}
	if input.TimeoutMs != nil && (*input.TimeoutMs < 10 || *input.TimeoutMs > 30000) {
		return nil, apperrors.New(40093, http.StatusBadRequest, "timeoutMs 必须在 10 到 30000 之间")
	}
	item, err := s.pg.UpdateAppFunction(ctx, appID, strings.ToLower(strings.TrimSpace(name)), input)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40490, http.StatusNotFound, "应用函数不存在")
	}
	return item, nil
}

func (s *AppFunctionService) DeleteFunction(ctx context.Context, appID int64, name string) error {
	count, err := s.pg.DeleteAppFunction(ctx, appID, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return err
	}
	if count == 0 {
		return apperrors.New(40490, http.StatusNotFound, "应用函数不存在")
	}
	return nil
}

func (s *AppFunctionService) CreateVersion(ctx context.Context, function *functiondomain.Function, input functiondomain.CreateVersionInput) (*functiondomain.Version, error) {
	input.Version = strings.TrimSpace(input.Version)
	if !functionVersionPattern.MatchString(input.Version) {
		return nil, apperrors.New(40094, http.StatusBadRequest, "函数版本格式无效")
	}
	input.AppID = function.AppID
	input.FunctionID = function.ID
	switch function.Runtime {
	case functiondomain.RuntimeWASM:
		if input.EndpointURL != "" || input.ResponsePublicKey != "" || input.Source != "" {
			return nil, apperrors.New(40095, http.StatusBadRequest, "WASM 版本不能设置远程端点或脚本")
		}
		validateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := s.sandbox.Validate(validateCtx, input.WASMModule); err != nil {
			return nil, apperrors.New(40096, http.StatusBadRequest, err.Error())
		}
		digest := sha256.Sum256(input.WASMModule)
		input.ArtifactSHA256 = hex.EncodeToString(digest[:])
	case functiondomain.RuntimeScript:
		if len(input.WASMModule) != 0 || input.EndpointURL != "" {
			return nil, apperrors.New(40100, http.StatusBadRequest, "脚本版本不能上传 WASM 或设置远程端点")
		}
		// 发布前做语法与入口检查，避免把跑不起来的脚本激活到线上
		if err := s.script.Validate(input.Source); err != nil {
			return nil, apperrors.New(40101, http.StatusBadRequest, err.Error())
		}
		digest := sha256.Sum256([]byte(input.Source))
		input.ArtifactSHA256 = hex.EncodeToString(digest[:])
	case functiondomain.RuntimeHTTP:
		if len(input.WASMModule) != 0 || input.Source != "" {
			return nil, apperrors.New(40097, http.StatusBadRequest, "HTTP 版本不能上传 WASM 或脚本")
		}
		validateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := s.http.ValidateEndpoint(validateCtx, input.EndpointURL); err != nil {
			return nil, apperrors.New(40098, http.StatusBadRequest, err.Error())
		}
		if err := validateResponsePublicKey(input.ResponsePublicKey); err != nil {
			return nil, apperrors.New(40099, http.StatusBadRequest, err.Error())
		}
		digest := sha256.Sum256([]byte(strings.TrimSpace(input.EndpointURL) + "\n" + input.ResponsePublicKey))
		input.ArtifactSHA256 = hex.EncodeToString(digest[:])
	}
	return s.pg.CreateAppFunctionVersion(ctx, input)
}

func (s *AppFunctionService) ListVersions(ctx context.Context, appID int64, name string) ([]functiondomain.Version, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	return s.pg.ListAppFunctionVersions(ctx, appID, function.ID)
}

func (s *AppFunctionService) ActivateVersion(ctx context.Context, appID int64, name, version string) error {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return err
	}
	item, err := s.pg.GetAppFunctionVersion(ctx, appID, function.ID, strings.TrimSpace(version))
	if err != nil {
		return err
	}
	if item == nil {
		return apperrors.New(40491, http.StatusNotFound, "函数版本不存在")
	}
	return s.pg.ActivateAppFunctionVersion(ctx, appID, function.ID, item.Version)
}

func (s *AppFunctionService) Invoke(ctx context.Context, appID int64, name, eventID string, input json.RawMessage, caller functiondomain.Caller) (*functiondomain.InvocationResult, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	if function.Status != functiondomain.StatusActive || function.ActiveVersion == "" {
		return nil, apperrors.New(40990, http.StatusConflict, "应用函数尚未激活")
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if !json.Valid(input) || len(input) > function.MaxRequestBytes {
		return nil, apperrors.New(40089, http.StatusBadRequest, "函数输入不是有效 JSON 或超过大小限制")
	}
	if eventID == "" {
		eventID = uuid.NewString()
	} else if _, parseErr := uuid.Parse(eventID); parseErr != nil {
		return nil, apperrors.New(40088, http.StatusBadRequest, "eventId 必须是 UUID")
	}
	if existing, existingErr := s.pg.GetAppFunctionInvocationByEvent(ctx, appID, eventID); existingErr == nil && existing != nil {
		if existing.Result != nil {
			return existing.Result, nil
		}
		return nil, apperrors.New(40991, http.StatusConflict, "相同 eventId 的调用已经失败")
	}

	version, err := s.pg.GetAppFunctionVersion(ctx, appID, function.ID, function.ActiveVersion)
	if err != nil {
		return nil, err
	}
	if version == nil || version.Status != functiondomain.VersionActive {
		return nil, apperrors.New(40992, http.StatusConflict, "激活版本不可用")
	}
	release, ok := s.acquire(function.ID)
	if !ok {
		return nil, apperrors.New(42990, http.StatusTooManyRequests, "函数并发调用已达到限制")
	}
	defer release()

	request := functiondomain.InvocationRequest{
		EventID: eventID, AppID: appID, Function: function.Name,
		Version: version.Version, Caller: caller, Input: input,
	}
	payload, _ := json.Marshal(request)
	requestDigest := sha256.Sum256(payload)
	invocation := functiondomain.Invocation{
		EventID: eventID, AppID: appID, FunctionID: function.ID, VersionID: version.ID,
		CallerType: caller.Type, CallerID: callerID(caller),
		RequestSHA256: hex.EncodeToString(requestDigest[:]),
	}
	reserved, err := s.pg.ReserveAppFunctionInvocation(ctx, invocation)
	if err != nil {
		return nil, err
	}
	if !reserved {
		existing, getErr := s.pg.GetAppFunctionInvocationByEvent(ctx, appID, eventID)
		if getErr != nil {
			return nil, getErr
		}
		if existing != nil && existing.Result != nil {
			return existing.Result, nil
		}
		return nil, apperrors.New(40993, http.StatusConflict, "相同 eventId 的函数调用正在执行或已经失败")
	}
	start := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(function.TimeoutMs)*time.Millisecond)
	defer cancel()

	var (
		result   *functiondomain.InvocationResult
		raw      []byte
		business *ScriptBusinessError
	)
	switch function.Runtime {
	case functiondomain.RuntimeScript:
		// 脚本在进程内执行，副作用由 SDK 当场落库并回填到 effects，
		// 因此这里拿到的已经是「实际发生了什么」而不是脚本的一面之词。
		result, business, err = s.executeScript(callCtx, function, version, eventID, caller, input)
		if result != nil {
			raw, _ = json.Marshal(result)
		}
	case functiondomain.RuntimeWASM:
		raw, err = s.sandbox.Execute(callCtx, version.WASMModule, payload, function.MaxResponseBytes)
		if err == nil {
			result, err = decodeFunctionResult(raw, eventID, version.Version)
			if err == nil {
				err = validateEffects(result.Effects, function.Capabilities)
			}
		}
	default:
		raw, err = s.http.Execute(callCtx, version.EndpointURL, version.ResponsePublicKey, payload, eventID, function.MaxResponseBytes)
		if err == nil {
			result, err = decodeFunctionResult(raw, eventID, version.Version)
			if err == nil {
				err = validateEffects(result.Effects, function.Capabilities)
			}
		}
	}
	duration := float64(time.Since(start).Microseconds()) / 1000
	invocation.DurationMs = duration
	if err != nil {
		invocation.Status = "error"
		invocation.ErrorMessage = truncateFunctionError(err.Error())
		_ = s.completeInvocation(invocation)
		// 脚本主动 aegis.fail() 属于业务判定（授权过期、次数用尽），
		// 要原样透传给调用方，不能混淆成「函数挂了」
		if business != nil {
			return nil, apperrors.New(business.Code, http.StatusForbidden, business.Message)
		}
		s.log.Warn("应用函数调用失败", zap.Int64("app_id", appID), zap.String("function", name), zap.Error(err))
		return nil, apperrors.New(50290, http.StatusBadGateway, "应用函数执行失败")
	}
	responseDigest := sha256.Sum256(raw)
	invocation.Status = "success"
	invocation.ResponseSHA256 = hex.EncodeToString(responseDigest[:])
	invocation.Result = result
	if err := s.completeInvocation(invocation); err != nil {
		return nil, err
	}
	return result, nil
}

// executeScript 运行服务端脚本。
//
// 与 wasm/http 两条路径的本质区别：脚本产生的副作用在执行过程中就已经通过 SDK
// 真实落库，返回的 effects 是这些操作的流水记录，会写进调用审计。
//
// 事务边界说明：每个 SDK 写操作各自原子（走对应服务的事务），但整个脚本不是一个
// 大事务 —— 脚本中途抛错时，先前已完成的写入不会回滚。因此脚本应当「先校验、
// 后写入」。所幸 eventId 幂等保证了失败调用不会被重放：同一 eventId 重试会直接
// 返回 40991，不会二次执行副作用。
func (s *AppFunctionService) executeScript(
	ctx context.Context,
	function *functiondomain.Function,
	version *functiondomain.Version,
	eventID string,
	caller functiondomain.Caller,
	input json.RawMessage,
) (*functiondomain.InvocationResult, *ScriptBusinessError, error) {
	if s.sdk.Points == nil || s.sdk.Vip == nil || s.sdk.Notifications == nil || s.sdk.Audit == nil {
		return nil, nil, errors.New("脚本运行时依赖未装配")
	}

	appKey := ""
	if app, err := s.pg.GetAppByID(ctx, function.AppID); err == nil && app != nil {
		appKey = app.AppKey
	}

	sdk := newScriptSDK(ctx, s.sdk, function.AppID, appKey, function.Name, eventID, caller, function.Capabilities)
	output, err := s.script.Execute(ctx, version.Source, functiondomain.ScriptContext{
		EventID: eventID, AppID: function.AppID, AppKey: appKey,
		Function: function.Name, Version: version.Version,
		Caller: caller, Input: input,
	}, sdk, function.MaxResponseBytes)
	if err != nil {
		return nil, sdk.BusinessError(), err
	}

	return &functiondomain.InvocationResult{
		EventID: eventID,
		Version: version.Version,
		Output:  output,
		Effects: sdk.Effects(),
	}, nil, nil
}

func (s *AppFunctionService) ListInvocations(ctx context.Context, appID int64, name string, limit int) ([]functiondomain.Invocation, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	return s.pg.ListAppFunctionInvocations(ctx, appID, function.ID, limit)
}

func (s *AppFunctionService) acquire(functionID int64) (func(), bool) {
	value, _ := s.concurrent.LoadOrStore(functionID, make(chan struct{}, 8))
	semaphore := value.(chan struct{})
	select {
	case semaphore <- struct{}{}:
		return func() { <-semaphore }, true
	default:
		return nil, false
	}
}

func (s *AppFunctionService) completeInvocation(invocation functiondomain.Invocation) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.pg.CompleteAppFunctionInvocation(ctx, invocation); err != nil {
		s.log.Error("记录应用函数调用结果失败",
			zap.Int64("app_id", invocation.AppID),
			zap.String("event_id", invocation.EventID),
			zap.Error(err))
		return err
	}
	return nil
}

func validateCapabilities(capabilities []string) error {
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if _, ok := allowedCapabilities[capability]; !ok {
			return apperrors.New(40087, http.StatusBadRequest, "包含不受支持的函数能力")
		}
		if _, duplicate := seen[capability]; duplicate {
			return apperrors.New(40086, http.StatusBadRequest, "函数能力不能重复")
		}
		seen[capability] = struct{}{}
	}
	return nil
}

func validateEffects(effects []functiondomain.Effect, capabilities []string) error {
	allowed := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		allowed[capability] = struct{}{}
	}
	for _, effect := range effects {
		if _, ok := allowed[effect.Type]; !ok {
			return errors.New("函数返回了未授权的 effect")
		}
		if len(effect.Arguments) == 0 || !json.Valid(effect.Arguments) {
			return errors.New("函数 effect 参数不是有效 JSON")
		}
	}
	return nil
}

func validateResponsePublicKey(value string) error {
	key, err := base64RawURLDecode(value)
	if err != nil || len(key) != 32 {
		return errors.New("responsePublicKey 必须是 Ed25519 公钥的 base64url 编码")
	}
	return nil
}

func base64RawURLDecode(value string) ([]byte, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for _, char := range strings.TrimSpace(value) {
		if !strings.ContainsRune(alphabet, char) {
			return nil, errors.New("invalid base64url")
		}
	}
	// 避免这里额外暴露多套编码格式。
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
}

func callerID(caller functiondomain.Caller) *int64 {
	if caller.UserID != nil {
		return caller.UserID
	}
	if caller.KeyID != nil {
		return caller.KeyID
	}
	return caller.AdminID
}

func clamp(value, min, max, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func truncateFunctionError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
