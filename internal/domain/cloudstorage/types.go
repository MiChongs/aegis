// Package cloudstorage 应用级用户云存储。
//
// 每个用户在应用内有一块私有空间，按「命名空间 / 键」存放任意文档：收藏夹、偏好设置、
// 草稿、游戏存档……接入方自己决定命名空间怎么分。服务端只关心四件事：
//
//	内容放哪       交给应用解析出的存储配置（与 /storage/upload 同一套），数据库只存索引
//	写入谁赢       修订号乐观并发：带 ifRevision 写入，不一致回 409 + 服务端当前版本
//	写错了怎么办   每次写入留一个修订，可回滚；删除先进回收站，过了保留期才真正清除
//	占了多少       按用户计配额（含历史修订与回收站），管理员可逐人覆盖与冻结
package cloudstorage

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// 内容编码：客户端写入时声明，读取时按同一种编码交回。
const (
	// EncodingJSON 内容是一个 JSON 值，原样落盘、原样交回（不重新序列化，字段顺序不变）。
	EncodingJSON = "json"
	// EncodingText 内容是 UTF-8 文本，请求里是一个 JSON 字符串。
	EncodingText = "text"
	// EncodingBase64 任意二进制，请求里是 base64 字符串，落盘的是解码后的原始字节。
	EncodingBase64 = "base64"
)

// 修订来源。
const (
	SourceWrite    = "write"
	SourceUpload   = "upload"
	SourceRollback = "rollback"
	SourceAdmin    = "admin"
)

// 条目状态筛选。
const (
	StatusActive  = "active"
	StatusDeleted = "deleted"
	StatusAll     = "all"
)

// 默认值与硬上限。
//
// 硬上限不是拍脑袋：单条目上限受网关请求体上限约束（multipart 32 MiB，
// JSON 8 MiB 且 base64 会膨胀 4/3），配额上限则是为了不让一次误填把一个应用的
// 存储桶交给单个用户。
const (
	DefaultQuotaBytes         int64 = 20 << 20
	DefaultMaxItemBytes       int64 = 1 << 20
	DefaultMaxItems                 = 500
	DefaultMaxRevisions             = 10
	DefaultTrashRetentionDays       = 30

	MaxQuotaBytes         int64 = 10 << 30
	MaxItemBytesCap       int64 = 25 << 20
	MaxItemsCap                 = 100000
	MaxRevisionsCap             = 100
	MaxTrashRetentionDays       = 365
	MaxNamespaces               = 50

	// InlineContentLimit 读取条目时内联返回内容的上限；更大的内容走下载链接。
	InlineContentLimit int64 = 4 << 20
	// JSONWriteLimit JSON 请求体写入的内容上限（解码后）。网关 JSON 请求体上限是 8 MiB，
	// base64 膨胀之后 6 MiB 的原始字节恰好装得下；更大的内容请走 multipart 上传。
	JSONWriteLimit int64 = 6 << 20
)

var (
	namespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	keyPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	contentTypeChars = regexp.MustCompile(`^[A-Za-z0-9!#$&^_.+\-]+/[A-Za-z0-9!#$&^_.+\-]+(\s*;\s*[A-Za-z0-9_.\-]+=[A-Za-z0-9_.\-"]+)*$`)
)

// ValidNamespace 命名空间：小写字母数字开头，可含 . _ -，至多 64 位。
// 它会出现在对象键与 URL 路径里，所以字符集刻意收窄。
func ValidNamespace(value string) bool { return namespacePattern.MatchString(value) }

// ValidKey 条目键：字母数字开头，可含 . _ -，至多 128 位。不允许斜杠 ——
// 层级请用命名空间表达，键里的斜杠会让路径参数与对象键各自产生歧义。
func ValidKey(value string) bool { return keyPattern.MatchString(value) }

// ValidEncoding 是否是受支持的编码。
func ValidEncoding(value string) bool {
	switch value {
	case EncodingJSON, EncodingText, EncodingBase64:
		return true
	}
	return false
}

// ValidContentType 粗校验 MIME 类型的形状（type/subtype[; param=value]）。
func ValidContentType(value string) bool {
	return len(value) <= 128 && contentTypeChars.MatchString(value)
}

