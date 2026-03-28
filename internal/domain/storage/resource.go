package storage

import "time"

// StorageObject 存储对象索引记录
type StorageObject struct {
	ID           int64          `json:"id"`
	ConfigID     int64          `json:"configId"`
	AppID        *int64         `json:"appId,omitempty"`
	ObjectKey    string         `json:"objectKey"`
	FileName     string         `json:"fileName"`
	ContentType  string         `json:"contentType"`
	Size         int64          `json:"size"`
	ETag         string         `json:"etag"`
	UploadedBy   *int64         `json:"uploadedBy,omitempty"`
	UploaderType string         `json:"uploaderType"` // user / admin
	Status       string         `json:"status"`       // active / deleted / pending_review
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"createdAt"`
	DeletedAt    *time.Time     `json:"deletedAt,omitempty"`
}

// StorageRule 存储规则
type StorageRule struct {
	ID       int64          `json:"id"`
	ConfigID *int64         `json:"configId,omitempty"`
	AppID    *int64         `json:"appId,omitempty"`
	Name     string         `json:"name"`
	RuleType string         `json:"ruleType"` // upload_limit, file_type, path_pattern, quota
	RuleData map[string]any `json:"ruleData"`
	IsActive bool           `json:"isActive"`
	CreatedAt time.Time     `json:"createdAt"`
}

// CDNConfig CDN 与防盗链配置
type CDNConfig struct {
	ID               int64     `json:"id"`
	ConfigID         int64     `json:"configId"`
	CDNDomain        string    `json:"cdnDomain"`
	CDNProtocol      string    `json:"cdnProtocol"`
	CacheMaxAge      int       `json:"cacheMaxAge"`
	RefererWhitelist []string  `json:"refererWhitelist"`
	RefererBlacklist []string  `json:"refererBlacklist"`
	IPWhitelist      []string  `json:"ipWhitelist"`
	SignURLEnabled   bool      `json:"signUrlEnabled"`
	SignURLSecret    string    `json:"signUrlSecret,omitempty"`
	SignURLTTL       int       `json:"signUrlTtl"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// ImageRule 图片处理规则
type ImageRule struct {
	ID        int64          `json:"id"`
	ConfigID  *int64         `json:"configId,omitempty"`
	Name      string         `json:"name"`
	RuleType  string         `json:"ruleType"` // thumbnail, watermark, format_convert
	RuleData  map[string]any `json:"ruleData"`
	IsActive  bool           `json:"isActive"`
	CreatedAt time.Time      `json:"createdAt"`
}

// UsageSnapshot 用量快照
type UsageSnapshot struct {
	ID           int64     `json:"id"`
	ConfigID     int64     `json:"configId"`
	AppID        *int64    `json:"appId,omitempty"`
	TotalFiles   int64     `json:"totalFiles"`
	TotalSize    int64     `json:"totalSize"`
	ActiveFiles  int64     `json:"activeFiles"`
	DeletedFiles int64     `json:"deletedFiles"`
	SnapshotAt   time.Time `json:"snapshotAt"`
}

// ObjectListQuery 文件列表查询参数
type ObjectListQuery struct {
	ConfigID    *int64 `json:"configId,omitempty"`
	AppID       *int64 `json:"appId,omitempty"`
	Prefix      string `json:"prefix,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Status      string `json:"status,omitempty"`
	Page        int    `json:"page"`
	Limit       int    `json:"limit"`
}

// UsageStats 用量统计
type UsageStats struct {
	ConfigID     int64      `json:"configId"`
	TotalFiles   int64      `json:"totalFiles"`
	TotalSize    int64      `json:"totalSize"`
	ActiveFiles  int64      `json:"activeFiles"`
	DeletedFiles int64      `json:"deletedFiles"`
	TopTypes     []TypeStat `json:"topTypes"`
}

// TypeStat 文件类型统计
type TypeStat struct {
	ContentType string `json:"contentType"`
	Count       int64  `json:"count"`
	Size        int64  `json:"size"`
}

// ── 输入类型 ──

type CreateRuleInput struct {
	ConfigID *int64         `json:"configId,omitempty"`
	AppID    *int64         `json:"appId,omitempty"`
	Name     string         `json:"name"`
	RuleType string         `json:"ruleType"`
	RuleData map[string]any `json:"ruleData"`
}

type CreateImageRuleInput struct {
	ConfigID *int64         `json:"configId,omitempty"`
	Name     string         `json:"name"`
	RuleType string         `json:"ruleType"`
	RuleData map[string]any `json:"ruleData"`
}

type UpsertCDNConfigInput struct {
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
