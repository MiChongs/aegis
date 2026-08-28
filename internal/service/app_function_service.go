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
	"fmt"
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
)

// 函数级闸门的取值范围。
const (
	minFunctionTimeoutMs     = 10
	maxFunctionTimeoutMs     = 30000
	defaultFunctionTimeoutMs = 500
	maxFunctionConcurrency   = 64
	defaultConcurrency       = 8
	maxFunctionRateLimit     = 600000
	// rateLimitKeyTTL 计数键的存活时间。取两分钟而不是一分钟：
	// 分钟桶在边界处会同时存在两个，早清会让跨边界那一刻的计数丢掉。
	rateLimitKeyTTL = 2 * time.Minute
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

// concurrencySlot 一个函数的并发闸门。
//
// 记住容量是必要的：`maxConcurrency` 现在可以在控制台上改，而 sync.Map 里
// 那个 channel 的容量是创建时定死的 —— 不比对容量的话，调大限额之后
// 仍然按旧值放行，控制台上显示 32、实际还是 8，且没有任何地方报错。
type concurrencySlot struct {
	semaphore chan struct{}
	capacity  int
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
//
// 收一个结构体而不是一串位置参数：这里已经有 7 个依赖，再加一个就会出现
// 「两个 *XxxService 位置写反、编译照过」这种只在运行时才发作的错误。
func (s *AppFunctionService) SetScriptDeps(deps ScriptSDKDeps) {
	deps.Log = s.log
	deps.PG = s.pg
	deps.HTTP = s.http
	s.sdk = deps
}

// ScriptDeps 暴露当前装配情况，供监控与自检读取（只读快照）。
func (s *AppFunctionService) ScriptDeps() ScriptSDKDeps { return s.sdk }

func (s *AppFunctionService) SigningPublicKey() string {
	return s.http.PublicKey()
}

// Catalog 下发能力目录、运行时额度与内置模板。
//
// 控制台的勾选框、编辑器的类型提示、模板选择器全部由它驱动 ——
// 新增一种能力只改 Go 侧的目录，前端零改动即自动出现。
func (s *AppFunctionService) Catalog() map[string]any {
	return map[string]any{
		"capabilities": functiondomain.CapabilityCatalog(),
		"limits":       FunctionRuntimeLimits(),
		"templates":    functiondomain.ScriptTemplates(),
		// 与能力无关、永远存在的那部分类型声明。控制台把它与各能力的
		// declaration 拼成喂给 Monaco 的 .d.ts —— 编辑器里出现什么，
		// 运行时就绑定了什么，这句话靠的是两边读同一份目录。
		"baseTypes":      functiondomain.BaseDeclaration,
		"runtimeDefault": functiondomain.RuntimeScript,
		// 入参契约的起步样例。给一份能直接改的骨架，而不是让作者
		// 对着一个空编辑器回忆 JSON Schema 的关键字怎么拼。
		"inputSchemaTemplate": functiondomain.InputSchemaTemplate,
	}
}

// SDKTypes 按已声明能力与入参契约生成喂给编辑器的完整 .d.ts。
func (s *AppFunctionService) SDKTypes(capabilities []string, inputSchema json.RawMessage) string {
	return functiondomain.SDKDeclarationWithInput(capabilities, inputSchema)
}

// decorateFunction 补上只出网、不入库的派生字段。
//
// 两项都是入参契约的投影：转成 TypeScript（编辑器的类型）与造一份示例
// input（试跑输入框的预填）。两个转换都只有 Go 这一处实现 ——
// 与能力的类型片段同一条约束，控制台再写一份就会出现「样例填出来的东西
// 通不过校验」这种自相矛盾的状态。
func decorateFunction(item *functiondomain.Function) *functiondomain.Function {
	if item == nil {
		return nil
	}
	item.InputTypes = functiondomain.InputSchemaDeclaration(item.InputSchema)
	item.InputSample = functiondomain.InputSchemaSample(item.InputSchema)
	return item
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
	if err := validateFunctionRuntime(input.Runtime); err != nil {
		return nil, err
	}
	if err := validateCapabilities(input.Capabilities); err != nil {
		return nil, err
	}
	input.Capabilities = functiondomain.NormalizeCapabilities(input.Capabilities)
	config, err := normalizeFunctionConfig(input.Config)
	if err != nil {
		return nil, err
	}
	input.Config = config
	schema, err := normalizeFunctionInputSchema(input.InputSchema)
	if err != nil {
		return nil, err
	}
	input.InputSchema = schema
	input.TimeoutMs = clamp(input.TimeoutMs, minFunctionTimeoutMs, maxFunctionTimeoutMs, defaultFunctionTimeoutMs)
	input.MaxRequestBytes = clamp(input.MaxRequestBytes, 1, 1<<20, 64<<10)
	input.MaxResponseBytes = clamp(input.MaxResponseBytes, 1, 1<<20, 64<<10)
	input.MaxConcurrency = clamp(input.MaxConcurrency, 1, maxFunctionConcurrency, defaultConcurrency)
	if input.RateLimitPerMin < 0 || input.RateLimitPerMin > maxFunctionRateLimit {
		return nil, apperrors.New(40104, http.StatusBadRequest,
			fmt.Sprintf("rateLimitPerMin 必须在 0 到 %d 之间（0 表示不限）", maxFunctionRateLimit))
	}
	created, err := s.pg.CreateAppFunction(ctx, input)
	if err != nil {
		return nil, err
	}
	return decorateFunction(created), nil
}

func (s *AppFunctionService) ListFunctions(ctx context.Context, appID int64) ([]functiondomain.Function, error) {
	items, err := s.pg.ListAppFunctions(ctx, appID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		decorateFunction(&items[index])
	}
	return items, nil
}

func (s *AppFunctionService) GetFunction(ctx context.Context, appID int64, name string) (*functiondomain.Function, error) {
	item, err := s.pg.GetAppFunction(ctx, appID, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40490, http.StatusNotFound, "应用函数不存在")
	}
	return decorateFunction(item), nil
}

// contractOf 把内部 Function 投影成接入方可见的契约。
//
// 只挑不敏感且调用时确实需要的字段。集中在这一处做投影，
// 而不是在 handler 里现挑 —— 那样每加一个下发点都要重新决定一次
// 「Config 给不给」，漏一次就是一次泄漏。
func contractOf(item *functiondomain.Function) functiondomain.Contract {
	return functiondomain.Contract{
		Name:             item.Name,
		Description:      item.Description,
		Version:          item.ActiveVersion,
		InputSchema:      item.InputSchema,
		InputTypes:       item.InputTypes,
		InputSample:      item.InputSample,
		TimeoutMs:        item.TimeoutMs,
		MaxRequestBytes:  item.MaxRequestBytes,
		MaxResponseBytes: item.MaxResponseBytes,
		RateLimitPerMin:  item.RateLimitPerMin,
	}
}

// ListContracts 返回接入方可调用的函数契约。
//
// 只含已启用且有激活版本的函数：草稿与停用状态对接入方而言不存在 ——
// 列出一个调了必回 40990 的名字，比不列它更误导人。
func (s *AppFunctionService) ListContracts(ctx context.Context, appID int64) ([]functiondomain.Contract, error) {
	items, err := s.ListFunctions(ctx, appID)
	if err != nil {
		return nil, err
	}
	contracts := make([]functiondomain.Contract, 0, len(items))
	for index := range items {
		item := &items[index]
		if item.Status != functiondomain.StatusActive || item.ActiveVersion == "" {
			continue
		}
		contracts = append(contracts, contractOf(item))
	}
	return contracts, nil
}

// GetContract 单个函数的调用契约。
//
// 未激活的函数返回 40990 而不是 404：调用方拿到的错误要与 invoke
// 的行为一致 —— 同一个名字，「查契约说不存在、调用说未激活」只会让人困惑。
func (s *AppFunctionService) GetContract(ctx context.Context, appID int64, name string) (*functiondomain.Contract, error) {
	item, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	if item.Status != functiondomain.StatusActive || item.ActiveVersion == "" {
		return nil, apperrors.New(40990, http.StatusConflict, "应用函数尚未激活")
	}
	contract := contractOf(item)
	return &contract, nil
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
		input.Capabilities = functiondomain.NormalizeCapabilities(input.Capabilities)
	}
	if input.TimeoutMs != nil && (*input.TimeoutMs < minFunctionTimeoutMs || *input.TimeoutMs > maxFunctionTimeoutMs) {
		return nil, apperrors.New(40093, http.StatusBadRequest,
			fmt.Sprintf("timeoutMs 必须在 %d 到 %d 之间", minFunctionTimeoutMs, maxFunctionTimeoutMs))
	}
	if input.MaxConcurrency != nil && (*input.MaxConcurrency < 1 || *input.MaxConcurrency > maxFunctionConcurrency) {
		return nil, apperrors.New(40103, http.StatusBadRequest,
			fmt.Sprintf("maxConcurrency 必须在 1 到 %d 之间", maxFunctionConcurrency))
	}
	if input.RateLimitPerMin != nil && (*input.RateLimitPerMin < 0 || *input.RateLimitPerMin > maxFunctionRateLimit) {
		return nil, apperrors.New(40104, http.StatusBadRequest,
			fmt.Sprintf("rateLimitPerMin 必须在 0 到 %d 之间（0 表示不限）", maxFunctionRateLimit))
	}
	if len(input.Config) > 0 {
		config, err := normalizeFunctionConfig(input.Config)
		if err != nil {
			return nil, err
		}
		input.Config = config
	}
	if len(input.InputSchema) > 0 {
		schema, err := normalizeFunctionInputSchema(input.InputSchema)
		if err != nil {
			return nil, err
		}
		input.InputSchema = schema
	}
	item, err := s.pg.UpdateAppFunction(ctx, appID, strings.ToLower(strings.TrimSpace(name)), input)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40490, http.StatusNotFound, "应用函数不存在")
	}
	// 并发闸门的容量可能变了，丢掉旧的让下一次调用按新容量重建
	s.concurrent.Delete(item.ID)
	return decorateFunction(item), nil
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
	input.Notes = strings.TrimSpace(input.Notes)
	if len(input.Notes) > 2000 {
		input.Notes = input.Notes[:2000]
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
		// 再过一遍静态检查。语法过了不等于跑得起来：调了没声明的能力，
		// 运行时是一句「Cannot read property 'add' of undefined」，
		// 而那要等到真实调用才出现 —— 这正是版本不可变最难受的地方，
		// 发现时只能再发一版。挡在这里的代价是一次报错，收益是不用回滚。
		if analysis := AnalyzeFunctionScript(input.Source, function.Capabilities); !analysis.OK {
			return nil, apperrors.New(40108, http.StatusBadRequest,
				describeBlockingDiagnostics(analysis.Diagnostics))
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

// GetVersionDetail 取一个版本的完整内容，**含脚本正文**。
//
// 只对管理端开放。没有它的话，改一行脚本要从零重写整份 —— 「版本不可变」
// 说的是发出去的那一版不能被改，不是作者拿不回自己写过的东西。
func (s *AppFunctionService) GetVersionDetail(ctx context.Context, appID int64, name, version string) (*functiondomain.VersionDetail, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	item, err := s.pg.GetAppFunctionVersion(ctx, appID, function.ID, strings.TrimSpace(version))
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40491, http.StatusNotFound, "函数版本不存在")
	}
	return &functiondomain.VersionDetail{Version: *item, Source: item.Source}, nil
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

// DeleteVersion 删除一个未激活的版本。
//
// 激活中的版本删不掉：那会让 active_version 指向一条不存在的记录，
// 调用时表现为 40992，而版本列表上看不出任何异常。
func (s *AppFunctionService) DeleteVersion(ctx context.Context, appID int64, name, version string) error {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return err
	}
	count, err := s.pg.DeleteAppFunctionVersion(ctx, appID, function.ID, strings.TrimSpace(version))
	if err != nil {
		return err
	}
	if count == 0 {
		return apperrors.New(40994, http.StatusConflict, "版本不存在，或它正处于激活状态（先激活另一个版本再删）")
	}
	return nil
}

