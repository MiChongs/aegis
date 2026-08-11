// Package oauth 定义应用级第三方登录（OAuth2）渠道的领域类型。
//
// 层级关系：
//   - 每个 App 在 app_oauth_providers 表中维护自己的渠道列表（Provider）；
//   - App 未配置某渠道时，回落到平台级 .env（OAUTH_*）配置，Source 标记为 platform；
//   - Template 是内置渠道模板目录，供管理端"一键添加"时自动填充端点与 scope。
package oauth

import "time"

// 适配器类型：决定 token 交换与用户信息解析的协议差异
const (
	KindGeneric   = "generic"   // 标准 OAuth2 / OIDC
	KindQQ        = "qq"        // QQ 互联（openid 二段式获取）
	KindWechat    = "wechat"    // 微信（appid/secret 参数名、openid 随 token 返回）
	KindWeibo     = "weibo"     // 微博（uid 随 token 返回）
	KindGitHub    = "github"    // GitHub（邮箱需二次拉取）
	KindMicrosoft = "microsoft" // Microsoft Graph / Entra ID
)

// token 端点凭据传递方式
const (
	TokenAuthAuto   = "auto"   // 先按表单参数尝试，遇到 invalid_client 再用 Basic 重试
	TokenAuthParams = "params" // client_id / client_secret 放在表单
	TokenAuthBasic  = "basic"  // Authorization: Basic base64(id:secret)
)

// 用户信息端点凭据传递方式
const (
	UserInfoAuthHeader = "header" // Authorization: Bearer
	UserInfoAuthQuery  = "query"  // ?access_token=
)

// 配置来源
const (
	SourceApp      = "app"      // 应用级配置（app_oauth_providers）
	SourcePlatform = "platform" // 平台级 .env 兜底
)

// 字段映射支持的键
const (
	MappingID       = "id"
	MappingNickname = "nickname"
	MappingAvatar   = "avatar"
	MappingEmail    = "email"
	MappingUnionID  = "unionId"
)

// Provider 一个 App 下的单个第三方登录渠道配置。
//
// ClientSecret 只在服务端内部流转（json:"-"）；管理端读取时通过
// ClientSecretSet / ClientSecretHint 判断是否已配置，永不回传明文。
type Provider struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appId"`
	Provider    string `json:"provider"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Enabled     bool   `json:"enabled"`

	ClientID         string `json:"clientId"`
	ClientSecret     string `json:"-"`
	ClientSecretSet  bool   `json:"clientSecretSet"`
	ClientSecretHint string `json:"clientSecretHint,omitempty"`

	RedirectURL       string            `json:"redirectUrl"`
	AuthURL           string            `json:"authUrl"`
	TokenURL          string            `json:"tokenUrl"`
	UserInfoURL       string            `json:"userInfoUrl"`
	Scopes            []string          `json:"scopes"`
	TokenAuthStyle    string            `json:"tokenAuthStyle"`
	UserInfoAuthStyle string            `json:"userInfoAuthStyle"`
	ProfileMapping    map[string]string `json:"profileMapping"`
	ExtraAuthParams   map[string]string `json:"extraAuthParams"`

	AllowLogin    bool   `json:"allowLogin"`
	AllowRegister bool   `json:"allowRegister"`
	AllowBind     bool   `json:"allowBind"`
	SortOrder     int    `json:"sortOrder"`
	Remark        string `json:"remark,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// ── 以下为运行时计算字段，不落库 ──

	Source   string   `json:"source"`             // app / platform
	Bindings int64    `json:"bindings"`           // 已绑定该渠道的用户数
	Ready    bool     `json:"ready"`              // 配置是否完整可用
	Issues   []string `json:"issues,omitempty"`   // 阻碍启用的具体缺失项
	Warnings []string `json:"warnings,omitempty"` // 不阻碍启用但值得提醒的问题
}

