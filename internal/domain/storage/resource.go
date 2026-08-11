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
	ID        int64          `json:"id"`
	ConfigID  *int64         `json:"configId,omitempty"`
	AppID     *int64         `json:"appId,omitempty"`
	Name      string         `json:"name"`
	RuleType  string         `json:"ruleType"` // upload_limit, file_type, path_pattern, quota
	RuleData  map[string]any `json:"ruleData"`
	IsActive  bool           `json:"isActive"`
	CreatedAt time.Time      `json:"createdAt"`
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

// 对象列表的排序字段白名单。取值直接来自控制台的列头，不落库、不进 SQL 拼接，
// 由 repository 映射成列名 —— 允许前端传列名等于把 ORDER BY 开放给调用方。
const (
	ObjectSortCreatedAt = "createdAt"
	ObjectSortDeletedAt = "deletedAt"
	ObjectSortSize      = "size"
	ObjectSortFileName  = "fileName"
	ObjectSortObjectKey = "objectKey"
)

// ObjectListQuery 文件列表查询参数
type ObjectListQuery struct {
	ConfigID *int64 `json:"configId,omitempty"`
	AppID    *int64 `json:"appId,omitempty"`
	// Prefix 递归前缀匹配（object_key LIKE prefix%），与 Folder 正交
	Prefix string `json:"prefix,omitempty"`
	// Folder 目录定位（不带首尾斜杠，空串即根目录）
	Folder string `json:"folder,omitempty"`
	// FolderView 目录浏览模式：只返回 Folder 下的**直接**子文件，配套返回子目录列表，
	// 让控制台能像文件管理器那样逐层下钻。关闭时 Folder 退化成普通的递归前缀，
	// 递归也是默认值 —— 后台任务（用量快照等）拿到的必须是全量而不是根目录那一层。
	FolderView bool `json:"folderView,omitempty"`
	// Keyword 文件名 / 对象键模糊匹配（大小写不敏感）
	Keyword string `json:"keyword,omitempty"`
	// ContentType 支持两种写法：完整类型（image/png）精确匹配，
	// 以 `/` 结尾的大类（image/）按前缀匹配。控制台的类型筛选发的是后者。
	ContentType  string     `json:"contentType,omitempty"`
	Status       string     `json:"status,omitempty"`
	Statuses     []string   `json:"statuses,omitempty"`
	UploaderType string     `json:"uploaderType,omitempty"`
	UploadedBy   *int64     `json:"uploadedBy,omitempty"`
	MinSize      *int64     `json:"minSize,omitempty"`
	MaxSize      *int64     `json:"maxSize,omitempty"`
	CreatedFrom  *time.Time `json:"createdFrom,omitempty"`
	CreatedTo    *time.Time `json:"createdTo,omitempty"`
	Sort         string     `json:"sort,omitempty"`
	Order        string     `json:"order,omitempty"`
	Page         int        `json:"page"`
	Limit        int        `json:"limit"`
}

// ObjectFolder 对象键前缀聚合出的「目录」。
// 存储服务本身没有目录概念，这里是按 object_key 的斜杠分段现算的视图。
type ObjectFolder struct {
	Name      string `json:"name"`   // 目录名（当前层级的那一段）
	Path      string `json:"path"`   // 完整路径（可直接作为下一次查询的 folder）
	FileCount int64  `json:"fileCount"`
	TotalSize int64  `json:"totalSize"`
}

// ObjectListSummary 当前筛选条件下的汇总。
// 刻意**不含 status 条件** —— 它要回答的是「这个目录里有多少活跃、多少在回收站」，
// 加上 status 之后另一档恒为 0，那条信息就没了。
type ObjectListSummary struct {
	TotalFiles   int64 `json:"totalFiles"`
	TotalSize    int64 `json:"totalSize"`
	ActiveFiles  int64 `json:"activeFiles"`
	ActiveSize   int64 `json:"activeSize"`
	DeletedFiles int64 `json:"deletedFiles"`
	DeletedSize  int64 `json:"deletedSize"`
}

// ObjectListResult 列表 + 目录 + 汇总的一次性返回。
// 三者由同一组筛选条件推导，分三个接口取会出现「文件已翻页、目录还是上一页的」。
type ObjectListResult struct {
	Items   []StorageObject   `json:"items"`
	Folders []ObjectFolder    `json:"folders"`
	Total   int64             `json:"total"`
	Page    int               `json:"page"`
	Limit   int               `json:"limit"`
	Summary ObjectListSummary `json:"summary"`
}

// 批量动作
const (
	BatchActionDelete  = "delete"  // 移入回收站（软删）
	BatchActionRestore = "restore" // 从回收站恢复
	BatchActionPurge   = "purge"   // 永久删除（限超管）
)

// BatchObjectResult 批量操作结果
type BatchObjectResult struct {
	Action    string `json:"action"`
	Requested int    `json:"requested"`
	Affected  int64  `json:"affected"`
	// Skipped 请求了但状态不满足前置条件的条数（例如恢复一个本来就是活跃的对象）。
	// 与 Affected 分开报，否则「点了没反应」和「本来就不需要动」分不清。
	Skipped int64 `json:"skipped"`
}

// ObjectAccessLink 管理端为单个已索引对象签发的访问链接
type ObjectAccessLink struct {
	ObjectID    int64     `json:"objectId"`
	ConfigID    int64     `json:"configId"`
	Provider    string    `json:"provider"`
	ObjectKey   string    `json:"objectKey"`
	URL         string    `json:"url"`
	Download    bool      `json:"download"`
	ContentType string    `json:"contentType"`
	ExpiresAt   time.Time `json:"expiresAt"`
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
