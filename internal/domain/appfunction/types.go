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
	ID               int64     `json:"id"`
	AppID            int64     `json:"appId"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Runtime          string    `json:"runtime"`
	Status           string    `json:"status"`
	ActiveVersion    string    `json:"activeVersion,omitempty"`
	Capabilities     []string  `json:"capabilities"`
	TimeoutMs        int       `json:"timeoutMs"`
	MaxRequestBytes  int       `json:"maxRequestBytes"`
	MaxResponseBytes int       `json:"maxResponseBytes"`
	CreatedBy        *int64    `json:"createdBy,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Version 是不可变的函数发布版本。
//
// WASMModule 与 Source 都不通过面向接入方的 API 返回：脚本正文只对管理员可见，
// 客户端永远拿不到逻辑本身，这是「把逻辑放服务端」的前提。
type Version struct {
	ID                int64      `json:"id"`
	FunctionID        int64      `json:"functionId"`
	AppID             int64      `json:"appId"`
	Version           string     `json:"version"`
	EndpointURL       string     `json:"endpointUrl,omitempty"`
	ResponsePublicKey string     `json:"responsePublicKey,omitempty"`
	WASMModule        []byte     `json:"-"`
	Source            string     `json:"-"`
	ArtifactSHA256    string     `json:"artifactSha256"`
	Status            string     `json:"status"`
	CreatedBy         *int64     `json:"createdBy,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	ActivatedAt       *time.Time `json:"activatedAt,omitempty"`
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
	CreatedBy        *int64
}

type UpdateFunctionInput struct {
	Description      *string
	Status           *string
	Capabilities     []string
	TimeoutMs        *int
	MaxRequestBytes  *int
	MaxResponseBytes *int
}

type CreateVersionInput struct {
	AppID             int64
	FunctionID        int64
	Version           string
	EndpointURL       string
	ResponsePublicKey string
	WASMModule        []byte
	Source            string
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
