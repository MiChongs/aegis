package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	functiondomain "aegis/internal/domain/appfunction"
	apperrors "aegis/pkg/errors"
)

// Agent 工具集（远程函数场景）。
//
// 设计原则：
//   - 工具面 = 控制台已有的管理面，不多不少 —— Agent 能做的事管理员本来就能做，
//     权限因此不需要单独一套（HTTP 入口已经按 app:write 判过）；
//   - 读写分档：Mutating 工具（建函数 / 改设置 / 发版）可以被请求整体关掉，
//     关掉后 Agent 只剩「读、试跑、往编辑器放草稿」——不落库的那部分；
//   - 每个工具的输出都截断（aiToolOutputLimit）：一个失控的结果不该把
//     上下文与压缩预算一起吃光。

const (
	// aiToolOutputLimit 单个工具结果送进上下文的字符上限。
	aiToolOutputLimit = 24 << 10
	// aiToolSourceLimit 工具入参里脚本正文的字节上限（与函数正文上限对齐）。
	aiToolSourceLimit = 256 << 10
	// aiMCPToolPrefix MCP 工具的名字前缀：mcp__{server}__{tool}。
	aiMCPToolPrefix = "mcp__"
)

// aiAgentTool 一个内置工具。
type aiAgentTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	// Mutating 会落库的写操作，可被请求整体关闭。
	Mutating bool
	Execute  func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error)
}

// aiAgentRun 一轮 Agent 对话的运行态，工具执行时可读写。
type aiAgentRun struct {
	AppID   int64
	AdminID int64
	// Ref 场景锚点：function 场景下是函数名。
	Ref string
	// DraftSource 编辑器当前草稿。stage_source 会更新它，
	// analyze_draft / test_draft 缺省即检查它 —— Agent 的迭代闭环靠这个字段串起来。
	DraftSource string
	// StagedSource 本轮 stage_source 交付过的最新正文（前端据此更新编辑器）。
	StagedSource string

	functions *AppFunctionService
	providers *AIProviderService
	// mcpClients 本轮已建立的 MCP 客户端，键为服务器名。
	mcpClients map[string]*aiMCPClient
}