// TestScript 在不创建版本、不写调用审计、不产生真实副作用的前提下跑一遍脚本。
//
// 没有它的时候，验证一行改动的唯一方式是「建版本 → 激活 → 调用」——
// 也就是把半成品推到线上，而且每改一次就多一条永久版本记录。
//
// 读是真的、写只记录：脚本的分支几乎全部由服务端状态决定（是不是会员、
// 额度用了多少），喂假数据跑出来的结论没有意义；而真写会让「试一下」
// 变成一次不可撤销的线上操作。
//
// 能力用函数**已声明**的那一份，请求侧不能临时加 —— 否则试跑通过、
// 发版之后才发现少声明一项，而那正是试跑本该拦住的事。
func (s *AppFunctionService) TestScript(
	ctx context.Context,
	appID int64,
	name string,
	request functiondomain.TestRequest,
	adminID *int64,
) (*functiondomain.TestResult, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	if function.Runtime != functiondomain.RuntimeScript {
		return nil, apperrors.New(40106, http.StatusBadRequest, "只有 script 运行时支持试跑")
	}
	if err := s.requireScriptDeps(); err != nil {
		return nil, err
	}
	if err := s.script.Validate(request.Source); err != nil {
		return nil, apperrors.New(40101, http.StatusBadRequest, err.Error())
	}
	input := request.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if !json.Valid(input) {
		return nil, apperrors.New(40089, http.StatusBadRequest, "函数输入不是有效 JSON")
	}
	// 试跑同样过一遍入参契约，而且是**先于执行**。
	//
	// 放行的话，作者会用一份线上根本进不来的 input 把脚本调通，
	// 然后在真实调用里撞上 40109 —— 而试跑存在的全部意义就是复现真实调用。
	// 与「试跑只用已声明的能力」是同一条取舍。
	if err := validateFunctionInput(function.InputSchema, input); err != nil {
		return nil, err
	}

	caller := functiondomain.Caller{Type: "admin", AdminID: adminID}
	if request.AsUserID > 0 {
		user, err := s.pg.GetUserByID(ctx, request.AsUserID)
		if err != nil {
			return nil, err
		}
		if user == nil || user.AppID != appID {
			return nil, apperrors.New(40105, http.StatusBadRequest, "试跑指定的用户不属于当前应用")
		}
		userID := user.ID
		caller = functiondomain.Caller{Type: "user", UserID: &userID}
	}

	config := function.Config
	if len(request.Config) > 0 {
		normalized, err := normalizeFunctionConfig(request.Config)
		if err != nil {
			return nil, err
		}
		config = normalized
	}

	timeout := clamp(request.TimeoutMs, minFunctionTimeoutMs, maxFunctionTimeoutMs, function.TimeoutMs)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	appKey := s.resolveAppKey(ctx, appID)
	eventID := uuid.NewString()
	sdk := newScriptSDK(runCtx, s.sdk, appID, appKey, function.Name, eventID, caller,
		function.Capabilities, scriptSDKOptions{Config: config, DryRun: true})
	defer sdk.Release()

	start := time.Now()
	output, err := s.script.Execute(runCtx, request.Source, functiondomain.ScriptContext{
		EventID: eventID, AppID: appID, AppKey: appKey,
		Function: function.Name, Version: "(dry-run)",
		Caller: caller, Input: input, DryRun: true,
	}, sdk, function.MaxResponseBytes)

	calls, mutations, fetches := sdk.Usage()
	result := &functiondomain.TestResult{
		OK:         err == nil,
		DurationMs: float64(time.Since(start).Microseconds()) / 1000,
		Effects:    sdk.Effects(),
		Logs:       sdk.Logs(),
		// 顺带回一份静态检查：作者试跑通过之后的下一个动作十有八九是发布，
		// 而发布会被同一套检查挡下 —— 在这里先说出来，免得他以为是发布坏了。
		Diagnostics: AnalyzeFunctionScript(request.Source, function.Capabilities).Diagnostics,
		SDKCalls:    calls, SDKMutations: mutations, SDKFetches: fetches,
	}
	if result.Effects == nil {
		result.Effects = []functiondomain.Effect{}
	}
	if result.Logs == nil {
		result.Logs = []functiondomain.LogEntry{}
	}
	if err != nil {
		// 试跑失败是**正常结果**而不是接口错误：作者要的是错误内容，
		// 以及在那之前打的日志与本该发生的副作用。回 4xx 会让前端
		// 只拿到一句错误消息，日志和 effects 全部丢掉。
		result.Error = truncateFunctionError(err.Error())
		result.ErrorLine, result.ErrorColumn, result.Stack = scriptErrorPosition(err)
		if business := sdk.BusinessError(); business != nil {
			result.BusinessCode = business.Code
			result.Error = business.Message
		}
		return result, nil
	}
	result.Output = output
	return result, nil
}

