package httptransport

// ListObjectsQuery 文件列表查询参数
type ListObjectsQuery struct {
	ConfigID *int64 `form:"configId"`
	AppID    *int64 `form:"appId"`
	// Prefix 递归前缀（兼容旧调用方）
	Prefix string `form:"prefix"`
	// Folder 目录路径；FolderView=true 时只列该目录下的直接子文件并返回子目录
	Folder     string `form:"folder"`
	FolderView bool   `form:"folderView"`
	Keyword    string `form:"keyword"`
	// ContentType 支持 `image/` 这样的大类前缀写法
	ContentType  string   `form:"contentType"`
	Status       string   `form:"status"`
	Statuses     []string `form:"statuses"`
	UploaderType string   `form:"uploaderType"`
	UploadedBy   *int64   `form:"uploadedBy"`
	MinSize      *int64   `form:"minSize"`
	MaxSize      *int64   `form:"maxSize"`
	// CreatedFrom / CreatedTo 接受 RFC3339；控制台发的是日期选择器的结果
	CreatedFrom string `form:"createdFrom"`
	CreatedTo   string `form:"createdTo"`
	Sort        string `form:"sort"`
	Order       string `form:"order"`
	Page        int    `form:"page"`
	Limit       int    `form:"limit"`
}

// BatchObjectsRequest 批量操作请求
type BatchObjectsRequest struct {
	IDs    []int64 `json:"ids" binding:"required"`
	Action string  `json:"action" binding:"required"`
}

// ObjectAccessLinkRequest 为单个对象签发访问链接
type ObjectAccessLinkRequest struct {
	// Download 为 true 时代理下载会带 Content-Disposition: attachment
	Download bool `json:"download"`
	// ExpiresIn 有效期（秒），服务端上限 1 小时
	ExpiresIn int `json:"expiresIn"`
}

// CreateRuleRequest 创建存储规则请求
type CreateRuleRequest struct {
	ConfigID *int64         `json:"configId,omitempty"`
	AppID    *int64         `json:"appId,omitempty"`
	Name     string         `json:"name" binding:"required"`
	RuleType string         `json:"ruleType" binding:"required"`
	RuleData map[string]any `json:"ruleData"`
}

// UpdateRuleRequest 更新存储规则请求
type UpdateRuleRequest struct {
	Name     *string         `json:"name,omitempty"`
	RuleData *map[string]any `json:"ruleData,omitempty"`
	IsActive *bool           `json:"isActive,omitempty"`
}

// UpsertCDNConfigRequest 创建或更新 CDN 配置请求
type UpsertCDNConfigRequest struct {
	CDNDomain        string   `json:"cdnDomain"`
	CDNProtocol      string   `json:"cdnProtocol"`
	CacheMaxAge      int      `json:"cacheMaxAge"`
	RefererWhitelist []string `json:"refererWhitelist"`
	RefererBlacklist []string `json:"refererBlacklist"`
	IPWhitelist      []string `json:"ipWhitelist"`
	SignURLEnabled   bool     `json:"signUrlEnabled"`
	SignURLSecret    string   `json:"signUrlSecret"`
	SignURLTTL       int      `json:"signUrlTtl"`
}

// CreateImageRuleRequest 创建图片处理规则请求
type CreateImageRuleRequest struct {
	ConfigID *int64         `json:"configId,omitempty"`
	Name     string         `json:"name" binding:"required"`
	RuleType string         `json:"ruleType" binding:"required"`
	RuleData map[string]any `json:"ruleData"`
}

// CleanupTrashRequest 清理回收站请求
type CleanupTrashRequest struct {
	OlderThanDays int `json:"olderThanDays"`
}