// aiFunctionTools 远程函数场景的完整内置工具集。
func aiFunctionTools() []aiAgentTool {
	return []aiAgentTool{
		{
			Name:        "list_functions",
			Description: "列出当前应用的全部远程函数（名称、状态、运行时、激活版本、能力、描述）。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, _ json.RawMessage) (any, error) {
				items, err := run.functions.ListFunctions(ctx, run.AppID)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(items))
				for _, item := range items {
					out = append(out, map[string]any{
						"name": item.Name, "status": item.Status, "runtime": item.Runtime,
						"activeVersion": item.ActiveVersion, "capabilities": item.Capabilities,
						"description": item.Description,
					})
				}
				return out, nil
			},
		},
		{
			Name: "get_function",
			Description: "读取一个函数的完整定义：状态、能力、闸门（超时/限流/并发）、函数配置、" +
				"入参契约与由它生成的 TypeScript 类型。name 缺省为当前函数。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前正在编辑的函数"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(args, &input)
				name := run.refOr(input.Name)
				function, err := run.functions.GetFunction(ctx, run.AppID, name)
				if err != nil {
					return nil, err
				}
				return function, nil
			},
		},
		{
			Name: "get_function_source",
			Description: "读取函数某个版本的脚本正文。version 缺省为激活版本；没有激活版本时报错并列出已有版本。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"version":{"type":"string","description":"版本号，缺省为激活版本"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				}
				_ = json.Unmarshal(args, &input)
				name := run.refOr(input.Name)
				version := strings.TrimSpace(input.Version)
				if version == "" {
					function, err := run.functions.GetFunction(ctx, run.AppID, name)
					if err != nil {
						return nil, err
					}
					if function.ActiveVersion == "" {
						versions, _ := run.functions.ListVersions(ctx, run.AppID, name)
						names := make([]string, 0, len(versions))
						for _, item := range versions {
							names = append(names, item.Version)
						}
						return nil, fmt.Errorf("函数 %s 没有激活版本；已有版本：%s", name, strings.Join(names, ", "))
					}
					version = function.ActiveVersion
				}
				detail, err := run.functions.GetVersionDetail(ctx, run.AppID, name, version)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"version": detail.Version.Version, "status": detail.Status,
					"notes": detail.Notes, "source": detail.Source,
				}, nil
			},
		},
		{
			Name:        "list_versions",
			Description: "列出函数的版本历史（版本号、状态、发版说明、大小、时间）。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(args, &input)
				return run.functions.ListVersions(ctx, run.AppID, run.refOr(input.Name))
			},
		},
		{
			Name: "get_capability_catalog",
			Description: "读取脚本能力目录：每项能力的键、用途、风险档、调用形态，以及运行时配额与限制。" +
				"判断「这个需求要声明哪些能力」时先看它。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, _ json.RawMessage) (any, error) {
				capabilities := functiondomain.CapabilityCatalog()
				out := make([]map[string]any, 0, len(capabilities))
				for _, capability := range capabilities {
					if capability.Deprecated {
						continue
					}
					out = append(out, map[string]any{
						"key": capability.Key, "label": capability.Label, "api": capability.API,
						"hint": capability.Hint, "risk": capability.Risk, "mutating": capability.Mutating,
						"requiresUser": capability.RequiresUser,
					})
				}
				return map[string]any{
					"capabilities": out,
					"limits":       FunctionRuntimeLimits(),
				}, nil
			},
		},
		{
			Name: "get_sdk_reference",
			Description: "生成当前函数（或指定能力集）可用的完整 TypeScript SDK 声明（aegis / ctx 的真实类型）。" +
				"写代码前先看它 —— 补全里没有的成员运行时也没有。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"capabilities":{"type":"array","items":{"type":"string"},"description":"直接指定能力集（忽略函数声明），用于评估「补上某能力后长什么样」"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name         string   `json:"name"`
					Capabilities []string `json:"capabilities"`
				}
				_ = json.Unmarshal(args, &input)
				capabilities := input.Capabilities
				var schema json.RawMessage
				if len(capabilities) == 0 {
					function, err := run.functions.GetFunction(ctx, run.AppID, run.refOr(input.Name))
					if err != nil {
						return nil, err
					}
					capabilities = function.Capabilities
					schema = function.InputSchema
				}
				return map[string]any{
					"capabilities": capabilities,
					"types":        run.functions.SDKTypes(capabilities, schema),
				}, nil
			},
		},
		{
			Name:        "list_script_templates",
			Description: "列出平台内置的脚本模板（标题、用途、依赖能力、正文），可作为新函数的起点。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, _ json.RawMessage) (any, error) {
				catalog := run.functions.Catalog()
				return catalog["templates"], nil
			},
		},
		{
			Name: "analyze_draft",
			Description: "对脚本做静态检查（与发布门禁同一套判定）：语法错误、未声明的能力、未知成员，" +
				"逐条带行号。source 缺省为编辑器当前草稿。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"source":{"type":"string","description":"要检查的脚本正文，缺省为编辑器当前草稿"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name   string `json:"name"`
					Source string `json:"source"`
				}
				_ = json.Unmarshal(args, &input)
				source, err := run.sourceOrDraft(input.Source)
				if err != nil {
					return nil, err
				}
				return run.functions.AnalyzeScript(ctx, run.AppID, run.refOr(input.Name), source)
			},
		},
		{
			Name: "test_draft",
			Description: "试跑脚本：读真写假（写操作只记入 effects 不执行），返回输出、日志、副作用清单、" +
				"错误位置与诊断。source 缺省为编辑器当前草稿。修改代码后必须试跑验证。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"source":{"type":"string","description":"脚本正文，缺省为编辑器当前草稿"},
				"input":{"description":"试跑入参（任意 JSON），缺省 {}"},
				"asUserId":{"type":"integer","description":"以某个应用用户的身份试跑；缺省以管理员身份"},
				"config":{"type":"object","description":"临时覆盖函数配置（不落库）"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name     string          `json:"name"`
					Source   string          `json:"source"`
					Input    json.RawMessage `json:"input"`
					AsUserID int64           `json:"asUserId"`
					Config   json.RawMessage `json:"config"`
				}
				_ = json.Unmarshal(args, &input)
				source, err := run.sourceOrDraft(input.Source)
				if err != nil {
					return nil, err
				}
				adminID := run.AdminID
				return run.functions.TestScript(ctx, run.AppID, run.refOr(input.Name), functiondomain.TestRequest{
					Source: source, Input: input.Input, Config: input.Config, AsUserID: input.AsUserID,
				}, &adminID)
			},
		},
		{
			Name: "stage_source",
			Description: "把一份完整的脚本正文放进作者的编辑器草稿（不落库、不发布）。这是把代码交给作者的唯一方式：" +
				"正文必须是完整脚本而不是片段。之后的 analyze_draft / test_draft 缺省即作用于这份草稿。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"source":{"type":"string","description":"完整的脚本正文"},
				"note":{"type":"string","description":"一句话说明这版改了什么"}},
				"required":["source"]}`),
			Execute: func(_ context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Source string `json:"source"`
					Note   string `json:"note"`
				}
				if err := json.Unmarshal(args, &input); err != nil {
					return nil, fmt.Errorf("入参不是合法 JSON：%w", err)
				}
				if strings.TrimSpace(input.Source) == "" {
					return nil, fmt.Errorf("source 不能为空")
				}
				if len(input.Source) > aiToolSourceLimit {
					return nil, fmt.Errorf("脚本超过 %d KB 上限", aiToolSourceLimit>>10)
				}
				run.DraftSource = input.Source
				run.StagedSource = input.Source
				return map[string]any{"ok": true, "bytes": len(input.Source), "note": input.Note}, nil
			},
		},
		{
			Name: "get_invocations",
			Description: "查函数的调用审计。排障时按 status=error 捞失败调用，返回错误消息、耗时、调用者与时间。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"status":{"type":"string","enum":["success","error","running"],"description":"按状态筛选"},
				"limit":{"type":"integer","description":"返回条数，缺省 10，上限 50"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name   string `json:"name"`
					Status string `json:"status"`
					Limit  int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &input)
				limit := input.Limit
				if limit <= 0 {
					limit = 10
				}
				if limit > 50 {
					limit = 50
				}
				page, err := run.functions.ListInvocations(ctx, run.AppID, run.refOr(input.Name),
					functiondomain.InvocationQuery{Status: input.Status, Page: 1, Limit: limit})
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(page.List))
				for _, item := range page.List {
					row := map[string]any{
						"eventId": item.EventID, "status": item.Status,
						"durationMs": item.DurationMs, "callerType": item.CallerType,
						"createdAt": item.CreatedAt.Format(time.RFC3339),
					}
					if item.ErrorMessage != "" {
						row["error"] = item.ErrorMessage
					}
					out = append(out, row)
				}
				return map[string]any{"total": page.Total, "list": out}, nil
			},
		},
		{
			Name:        "get_invocation_stats",
			Description: "函数近期运行统计：成功率、P95 / 平均耗时、错误 Top、按小时分桶的调用量。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"hours":{"type":"integer","description":"统计窗口小时数，缺省 24"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name  string `json:"name"`
					Hours int    `json:"hours"`
				}
				_ = json.Unmarshal(args, &input)
				hours := input.Hours
				if hours <= 0 {
					hours = 24
				}
				return run.functions.Stats(ctx, run.AppID, run.refOr(input.Name), hours)
			},
		},
		{
			Name: "browse_kv",
			Description: "浏览函数 KV 存储（脚本的服务端状态）。scope=app 应用共享，scope=user 按用户隔离（scopeId 为用户 ID）。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"scope":{"type":"string","enum":["app","user"],"description":"缺省 app"},
				"scopeId":{"type":"integer","description":"scope=user 时的用户 ID"},
				"prefix":{"type":"string","description":"键前缀过滤"},
				"limit":{"type":"integer","description":"返回条数，缺省 20，上限 100"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Scope   string `json:"scope"`
					ScopeID int64  `json:"scopeId"`
					Prefix  string `json:"prefix"`
					Limit   int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &input)
				if input.Scope == "" {
					input.Scope = functiondomain.KVScopeApp
				}
				limit := input.Limit
				if limit <= 0 {
					limit = 20
				}
				if limit > 100 {
					limit = 100
				}
				return run.functions.BrowseKV(ctx, run.AppID, functiondomain.KVQuery{
					Scope: input.Scope, ScopeID: input.ScopeID, Prefix: input.Prefix, Page: 1, Limit: limit,
				})
			},
		},

		// ── 写操作（可整体关闭）──
		{
			Name:     "create_function",
			Mutating: true,
			Description: "创建一个新的 script 运行时函数。创建后记得 stage_source 放入首版脚本、" +
				"publish_version 发布激活后才可被调用。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名（小写字母/数字/连字符）"},
				"description":{"type":"string"},
				"capabilities":{"type":"array","items":{"type":"string"},"description":"声明的能力键，见 get_capability_catalog"},
				"inputSchema":{"type":"object","description":"入参契约 JSON Schema，缺省不约束"},
				"timeoutMs":{"type":"integer","description":"单次执行超时毫秒，缺省 500"},
				"rateLimitPerMin":{"type":"integer","description":"每分钟调用上限，0 不限"}},
				"required":["name"]}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name            string          `json:"name"`
					Description     string          `json:"description"`
					Capabilities    []string        `json:"capabilities"`
					InputSchema     json.RawMessage `json:"inputSchema"`
					TimeoutMs       int             `json:"timeoutMs"`
					RateLimitPerMin int             `json:"rateLimitPerMin"`
				}
				if err := json.Unmarshal(args, &input); err != nil {
					return nil, fmt.Errorf("入参不是合法 JSON：%w", err)
				}
				adminID := run.AdminID
				return run.functions.CreateFunction(ctx, functiondomain.CreateFunctionInput{
					AppID: run.AppID, Name: input.Name, Description: input.Description,
					Runtime: functiondomain.RuntimeScript, Capabilities: input.Capabilities,
					TimeoutMs: input.TimeoutMs, RateLimitPerMin: input.RateLimitPerMin,
					InputSchema: input.InputSchema, CreatedBy: &adminID,
				})
			},
		},
		{
			Name:     "update_function_settings",
			Mutating: true,
			Description: "更新函数设置：能力声明、入参契约、描述、状态、闸门与函数配置。" +
				"只传要改的字段；capabilities 是整组替换。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"description":{"type":"string"},
				"status":{"type":"string","enum":["draft","active","disabled"]},
				"capabilities":{"type":"array","items":{"type":"string"},"description":"整组替换的能力键"},
				"inputSchema":{"type":"object","description":"入参契约 JSON Schema"},
				"config":{"type":"object","description":"函数配置（脚本里读作 aegis.config）"},
				"timeoutMs":{"type":"integer"},
				"rateLimitPerMin":{"type":"integer"},
				"maxConcurrency":{"type":"integer"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name            string          `json:"name"`
					Description     *string         `json:"description"`
					Status          *string         `json:"status"`
					Capabilities    []string        `json:"capabilities"`
					InputSchema     json.RawMessage `json:"inputSchema"`
					Config          json.RawMessage `json:"config"`
					TimeoutMs       *int            `json:"timeoutMs"`
					RateLimitPerMin *int            `json:"rateLimitPerMin"`
					MaxConcurrency  *int            `json:"maxConcurrency"`
				}
				if err := json.Unmarshal(args, &input); err != nil {
					return nil, fmt.Errorf("入参不是合法 JSON：%w", err)
				}
				return run.functions.UpdateFunction(ctx, run.AppID, run.refOr(input.Name), functiondomain.UpdateFunctionInput{
					Description: input.Description, Status: input.Status,
					Capabilities: input.Capabilities, InputSchema: input.InputSchema,
					Config: input.Config, TimeoutMs: input.TimeoutMs,
					RateLimitPerMin: input.RateLimitPerMin, MaxConcurrency: input.MaxConcurrency,
				})
			},
		},
		{
			Name:     "publish_version",
			Mutating: true,
			Description: "把脚本发布成一个不可变版本，可选立即激活。source 缺省为编辑器当前草稿。" +
				"发布前会走与控制台相同的静态门禁 —— 检查不过会点名第几行缺哪项能力。" +
				"除非作者明确要求发布，否则先 stage_source 交给作者确认。",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"name":{"type":"string","description":"函数名，缺省为当前函数"},
				"source":{"type":"string","description":"脚本正文，缺省为编辑器当前草稿"},
				"version":{"type":"string","description":"版本号，缺省自动按时间生成"},
				"notes":{"type":"string","description":"发版说明：这一版改了什么"},
				"activate":{"type":"boolean","description":"发布后立即激活，缺省 false"}}}`),
			Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
				var input struct {
					Name     string `json:"name"`
					Source   string `json:"source"`
					Version  string `json:"version"`
					Notes    string `json:"notes"`
					Activate bool   `json:"activate"`
				}
				if err := json.Unmarshal(args, &input); err != nil {
					return nil, fmt.Errorf("入参不是合法 JSON：%w", err)
				}
				name := run.refOr(input.Name)
				source, err := run.sourceOrDraft(input.Source)
				if err != nil {
					return nil, err
				}
				function, err := run.functions.GetFunction(ctx, run.AppID, name)
				if err != nil {
					return nil, err
				}
				version := strings.TrimSpace(input.Version)
				if version == "" {
					version = "v" + time.Now().Format("20060102-150405")
				}
				adminID := run.AdminID
				created, err := run.functions.CreateVersion(ctx, function, functiondomain.CreateVersionInput{
					Version: version, Source: source, Notes: input.Notes, CreatedBy: &adminID,
				})
				if err != nil {
					return nil, err
				}
				activated := false
				if input.Activate {
					if err := run.functions.ActivateVersion(ctx, run.AppID, name, created.Version); err != nil {
						return map[string]any{
							"version": created.Version, "activated": false,
							"warning": "版本已创建但激活失败：" + err.Error(),
						}, nil
					}
					activated = true
				}
				return map[string]any{"version": created.Version, "activated": activated}, nil
			},
		},
	}
}