// DefaultContentType 编码对应的默认内容类型。
func DefaultContentType(encoding string) string {
	switch encoding {
	case EncodingText:
		return "text/plain; charset=utf-8"
	case EncodingBase64:
		return "application/octet-stream"
	default:
		return "application/json"
	}
}

// Namespace 命名空间目录中的一项。名称只用于展示。
type Namespace struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Config 应用级配置。
type Config struct {
	AppID              int64       `json:"appid"`
	Enabled            bool        `json:"enabled"`
	StorageConfigName  string      `json:"storageConfigName"`
	QuotaBytes         int64       `json:"quotaBytes"`
	MaxItemBytes       int64       `json:"maxItemBytes"`
	MaxItems           int         `json:"maxItems"`
	MaxRevisions       int         `json:"maxRevisions"`
	TrashRetentionDays int         `json:"trashRetentionDays"`
	RestrictNamespaces bool        `json:"restrictNamespaces"`
	Namespaces         []Namespace `json:"namespaces"`
	UpdatedBy          string      `json:"updatedBy,omitempty"`
	UpdatedAt          *time.Time  `json:"updatedAt,omitempty"`
	// Configured 是否在库里存过。没存过的应用拿到的是一份默认值（未启用）。
	Configured bool `json:"configured"`
}

// DefaultConfig 没配置过的应用拿到的默认值：未启用，其余取保守的默认档。
func DefaultConfig(appID int64) Config {
	return Config{
		AppID:              appID,
		QuotaBytes:         DefaultQuotaBytes,
		MaxItemBytes:       DefaultMaxItemBytes,
		MaxItems:           DefaultMaxItems,
		MaxRevisions:       DefaultMaxRevisions,
		TrashRetentionDays: DefaultTrashRetentionDays,
		Namespaces:         []Namespace{},
	}
}

// Normalize 修整并校验配置；返回的错误信息可以直接给管理员看。
func (c *Config) Normalize() error {
	c.StorageConfigName = strings.TrimSpace(c.StorageConfigName)
	if len(c.StorageConfigName) > 128 {
		return errors.New("存储配置名称过长")
	}
	if c.QuotaBytes <= 0 || c.QuotaBytes > MaxQuotaBytes {
		return fmt.Errorf("单用户配额需在 1 字节到 %s 之间", FormatBytes(MaxQuotaBytes))
	}
	if c.MaxItemBytes <= 0 || c.MaxItemBytes > MaxItemBytesCap {
		return fmt.Errorf("单条目上限需在 1 字节到 %s 之间", FormatBytes(MaxItemBytesCap))
	}
	if c.MaxItemBytes > c.QuotaBytes {
		return errors.New("单条目上限不能超过单用户配额")
	}
	if c.MaxItems <= 0 || c.MaxItems > MaxItemsCap {
		return fmt.Errorf("条目数上限需在 1 到 %d 之间", MaxItemsCap)
	}
	if c.MaxRevisions <= 0 || c.MaxRevisions > MaxRevisionsCap {
		return fmt.Errorf("保留修订数需在 1 到 %d 之间", MaxRevisionsCap)
	}
	if c.TrashRetentionDays < 0 || c.TrashRetentionDays > MaxTrashRetentionDays {
		return fmt.Errorf("回收站保留天数需在 0 到 %d 之间", MaxTrashRetentionDays)
	}
	if len(c.Namespaces) > MaxNamespaces {
		return fmt.Errorf("命名空间目录最多 %d 项", MaxNamespaces)
	}
	seen := make(map[string]bool, len(c.Namespaces))
	cleaned := make([]Namespace, 0, len(c.Namespaces))
	for _, item := range c.Namespaces {
		item.Key = strings.TrimSpace(item.Key)
		item.Name = strings.TrimSpace(item.Name)
		item.Description = strings.TrimSpace(item.Description)
		if !ValidNamespace(item.Key) {
			return fmt.Errorf("命名空间 %q 格式无效：需为小写字母或数字开头，可含 . _ -，至多 64 位", item.Key)
		}
		if seen[item.Key] {
			return fmt.Errorf("命名空间 %q 重复", item.Key)
		}
		seen[item.Key] = true
		if item.Name == "" {
			item.Name = item.Key
		}
		if len([]rune(item.Name)) > 64 || len([]rune(item.Description)) > 255 {
			return fmt.Errorf("命名空间 %q 的名称或说明过长", item.Key)
		}
		cleaned = append(cleaned, item)
	}
	if c.RestrictNamespaces && len(cleaned) == 0 {
		return errors.New("限定命名空间时目录不能为空")
	}
	c.Namespaces = cleaned
	return nil
}