// AnalyzeScript 只做静态检查，不执行任何代码。
//
// 与试跑的分工：试跑要一个用户身份、要真实读库、要几百毫秒；而作者在
// 敲代码的过程中需要的只是「我现在这份能不能发出去」。把这件事从试跑里
// 拆出来，编辑器才能在每次停顿时问一遍，而不是等作者主动点「试跑」。
func (s *AppFunctionService) AnalyzeScript(
	ctx context.Context,
	appID int64,
	name string,
	source string,
) (*functiondomain.AnalysisResult, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	if function.Runtime != functiondomain.RuntimeScript {
		return nil, apperrors.New(40106, http.StatusBadRequest, "只有 script 运行时支持静态检查")
	}
	if len(source) > maxScriptSourceBytes {
		return nil, apperrors.New(40101, http.StatusBadRequest,
			fmt.Sprintf("脚本超过 %d KB 上限", maxScriptSourceBytes>>10))
	}
	analysis := AnalyzeFunctionScript(source, function.Capabilities)
	// 语法错误由编译器给位置，词法扫描给不出来 —— 两者合成一份诊断，
	// 编辑器只消费一个列表。
	if syntaxError := s.script.CompileCheck(source); syntaxError != nil {
		analysis.Diagnostics = append([]functiondomain.Diagnostic{*syntaxError}, analysis.Diagnostics...)
		analysis.OK = false
	}
	return &analysis, nil
}