// refOr 函数名参数缺省落回会话锚点。
func (run *aiAgentRun) refOr(name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return run.Ref
}

// sourceOrDraft 脚本正文参数缺省落回编辑器草稿。
func (run *aiAgentRun) sourceOrDraft(source string) (string, error) {
	if strings.TrimSpace(source) != "" {
		if len(source) > aiToolSourceLimit {
			return "", fmt.Errorf("脚本超过 %d KB 上限", aiToolSourceLimit>>10)
		}
		return source, nil
	}
	if strings.TrimSpace(run.DraftSource) == "" {
		return "", fmt.Errorf("编辑器里没有草稿：请先用 stage_source 放入完整脚本，或在入参里带上 source")
	}
	return run.DraftSource, nil
}

// executeAgentTool 统一的工具执行入口：内置工具查表，mcp__ 前缀转投 MCP 客户端。
// 返回值是**已经截断**的字符串结果 —— 送进模型上下文的从来不是原始对象。
func executeAgentTool(ctx context.Context, run *aiAgentRun, tools map[string]aiAgentTool,
	name string, args json.RawMessage) (string, error) {
	if strings.HasPrefix(name, aiMCPToolPrefix) {
		return executeMCPTool(ctx, run, name, args)
	}
	tool, ok := tools[name]
	if !ok {
		return "", fmt.Errorf("未知工具：%s", name)
	}
	result, err := tool.Execute(ctx, run, args)
	if err != nil {
		// 业务错误原样给模型 —— 「函数版本不存在」这类信息模型能自行纠正。
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return "", fmt.Errorf("%s", appErr.Message)
		}
		return "", err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("工具结果序列化失败：%w", err)
	}
	return truncateAIToolOutput(string(encoded)), nil
}