// AllowsNamespace 该命名空间是否允许写入。
func (c Config) AllowsNamespace(namespace string) bool {
	if !c.RestrictNamespaces {
		return true
	}
	for _, item := range c.Namespaces {
		if item.Key == namespace {
			return true
		}
	}
	return false
}

// NamespaceName 命名空间的展示名；目录里没有时返回空串。
func (c Config) NamespaceName(namespace string) string {
	for _, item := range c.Namespaces {
		if item.Key == namespace {
			return item.Name
		}
	}
	return ""
}

// Limits 下发给客户端的限制，客户端据此在本地先拦一道。
type Limits struct {
	QuotaBytes         int64 `json:"quotaBytes"`
	MaxItemBytes       int64 `json:"maxItemBytes"`
	MaxItems           int   `json:"maxItems"`
	MaxRevisions       int   `json:"maxRevisions"`
	TrashRetentionDays int   `json:"trashRetentionDays"`
	InlineContentBytes int64 `json:"inlineContentBytes"`
	JSONWriteBytes     int64 `json:"jsonWriteBytes"`
}

// UserState 用户的配额覆盖、冻结状态与用量账目。
type UserState struct {
	AppID         int64      `json:"appid"`
	UserID        int64      `json:"userId"`
	Account       string     `json:"account,omitempty"`
	Nickname      string     `json:"nickname,omitempty"`
	QuotaOverride *int64     `json:"quotaOverride,omitempty"`
	Frozen        bool       `json:"frozen"`
	FrozenReason  string     `json:"frozenReason,omitempty"`
	Note          string     `json:"note,omitempty"`
	UsedBytes     int64      `json:"usedBytes"`
	ItemCount     int        `json:"itemCount"`
	TrashCount    int        `json:"trashCount"`
	RevisionCount int        `json:"revisionCount"`
	LastWriteAt   *time.Time `json:"lastWriteAt,omitempty"`
	UpdatedBy     string     `json:"updatedBy,omitempty"`
	CreatedAt     *time.Time `json:"createdAt,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
	// QuotaBytes 生效配额（覆盖值或应用默认），由服务层填。
	QuotaBytes int64 `json:"quotaBytes"`
}

// EffectiveQuota 生效配额。
func (s UserState) EffectiveQuota(cfg Config) int64 {
	if s.QuotaOverride != nil && *s.QuotaOverride > 0 {
		return *s.QuotaOverride
	}
	return cfg.QuotaBytes
}

// NamespaceUsage 一个命名空间下的用量。
type NamespaceUsage struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name,omitempty"`
	ItemCount   int    `json:"itemCount"`
	TrashCount  int    `json:"trashCount"`
	StoredBytes int64  `json:"storedBytes"`
}

// Status 用户端状态：开没开、能不能写、用了多少、限制是什么。
type Status struct {
	Enabled      bool             `json:"enabled"`
	Writable     bool             `json:"writable"`
	Frozen       bool             `json:"frozen"`
	FrozenReason string           `json:"frozenReason,omitempty"`
	UsedBytes    int64            `json:"usedBytes"`
	ItemCount    int              `json:"itemCount"`
	TrashCount   int              `json:"trashCount"`
	Limits       Limits           `json:"limits"`
	Namespaces   []NamespaceUsage `json:"namespaces"`
	// Catalog 应用配置的命名空间目录；RestrictNamespaces 为真时只能写这里列出的。
	Catalog            []Namespace `json:"catalog"`
	RestrictNamespaces bool        `json:"restrictNamespaces"`
	LastWriteAt        *time.Time  `json:"lastWriteAt,omitempty"`
}