// SaveInput 管理端写入渠道配置的入参。
//
// ClientSecret 为空表示"保持原值不变"（编辑时前端不回填密钥）；
// 需要清空时显式传 ClearClientSecret=true。
type SaveInput struct {
	Provider          string            `json:"provider"`
	Kind              string            `json:"kind"`
	DisplayName       string            `json:"displayName"`
	Icon              string            `json:"icon"`
	Color             string            `json:"color"`
	Enabled           bool              `json:"enabled"`
	ClientID          string            `json:"clientId"`
	ClientSecret      string            `json:"clientSecret"`
	ClearClientSecret bool              `json:"clearClientSecret"`
	RedirectURL       string            `json:"redirectUrl"`
	AuthURL           string            `json:"authUrl"`
	TokenURL          string            `json:"tokenUrl"`
	UserInfoURL       string            `json:"userInfoUrl"`
	Scopes            []string          `json:"scopes"`
	TokenAuthStyle    string            `json:"tokenAuthStyle"`
	UserInfoAuthStyle string            `json:"userInfoAuthStyle"`
	ProfileMapping    map[string]string `json:"profileMapping"`
	ExtraAuthParams   map[string]string `json:"extraAuthParams"`
	AllowLogin        *bool             `json:"allowLogin"`
	AllowRegister     *bool             `json:"allowRegister"`
	AllowBind         *bool             `json:"allowBind"`
	SortOrder         *int              `json:"sortOrder"`
	Remark            string            `json:"remark"`
}

// PublicProvider 登录页渲染用的最小信息集，不含任何凭据。
type PublicProvider struct {
	Provider    string `json:"provider"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	AllowLogin  bool   `json:"allowLogin"`
	AllowBind   bool   `json:"allowBind"`
	SortOrder   int    `json:"sortOrder"`
}

// TemplateField 模板里需要人工填写字段的说明，用于前端表单提示。
type TemplateField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// Template 内置渠道模板：管理端点一下即可预填端点、scope 与品牌信息。
type Template struct {
	Key               string          `json:"key"`
	Kind              string          `json:"kind"`
	Name              string          `json:"name"`
	Icon              string          `json:"icon"`
	Color             string          `json:"color"`
	Category          string          `json:"category"`
	Description       string          `json:"description"`
	DocsURL           string          `json:"docsUrl,omitempty"`
	ConsoleURL        string          `json:"consoleUrl,omitempty"`
	AuthURL           string          `json:"authUrl"`
	TokenURL          string          `json:"tokenUrl"`
	UserInfoURL       string          `json:"userInfoUrl"`
	Scopes            []string        `json:"scopes"`
	TokenAuthStyle    string          `json:"tokenAuthStyle"`
	UserInfoAuthStyle string          `json:"userInfoAuthStyle"`
	RequiresEndpoints bool            `json:"requiresEndpoints"` // 端点需人工填写（自定义 / OIDC）
	Fields            []TemplateField `json:"fields,omitempty"`
	Notes             []string        `json:"notes,omitempty"`
}

// Resolved 运行期解析结果：AuthService 用它构造实际的 OAuth 适配器。
type Resolved struct {
	Provider      string
	Kind          string
	DisplayName   string
	Source        string
	AllowLogin    bool
	AllowRegister bool
	AllowBind     bool

	ClientID          string
	ClientSecret      string
	RedirectURL       string
	AuthURL           string
	TokenURL          string
	UserInfoURL       string
	Scopes            []string
	TokenAuthStyle    string
	UserInfoAuthStyle string
	ProfileMapping    map[string]string
	ExtraAuthParams   map[string]string
}

// TestResult 连通性自检结果。
type TestResult struct {
	Provider     string         `json:"provider"`
	Ready        bool           `json:"ready"`
	Issues       []string       `json:"issues,omitempty"`
	Warnings     []string       `json:"warnings,omitempty"`
	AuthorizeURL string         `json:"authorizeUrl,omitempty"`
	Endpoints    []Endpoint     `json:"endpoints"`
	CheckedAt    time.Time      `json:"checkedAt"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// Endpoint 单个端点的探测结果。
type Endpoint struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	Status    int    `json:"status,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Binding 用户与第三方账号的绑定记录（管理端 / 用户端共用）。
type Binding struct {
	ID             int64     `json:"id"`
	AppID          int64     `json:"appId"`
	UserID         int64     `json:"userId"`
	Account        string    `json:"account,omitempty"`
	Provider       string    `json:"provider"`
	DisplayName    string    `json:"displayName,omitempty"`
	Icon           string    `json:"icon,omitempty"`
	ProviderUserID string    `json:"providerUserId"`
	UnionID        string    `json:"unionId,omitempty"`
	Nickname       string    `json:"nickname,omitempty"`
	Avatar         string    `json:"avatar,omitempty"`
	Email          string    `json:"email,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// BindingQuery 管理端绑定列表的查询条件。
type BindingQuery struct {
	AppID    int64
	Provider string
	UserID   int64
	Keyword  string
	Page     int
	PageSize int
}

// BindingPage 分页结果。
type BindingPage struct {
	Items    []Binding `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}