// describeBlockingDiagnostics 把挡住发布的那几条拼成一句人能直接照做的话。
//
// 只说「静态检查未通过」等于让作者自己去点一次检查再看列表 ——
// 而他此刻正盯着的是这个发布对话框。
func describeBlockingDiagnostics(diagnostics []functiondomain.Diagnostic) string {
	blocking := BlockingDiagnostics(diagnostics)
	if len(blocking) == 0 {
		return "脚本静态检查未通过"
	}
	parts := make([]string, 0, len(blocking))
	for index, diagnostic := range blocking {
		if index >= 3 {
			parts = append(parts, fmt.Sprintf("…另有 %d 处", len(blocking)-index))
			break
		}
		parts = append(parts, fmt.Sprintf("第 %d 行：%s", diagnostic.Line, diagnostic.Message))
	}
	return "脚本静态检查未通过 —— " + strings.Join(parts, "；")
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
	// 入参契约排在幂等之前：一个形状就不对的请求不该占用一个 eventId，
	// 否则调用方改对参数之后重试，会撞上「这个 eventId 已经失败过」。
	if err := validateFunctionInput(function.InputSchema, input); err != nil {
		return nil, err
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

	// 频次闸门排在幂等重放之后：一次网络重试拿回既有结果，不该再吃一次配额。
	if err := s.enforceRateLimit(ctx, function); err != nil {
		return nil, err
	}

	version, err := s.pg.GetAppFunctionVersion(ctx, appID, function.ID, function.ActiveVersion)
	if err != nil {
		return nil, err
	}
	if version == nil || version.Status != functiondomain.VersionActive {
		return nil, apperrors.New(40992, http.StatusConflict, "激活版本不可用")
	}
	release, ok := s.acquire(function.ID, function.MaxConcurrency)
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
	if err := s.requireScriptDeps(); err != nil {
		return nil, nil, err
	}

	appKey := s.resolveAppKey(ctx, function.AppID)
	sdk := newScriptSDK(ctx, s.sdk, function.AppID, appKey, function.Name, eventID, caller,
		function.Capabilities, scriptSDKOptions{Config: function.Config})
	// 脚本被超时中断时它自己那句 release 不会执行，锁只能靠这里归还。
	defer sdk.Release()
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

// requireScriptDeps 检查脚本运行时的依赖是否装配齐全。
//
// 逐项点名而不是一句「未装配」：这条错误只会在部署或重构之后出现一次，
// 而那一次要能立刻看出漏了哪个服务。
func (s *AppFunctionService) requireScriptDeps() error {
	missing := make([]string, 0, 4)
	if s.sdk.Points == nil {
		missing = append(missing, "积分")
	}
	if s.sdk.Vip == nil {
		missing = append(missing, "会员")
	}
	if s.sdk.Notifications == nil {
		missing = append(missing, "通知")
	}
	if s.sdk.Audit == nil {
		missing = append(missing, "审计")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("脚本运行时依赖未装配：%s", strings.Join(missing, "、"))
}

func (s *AppFunctionService) resolveAppKey(ctx context.Context, appID int64) string {
	if app, err := s.pg.GetAppByID(ctx, appID); err == nil && app != nil {
		return app.AppKey
	}
	return ""
}

func (s *AppFunctionService) ListInvocations(
	ctx context.Context,
	appID int64,
	name string,
	query functiondomain.InvocationQuery,
) (*functiondomain.InvocationPage, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	return s.pg.ListAppFunctionInvocations(ctx, appID, function.ID, query)
}

func (s *AppFunctionService) Stats(ctx context.Context, appID int64, name string, windowHours int) (*functiondomain.Stats, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	return s.pg.AppFunctionStats(ctx, appID, function.ID, windowHours)
}

// BrowseKV 管理端的 KV 浏览器。
//
// 脚本的全部「服务端独占状态」都落在这张表上，而排障时最常问的一句是
// 「这个用户的配额计数现在是多少」。没有这个视图，唯一的回答方式是
// 临时写一个脚本去读它 —— 而那本身就是一次真实的副作用。
func (s *AppFunctionService) BrowseKV(ctx context.Context, appID int64, query functiondomain.KVQuery) (*functiondomain.KVPage, error) {
	if query.Scope != "" && query.Scope != functiondomain.KVScopeApp && query.Scope != functiondomain.KVScopeUser {
		return nil, apperrors.New(40107, http.StatusBadRequest, "KV 作用域仅支持 app 或 user")
	}
	return s.pg.BrowseAppFunctionKV(ctx, appID, query)
}

// DeleteKV 删掉一个键。「把这个用户的计数清零再试一次」是排障最常见的动作。
func (s *AppFunctionService) DeleteKV(ctx context.Context, appID int64, scope string, scopeID int64, key string) error {
	if scope != functiondomain.KVScopeApp && scope != functiondomain.KVScopeUser {
		return apperrors.New(40107, http.StatusBadRequest, "KV 作用域仅支持 app 或 user")
	}
	removed, err := s.pg.DeleteAppFunctionKV(ctx, appID, scope, scopeID, strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if !removed {
		return apperrors.New(40493, http.StatusNotFound, "键不存在")
	}
	return nil
}

// enforceRateLimit 每分钟调用上限。
//
// 计数落在 app_function_kv 上而不是各实例的内存里：内存计数在多实例部署下
// 的表现是「配了 60/分钟，实际放行 60×实例数」，而控制台上完全看不出来 ——
// 一个看起来在防、其实没防的闸门比没有这个闸门更糟。
//
// 键带平台保留前缀，脚本读写不到，因此没法把限制自己的那个计数清零。
func (s *AppFunctionService) enforceRateLimit(ctx context.Context, function *functiondomain.Function) error {
	if function.RateLimitPerMin <= 0 {
		return nil
	}
	key := fmt.Sprintf("%srate:%d:%d", kvReservedPrefix, function.ID, time.Now().Unix()/60)
	used, err := s.pg.IncrAppFunctionKV(ctx, function.AppID,
		functiondomain.KVScopeApp, 0, key, 1, rateLimitKeyTTL)
	if err != nil {
		// 计数失败时放行：限流是保护措施，让它的故障演变成全站不可用
		// 是本末倒置（与防火墙、登录基线同一取向）。
		s.log.Warn("函数频次计数失败，本次放行",
			zap.Int64("app_id", function.AppID),
			zap.String("function", function.Name), zap.Error(err))
		return nil
	}
	if used > int64(function.RateLimitPerMin) {
		return apperrors.New(42991, http.StatusTooManyRequests,
			fmt.Sprintf("函数调用频次超过每分钟 %d 次的限制", function.RateLimitPerMin))
	}
	return nil
}

// acquire 取一个并发名额。容量变了就按新容量重建闸门。
func (s *AppFunctionService) acquire(functionID int64, capacity int) (func(), bool) {
	if capacity <= 0 {
		capacity = defaultConcurrency
	}
	slot := s.concurrencySlot(functionID, capacity)
	select {
	case slot.semaphore <- struct{}{}:
		return func() { <-slot.semaphore }, true
	default:
		return nil, false
	}
}

func (s *AppFunctionService) concurrencySlot(functionID int64, capacity int) *concurrencySlot {
	if value, ok := s.concurrent.Load(functionID); ok {
		if existing, ok := value.(*concurrencySlot); ok && existing.capacity == capacity {
			return existing
		}
	}
	fresh := &concurrencySlot{semaphore: make(chan struct{}, capacity), capacity: capacity}
	// LoadOrStore 保证并发创建时只有一个胜出；输的一方沿用赢家那个，
	// 否则两个请求会各拿一个独立闸门，限额等于翻倍。
	actual, loaded := s.concurrent.LoadOrStore(functionID, fresh)
	if !loaded {
		return fresh
	}
	existing, ok := actual.(*concurrencySlot)
	if ok && existing.capacity == capacity {
		return existing
	}
	// 容量确实变了（刚改过设置）：覆盖成新的。
	s.concurrent.Store(functionID, fresh)
	return fresh
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

// normalizeFunctionConfig 校验函数级配置：必须是 JSON 对象且不超过上限。
//
// 只接受对象是刻意的：脚本里读作 `aegis.config.xxx`，顶层是数组或标量时
// 那句取值恒为 undefined —— 而那种失败不会报错，只会让阈值静默变成默认值。
func normalizeFunctionConfig(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`), nil
	}
	if len(trimmed) > maxConfigBytes {
		return nil, apperrors.New(40102, http.StatusBadRequest,
			fmt.Sprintf("函数配置超过 %d KB 上限", maxConfigBytes>>10))
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil, apperrors.New(40102, http.StatusBadRequest, "函数配置必须是 JSON 对象")
	}
	encoded, err := json.Marshal(probe)
	if err != nil {
		return nil, apperrors.New(40102, http.StatusBadRequest, "函数配置必须是 JSON 对象")
	}
	return encoded, nil
}

// normalizeFunctionInputSchema 校验入参契约。
//
// 不只是「是不是 JSON 对象」，还要**真的编译一遍**：一份编译不过的 schema
// 在调用时的表现是「校验永远抛错」或者更糟 ——「校验被跳过」。
// 两种都不会在保存时暴露，而这份 schema 一旦保存就会作用于每一次真实调用。
func normalizeFunctionInputSchema(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`), nil
	}
	if len(trimmed) > maxConfigBytes {
		return nil, apperrors.New(40110, http.StatusBadRequest,
			fmt.Sprintf("入参 schema 超过 %d KB 上限", maxConfigBytes>>10))
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil, apperrors.New(40110, http.StatusBadRequest, "入参 schema 必须是 JSON 对象")
	}
	if len(probe) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if err := compileInputSchema(probe); err != nil {
		return nil, apperrors.New(40110, http.StatusBadRequest, "入参 schema 不可用："+err.Error())
	}
	encoded, err := json.Marshal(probe)
	if err != nil {
		return nil, apperrors.New(40110, http.StatusBadRequest, "入参 schema 必须是 JSON 对象")
	}
	return encoded, nil
}

// validateFunctionInput 按入参契约校验一次调用的 input。
//
// 放在执行**之前**：没有它的时候，接入方少传一个字段的表现是脚本在第三行
// 抛 TypeError，而调用方拿到的是 50290「应用函数执行失败」—— 一个既不说
// 少了什么、也不说是自己传错了的错误，双方都只能靠猜。
func validateFunctionInput(schema json.RawMessage, input json.RawMessage) error {
	definition := decodeSchemaObject(schema)
	if definition == nil {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return apperrors.New(40089, http.StatusBadRequest, "函数输入不是有效 JSON")
	}
	problems := validateAgainstSchema(definition, decoded)
	if len(problems) == 0 {
		return nil
	}
	// 逐条列出而不是只说第一条：调用方改一处再试一次、又冒出一条，
	// 这个来回在跨团队接入时是以天计的。
	if len(problems) > 5 {
		problems = append(problems[:5], fmt.Sprintf("…另有 %d 处", len(problems)-5))
	}
	return apperrors.New(40109, http.StatusBadRequest,
		"函数入参不符合契约："+strings.Join(problems, "；"))
}

func decodeSchemaObject(raw json.RawMessage) map[string]any {
	if !functiondomain.HasInputSchema(raw) {
		return nil
	}
	var definition map[string]any
	if err := json.Unmarshal(raw, &definition); err != nil {
		return nil
	}
	return definition
}

// validateFunctionRuntime 校验运行时取值。
//
// 单独提出来是因为它出过一次影响面很大的错：这里曾经只放行 wasm / http，
// 而 script 恰恰是控制台上的默认选项，于是「创建远程函数」这个表单按默认值
// 提交必然拿回 40091「仅支持 wasm 或 http」—— 整个功能从第一步就走不通，
// 而错误文案里根本没提 script，看到的人只会以为自己选错了运行时。
// TestCreateFunctionAcceptsScriptRuntime 钉死这一条。
func validateFunctionRuntime(runtime string) error {
	switch runtime {
	case functiondomain.RuntimeScript, functiondomain.RuntimeWASM, functiondomain.RuntimeHTTP:
		return nil
	}
	return apperrors.New(40091, http.StatusBadRequest, "函数运行时仅支持 script、wasm 或 http")
}

func validateCapabilities(capabilities []string) error {
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if !functiondomain.IsKnownCapability(capability) {
			return apperrors.New(40087, http.StatusBadRequest,
				fmt.Sprintf("不支持的函数能力：%s", capability))
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
	for _, capability := range functiondomain.NormalizeCapabilities(capabilities) {
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
