package httptransport

// ── 应用级第三方登录（OAuth2）渠道配置 DTO ──

// AppOAuthProviderRequest 管理端新建 / 更新渠道配置。
//
// clientSecret 留空表示保持原密钥不变（编辑表单不回填密钥）；
// 需要清空时传 clearClientSecret=true。
type AppOAuthProviderRequest struct {
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

// AppOAuthProviderEnabledRequest 列表页一键启停。
type AppOAuthProviderEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// AppOAuthProviderReorderRequest 按数组顺序重排登录页展示次序。
type AppOAuthProviderReorderRequest struct {
	Providers []string `json:"providers" binding:"required"`
}

// AppOAuthBindingQuery 管理端绑定记录查询。
type AppOAuthBindingQuery struct {
	Provider string `form:"provider"`
	UserID   int64  `form:"userId"`
	Keyword  string `form:"keyword"`
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
}

// AdminUnbindOAuthRequest 管理端强制解绑。
type AdminUnbindOAuthRequest struct {
	UserID   int64  `json:"userId" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	// Force 跳过"解绑后无法登录"的保护性校验
	Force bool `json:"force"`
}

// OAuthProvidersQuery 登录页拉取可用渠道。
type OAuthProvidersQuery struct {
	AppID int64 `form:"appid" binding:"required"`
}

// OAuthBindURLRequest 已登录用户发起第三方账号绑定。
type OAuthBindURLRequest struct {
	Provider string `json:"provider" binding:"required"`
	DeviceID string `json:"deviceId"`
	Device   string `json:"device"`
	MarkCode string `json:"markCode"`
}