// Item 条目元数据（当前修订）。
type Item struct {
	ID            int64          `json:"id"`
	Namespace     string         `json:"namespace"`
	Key           string         `json:"key"`
	Revision      int64          `json:"revision"`
	ContentType   string         `json:"contentType"`
	Encoding      string         `json:"encoding"`
	Size          int64          `json:"size"`
	SHA256        string         `json:"sha256"`
	Metadata      map[string]any `json:"metadata"`
	DeviceID      string         `json:"deviceId,omitempty"`
	Deleted       bool           `json:"deleted"`
	DeletedAt     *time.Time     `json:"deletedAt,omitempty"`
	PurgeAt       *time.Time     `json:"purgeAt,omitempty"`
	RevisionCount int            `json:"revisionCount"`
	StoredBytes   int64          `json:"storedBytes"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`

	AppID  int64 `json:"-"`
	UserID int64 `json:"-"`
}

// ItemContent 条目 + 内容。内容超过内联上限时 ContentOmitted 为真，改用下载链接取。
type ItemContent struct {
	Item
	// ContentRevision 交回的内容属于哪个修订（读历史修订时与 Revision 不同）。
	ContentRevision int64           `json:"contentRevision"`
	Content         json.RawMessage `json:"content,omitempty"`
	ContentOmitted  bool            `json:"contentOmitted,omitempty"`
}

// Revision 一个修订。
type Revision struct {
	ID           int64          `json:"id"`
	ItemID       int64          `json:"itemId"`
	Revision     int64          `json:"revision"`
	ContentType  string         `json:"contentType"`
	Encoding     string         `json:"encoding"`
	Size         int64          `json:"size"`
	SHA256       string         `json:"sha256"`
	Metadata     map[string]any `json:"metadata"`
	DeviceID     string         `json:"deviceId,omitempty"`
	Source       string         `json:"source"`
	RestoredFrom *int64         `json:"restoredFrom,omitempty"`
	Operator     string         `json:"operator,omitempty"`
	Current      bool           `json:"current"`
	CreatedAt    time.Time      `json:"createdAt"`

	StorageConfigID int64  `json:"-"`
	ObjectKey       string `json:"-"`
}

// StoredBlob 存储桶里的一个对象（清除时用）。
type StoredBlob struct {
	StorageConfigID int64
	ObjectKey       string
}

// ItemQuery 条目列表查询。
type ItemQuery struct {
	AppID     int64
	UserID    int64
	Namespace string
	Keyword   string
	Status    string
	Page      int
	Limit     int
}

// ItemPage 条目分页。
type ItemPage struct {
	Items []Item `json:"items"`
	Total int64  `json:"total"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

// UserQuery 管理端用户列表查询。
type UserQuery struct {
	AppID   int64
	Keyword string
	// Sort usage / items / recent
	Sort   string
	Frozen *bool
	Page   int
	Limit  int
}

// UserPage 用户分页。
type UserPage struct {
	Items []UserState `json:"items"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

// DailyWrites 某一天的写入量。
type DailyWrites struct {
	Day    string `json:"day"`
	Writes int64  `json:"writes"`
	Bytes  int64  `json:"bytes"`
	Users  int64  `json:"users"`
}

// Stats 管理端概览。
type Stats struct {
	Users         int64            `json:"users"`
	FrozenUsers   int64            `json:"frozenUsers"`
	UsedBytes     int64            `json:"usedBytes"`
	Items         int64            `json:"items"`
	TrashItems    int64            `json:"trashItems"`
	Revisions     int64            `json:"revisions"`
	WritesToday   int64            `json:"writesToday"`
	Namespaces    []NamespaceUsage `json:"namespaces"`
	TopUsers      []UserState      `json:"topUsers"`
	Trend         []DailyWrites    `json:"trend"`
	StorageTarget *StorageTarget   `json:"storageTarget,omitempty"`
}

// StorageTarget 内容实际写往的存储配置（控制台展示「写到了哪里」）。
type StorageTarget struct {
	ConfigID   int64  `json:"configId"`
	ConfigName string `json:"configName"`
	Provider   string `json:"provider"`
	Scope      string `json:"scope"`
	Error      string `json:"error,omitempty"`
}

// FormatBytes 人类可读的字节数（只用于错误信息）。
func FormatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