func executeMCPTool(ctx context.Context, run *aiAgentRun, name string, args json.RawMessage) (string, error) {
	rest := strings.TrimPrefix(name, aiMCPToolPrefix)
	serverKey, toolName, found := strings.Cut(rest, "__")
	if !found {
		return "", fmt.Errorf("MCP 工具名格式无效：%s", name)
	}
	client, ok := run.mcpClients[serverKey]
	if !ok {
		return "", fmt.Errorf("MCP 服务器不在本轮会话里：%s", serverKey)
	}
	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		return "", err
	}
	return truncateAIToolOutput(result), nil
}

func truncateAIToolOutput(output string) string {
	if len(output) <= aiToolOutputLimit {
		return output
	}
	return output[:aiToolOutputLimit] + fmt.Sprintf("\n…（结果过长，已截断，原始长度 %d 字符）", len(output))
}

// buildAgentToolList 把内置工具（按写开关过滤）与 MCP 工具合成统一的模型工具清单。
func buildAgentToolList(builtin []aiAgentTool, disableWrites bool, mcpTools map[string][]aidomain.MCPTool) []aidomain.Tool {
	tools := make([]aidomain.Tool, 0, len(builtin)+8)
	for _, tool := range builtin {
		if disableWrites && tool.Mutating {
			continue
		}
		tools = append(tools, aidomain.Tool{
			Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema,
		})
	}
	for serverKey, list := range mcpTools {
		for _, tool := range list {
			description := tool.Description
			if description == "" {
				description = "MCP 工具"
			}
			tools = append(tools, aidomain.Tool{
				Name:        aiMCPToolPrefix + serverKey + "__" + tool.Name,
				Description: fmt.Sprintf("[MCP·%s] %s", serverKey, description),
				InputSchema: tool.InputSchema,
			})
		}
	}
	return tools
}

// sanitizeMCPServerKey 服务器名进工具名前的清洗：工具名只允许 [A-Za-z0-9_-]。
func sanitizeMCPServerKey(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	key := strings.Trim(builder.String(), "-")
	if key == "" {
		key = "server"
	}
	if len(key) > 24 {
		key = key[:24]
	}
	return key
}
