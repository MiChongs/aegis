package appfunction

import (
	"encoding/json"
	"time"
)

const (
	// RuntimeWASM 纯计算沙箱：无宿主能力，适合确定性算法。
	RuntimeWASM = "wasm"
	// RuntimeHTTP 转发到接入方自建 HTTPS 端点，适合已有微服务。
	RuntimeHTTP = "http"
	// RuntimeScript 服务端 JS 脚本：跑在 Aegis 进程内，可通过受控 SDK 读写平台数据。
	// 这是自定义 API 的主路径 —— 逻辑与其依赖的状态都只存在于服务端。
	RuntimeScript = "script"

	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusDisabled = "disabled"

	VersionStaged  = "staged"
	VersionActive  = "active"
	VersionRetired = "retired"
)

// Function 是严格归属于单个应用的远程函数定义。
type Function struct {
	ID               int64    `json:"id"`
	AppID            int64    `json:"appId"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Runtime          string   `json:"runtime"`
	Status           string   `json:"status"`
	ActiveVersion    string   `json:"activeVersion,omitempty"`
	Capabilities     []string `json:"capabilities"`
	TimeoutMs        int      `json:"timeoutMs"`
	MaxRequestBytes  int      `json:"maxRequestBytes"`
	MaxResponseBytes int      `json:"maxResponseBytes"`
	// MaxConcurrency 单实例同时执行的上限。多实例部署时每个实例各自持有一份，
	// 因此它是「保护本进程」的闸门，不是全局配额 —— 全局配额用 RateLimitPerMin。
	MaxConcurrency int `json:"maxConcurrency"`
	// RateLimitPerMin 每分钟调用上限，0 表示不限。
	// 计数落在 app_function_kv 上（数据库原子自增），因此跨实例准确。
	RateLimitPerMin int `json:"rateLimitPerMin"`
	// Config 函数级参数，脚本里读作 `aegis.config`。
	// 它的价值是「改阈值不必发新版本」，因此永远不下发给接入方。
	Config json.RawMessage `json:"config"`
	// InputSchema 入参契约（JSON Schema）。`{}` 表示不约束。
	//
	// 与 Config 相反，它**是**要给接入方看的 —— 那是这个函数的 API 契约。
	// 一份声明同时驱动三处：调用入口的前置校验、控制台试跑输入框的补全与校验、
	// 以及编辑器里 `ctx.input` 的真实类型。三处各写一份的结果是它们很快就不一致，
	// 而不一致的表现是「补全里有这个字段、调用时却说它不该存在」。
	InputSchema json.RawMessage `json:"inputSchema"`
	// InputTypes 由 InputSchema 生成的 TypeScript 声明，只出网不入库。
	//
	// 与 schema 一起下发是刻意的冗余：控制台完全可以自己把 JSON Schema
	// 转成 TS，但那样就有了第二个转换器，而两个转换器迟早给出不同的类型 ——
	// 表现是「补全里有这个字段、运行时却没有」。同一条约束下，
	// 能力的类型片段也是由 Go 生成再下发的。
	InputTypes string `json:"inputTypes,omitempty"`
	// InputSample 按契约造出的示例 input，控制台拿它预填试跑输入框。
	// 与 InputTypes 同理：只出网不入库，且转换只有 Go 这一处实现。
	InputSample string `json:"inputSample,omitempty"`
	CreatedBy   *int64 `json:"createdBy,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Version 是不可变的函数发布版本。
//
// WASMModule 与 Source 都不通过面向接入方的 API 返回：脚本正文只对管理员可见，
// 客户端永远拿不到逻辑本身，这是「把逻辑放服务端」的前提。
type Version struct {
	ID                int64  `json:"id"`
	FunctionID        int64  `json:"functionId"`
	AppID             int64  `json:"appId"`
	Version           string `json:"version"`
	EndpointURL       string `json:"endpointUrl,omitempty"`
	ResponsePublicKey string `json:"responsePublicKey,omitempty"`
	WASMModule        []byte `json:"-"`
	Source            string `json:"-"`
	// Notes 发版说明，让版本列表回答得了「这一版改了什么」。
	Notes string `json:"notes"`
	// SourceBytes 脚本正文长度；列表里不带正文，但要能看出这一版有多大。
	SourceBytes    int        `json:"sourceBytes"`
	ArtifactSHA256 string     `json:"artifactSha256"`
	Status         string     `json:"status"`
	CreatedBy      *int64     `json:"createdBy,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ActivatedAt    *time.Time `json:"activatedAt,omitempty"`
}

// VersionDetail 是**仅限管理端**的版本视图，带脚本正文。
//
// 正文单独开一个类型而不是给 Version 的 Source 加 json tag：
// 那个字段会被列表、调用链路等多处序列化，一旦带上 tag，
// 「脚本永远不下发」这条保证就取决于每个消费方是否记得剔除它。
type VersionDetail struct {
	Version
	Source string `json:"source"`
}

// Contract 面向接入方的函数契约视图。
//
// 它是 Function 的投影而不是别名，字段逐个挑过：
//   - Config 永不出现 —— 里面是阈值与密钥，与脚本正文同级；
//   - Capabilities、内部 ID、审计字段不给 —— 接入方要的是「怎么调」，不是「怎么管」；
//   - InputSchema / InputSample / InputTypes 给全 —— 那正是这个函数的 API 契约，
//     调用入口按它做前置校验，接入方照它拼请求体。
type Contract struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Version 当前激活版本号。接入方无法选版本（调用永远落在激活版上），
	// 给它是为了让调用方的缓存与排障对得上号。
	Version string `json:"version"`
	// InputSchema 入参契约（JSON Schema），`{}` 表示不约束。
	InputSchema json.RawMessage `json:"inputSchema"`
	// InputTypes 由契约生成的 TypeScript 声明，TS 接入方可直接落地为类型文件。
	InputTypes string `json:"inputTypes,omitempty"`
	// InputSample 按契约生成、保证能通过校验的示例 input。
	InputSample      string `json:"inputSample,omitempty"`
	TimeoutMs        int    `json:"timeoutMs"`
	MaxRequestBytes  int    `json:"maxRequestBytes"`
	MaxResponseBytes int    `json:"maxResponseBytes"`
	// RateLimitPerMin 每分钟调用上限，0 表示不限。
	RateLimitPerMin int `json:"rateLimitPerMin"`
}

type CreateFunctionInput struct {
	AppID            int64
	Name             string
	Description      string
	Runtime          string
	Capabilities     []string
	TimeoutMs        int
	MaxRequestBytes  int
	MaxResponseBytes int
	MaxConcurrency   int
	RateLimitPerMin  int
	Config           json.RawMessage
	InputSchema      json.RawMessage
	CreatedBy        *int64
}

type UpdateFunctionInput struct {
	Description      *string
	Status           *string
	Capabilities     []string
	TimeoutMs        *int
	MaxRequestBytes  *int
	MaxResponseBytes *int
	MaxConcurrency   *int
	RateLimitPerMin  *int
	Config           json.RawMessage
	InputSchema      json.RawMessage
}

type CreateVersionInput struct {
	AppID             int64
	FunctionID        int64
	Version           string
	EndpointURL       string
	ResponsePublicKey string
	WASMModule        []byte
	Source            string
	Notes             string
	ArtifactSHA256    string
	CreatedBy         *int64
}

// ScriptContext 是注入脚本的 `ctx` 对象：调用元数据 + 调用者身份。
// 脚本读得到「谁在调用」，但要拿到这个人的状态必须走 SDK，由服务端现查。
type ScriptContext struct {
	EventID  string          `json:"eventId"`
	AppID    int64           `json:"appId"`
	AppKey   string          `json:"appKey"`
	Function string          `json:"function"`
	Version  string          `json:"version"`
	Caller   Caller          `json:"caller"`
	Input    json.RawMessage `json:"input"`
	// DryRun 控制台里的试跑。脚本读得到它，可以据此跳过不可逆的外部动作。
	DryRun bool `json:"dryRun"`
}

// KVScope 限定键值对的可见范围。
const (
	// KVScopeApp 应用级共享
	KVScopeApp = "app"
	// KVScopeUser 按调用者隔离，脚本无法跨用户读写
	KVScopeUser = "user"
)

// KVEntry 是脚本可用的服务端独占状态。
type KVEntry struct {
	Scope     string
	ScopeID   int64
	Key       string
	Value     json.RawMessage
	ExpiresAt *time.Time
}

type Caller struct {
	Type    string `json:"type"`
	UserID  *int64 `json:"userId,omitempty"`
	AdminID *int64 `json:"adminId,omitempty"`
	KeyID   *int64 `json:"keyId,omitempty"`
}

type InvocationRequest struct {
	EventID  string          `json:"eventId"`
	AppID    int64           `json:"appId"`
	Function string          `json:"function"`
	Version  string          `json:"version"`
	Caller   Caller          `json:"caller"`
	Input    json.RawMessage `json:"input"`
}

type Effect struct {
	Type      string          `json:"type"`
	Arguments json.RawMessage `json:"arguments"`
	// Simulated 试跑时为 true：这条副作用**没有真的发生**，只是记下了脚本想做什么。
	Simulated bool `json:"simulated,omitempty"`
}

type InvocationResult struct {
	EventID string          `json:"eventId"`
	Version string          `json:"version"`
	Output  json.RawMessage `json:"output,omitempty"`
	Effects []Effect        `json:"effects,omitempty"`
}

type Invocation struct {
	ID             int64             `json:"id"`
	EventID        string            `json:"eventId"`
	AppID          int64             `json:"appId"`
	FunctionID     int64             `json:"functionId"`
	VersionID      int64             `json:"versionId"`
	CallerType     string            `json:"callerType"`
	CallerID       *int64            `json:"callerId,omitempty"`
	Status         string            `json:"status"`
	DurationMs     float64           `json:"durationMs"`
	RequestSHA256  string            `json:"requestSha256"`
	ResponseSHA256 string            `json:"responseSha256,omitempty"`
	ErrorMessage   string            `json:"errorMessage,omitempty"`
	Result         *InvocationResult `json:"result,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
}

// Key 是接入 App 后端使用的函数调用凭据，数据库只保存摘要。
type Key struct {
	ID         int64      `json:"id"`
	AppID      int64      `json:"appId"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"`
	KeyHash    []byte     `json:"-"`
	Status     string     `json:"status"`
	CreatedBy  *int64     `json:"createdBy,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

type CreatedKey struct {
	Key
	Secret string `json:"secret"`
}

// InvocationQuery 调用审计的筛选条件。
//
// 之所以要有筛选：排障时看的从来不是「最近 50 条」，而是「失败的那几条」。
// 一个只按时间倒序取 50 条的列表，在一个每分钟几百次调用的函数上
// 永远也翻不到那条失败记录。
type InvocationQuery struct {
	// Status 为空表示不筛；取值 success / error / running
	Status string
	// CallerType 为空表示不筛；取值 user / admin / app
	CallerType string
	// EventID 精确匹配
	EventID string
	Page    int
	Limit   int
}

type InvocationPage struct {
	List  []Invocation `json:"list"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
}

// Stats 是一个函数近期的运行状况。
//
// 「有没有在跑、跑得对不对、跑得快不快」是三个问题，只给一份调用列表
// 回答不了任何一个 —— 看列表的人得自己数。
type Stats struct {
	WindowHours int `json:"windowHours"`
	Total       int64 `json:"total"`
	Success     int64 `json:"success"`
	Failed      int64 `json:"failed"`
	Running     int64 `json:"running"`
	// SuccessRate 0~1；Total 为 0 时为 0，展示端应显示「—」而不是 0%
	SuccessRate float64 `json:"successRate"`
	AvgMs       float64 `json:"avgMs"`
	P95Ms       float64 `json:"p95Ms"`
	MaxMs       float64 `json:"maxMs"`
	LastInvokedAt *time.Time `json:"lastInvokedAt,omitempty"`
	// TopErrors 出现次数最多的几种错误，排障从这里开始
	TopErrors []StatsError `json:"topErrors"`
	// Buckets 按小时分桶的调用量，供控制台画趋势
	Buckets []StatsBucket `json:"buckets"`
}

type StatsError struct {
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

type StatsBucket struct {
	At      time.Time `json:"at"`
	Success int64     `json:"success"`
	Failed  int64     `json:"failed"`
}

// TestRequest 控制台里的试跑。
//
// 试跑**不创建版本、不写调用审计、不产生真实副作用** ——
// 写操作只被记成 simulated effect。这样作者可以在发布之前反复迭代，
// 而不必先把半成品激活到线上。
// 能力刻意不在这里：试跑一律用函数**已声明**的能力。
// 允许请求侧临时加一项，会让「声明即授权」在这条链路上不成立 ——
// 而作者恰恰会在试跑通过之后直接发版，那时才发现少了一项。
// 函数设置现在可以随时改，先勾上再试跑即可。
type TestRequest struct {
	Source string
	Input  json.RawMessage
	// Config 临时覆盖函数配置。调一个阈值试试，不必先把它存进去。
	Config json.RawMessage
	// AsUserID 以某个应用用户的身份试跑；为 0 时以当前管理员身份。
	// 大多数脚本第一行就是 aegis.user.get()，没有这个字段试跑只能测到那句 fail。
	AsUserID  int64
	TimeoutMs int
}

// LogEntry 是脚本写下的一行日志。
//
// 结构化而不是一个拼好的字符串：控制台要按级别着色、按级别过滤，
// 而「从 "warn 内容" 里把级别切出来」这种解析在消息本身以 warn 开头时就会出错。
// ElapsedMs 是相对本次执行起点的毫秒数 —— 脚本里没有计时器，
// 「哪一步慢」只能靠日志之间的间隔看出来。
type LogEntry struct {
	Level     string  `json:"level"`
	Message   string  `json:"message"`
	ElapsedMs float64 `json:"elapsedMs"`
}

// 诊断级别。error 会挡住发布，warning / info 只提示。
const (
	DiagnosticError   = "error"
	DiagnosticWarning = "warning"
	DiagnosticInfo    = "info"
)

// Diagnostic 是发布前静态检查的一条结论。
//
// 带行列位置是刚需：编辑器要把它标在出问题的那一行上。一句
// 「脚本里用了没声明的能力」而不说是哪一行，在两百行的脚本里等于没说。
type Diagnostic struct {
	Severity string `json:"severity"`
	// Rule 规则标识（capability / unknown-member / forbidden-global / …），
	// 控制台据此决定要不要给「一键修复」。
	Rule    string `json:"rule"`
	Message string `json:"message"`
	// Line / Column 从 1 起算，与编辑器一致
	Line      int `json:"line"`
	Column    int `json:"column"`
	EndColumn int `json:"endColumn,omitempty"`
	// Capabilities 缺失的能力键：任意一项被声明即可满足。
	// 控制台拿它渲染「补上这项能力」按钮。
	Capabilities []string `json:"capabilities,omitempty"`
}

// AnalysisResult 静态检查的完整结论。
type AnalysisResult struct {
	OK          bool         `json:"ok"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	// UsedCapabilities 脚本实际用到的能力，控制台据此提示「勾了却没用到」
	UsedCapabilities []string `json:"usedCapabilities"`
	SourceBytes      int      `json:"sourceBytes"`
}

// TestResult 试跑结果。日志与副作用一并回传 —— 试跑的价值就在于看得见过程。
type TestResult struct {
	OK         bool            `json:"ok"`
	DurationMs float64         `json:"durationMs"`
	Output     json.RawMessage `json:"output,omitempty"`
	Effects    []Effect        `json:"effects"`
	Logs       []LogEntry      `json:"logs"`
	// Error 执行失败时的原因；BusinessCode 非 0 表示脚本自己调了 aegis.fail()
	Error        string `json:"error,omitempty"`
	BusinessCode int    `json:"businessCode,omitempty"`
	// ErrorLine / ErrorColumn 抛错位置。没有它作者只能对着一句
	// 「TypeError: Cannot read property 'x' of null」在两百行里找那个 null。
	ErrorLine   int `json:"errorLine,omitempty"`
	ErrorColumn int `json:"errorColumn,omitempty"`
	// Stack 调用栈（已剥离宿主帧）
	Stack []string `json:"stack,omitempty"`
	// Diagnostics 与发布门禁同一套静态检查，在试跑时顺带回一份 ——
	// 作者的下一步动作十有八九就是发布。
	Diagnostics []Diagnostic `json:"diagnostics"`
	// SDKCalls 本次用掉的额度，作者据此判断离上限还有多远
	SDKCalls     int `json:"sdkCalls"`
	SDKMutations int `json:"sdkMutations"`
	SDKFetches   int `json:"sdkFetches"`
}

// KVView 是 KV 浏览器里的一行。value 会被截断，键值存储不是用来存大对象的。
type KVView struct {
	Scope     string          `json:"scope"`
	ScopeID   int64           `json:"scopeId"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Truncated bool            `json:"truncated,omitempty"`
	ExpiresAt *time.Time      `json:"expiresAt,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type KVQuery struct {
	Scope   string
	ScopeID int64
	Prefix  string
	Page    int
	Limit   int
}

type KVPage struct {
	List  []KVView `json:"list"`
	Total int64    `json:"total"`
	Page  int      `json:"page"`
	Limit int      `json:"limit"`
}
