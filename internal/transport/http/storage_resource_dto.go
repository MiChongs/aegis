package httptransport

// ListObjectsQuery 文件列表查询参数
type ListObjectsQuery struct {
	ConfigID    *int64 `form:"configId"`
	AppID       *int64 `form:"appId"`
	Prefix      string `form:"prefix"`
	ContentType string `form:"contentType"`
	Status      string `form:"status"`
	Page        int    `form:"page"`
	Limit       int    `form:"limit"`
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
