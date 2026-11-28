package cloudstorage

import (
	"encoding/json"
	"regexp"
	"time"
)

// 实时同步：记录级的跨设备同步（听歌、影视、番剧、短剧、小说、听书……）。
//
// 模型参照 Firefox Sync Storage 1.5 的「集合 + 记录」与 Kotatsu 同步服务的逐条最后写入者胜：
//
//	集合     music.history、novel.bookshelf……，一个集合一类数据
//	记录     （集合, 记录 ID）一行，内容是任意 JSON（各端约定的字段见 docs/cloud-service.md）
//	谁赢     逐条比较客户端的 updatedAt，新的胜；同一毫秒按 deviceId 字典序定胜负，各端结果一致
//	怎么拉   每次被接受的写入拿到一个按用户单调递增的 seq，客户端带上次的 seq 增量拉取
//	删除     留墓碑（deleted=true、内容清空）让别的设备也删掉；墓碑保留 TombstoneRetention 后清除，
//	         比墓碑地平线更旧的游标拿到 reset=true，客户端全量重拉再按同一规则合并（LWW 合并可重入）
//
// 与文档式的云存储条目（命名空间 / 键）共用一份配额：用量 = 条目修订 + 同步记录。

// 集合的种类：决定档位里「历史保留天数」管不管它。
const (
	CollectionKindHistory = "history"
	CollectionKindLibrary = "library"
	CollectionKindState   = "state"
)

// 同步的硬上限。
const (
	MaxSyncRecordBytes   = 16 << 10
	MaxSyncPushRecords   = 500
	MaxSyncPullRecords   = 1000
	DefaultSyncPullLimit = 500
	// TombstoneRetention 墓碑在云端留多久：比这更久没同步的设备要全量重拉。
	TombstoneRetention = 90 * 24 * time.Hour
	// MaxClockSkew 客户端时间最多比服务端快多少（更快的按服务端时间算，免得一台设备的错乱时钟永远赢）。
	MaxClockSkew = 5 * time.Minute
	// DeviceOnlineWindow 设备在这段时间内有过同步、心跳或上报即视为在线。
	DeviceOnlineWindow = 3 * time.Minute
	// HandoffTTL 接力请求多久不被领取就作废。
	HandoffTTL = 15 * time.Minute
	// MaxActivityBytes 设备「正在播放 / 阅读」上报的内容上限。
	MaxActivityBytes = 4 << 10
	deviceNameLimit  = 64
)

var (
	collectionPattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,23}\.[a-z][a-z0-9_-]{0,39}$`)
	recordIDPattern   = regexp.MustCompile(`^[\x21-\x7e]{1,200}$`)
	deviceIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// ValidCollection 集合名：「领域.名称」，都是小写，如 music.history。
func ValidCollection(value string) bool { return collectionPattern.MatchString(value) }

// ValidRecordID 记录 ID：1–200 个可见 ASCII 字符（各端多用「源:编号」或摘要）。
func ValidRecordID(value string) bool { return recordIDPattern.MatchString(value) }

// ValidDeviceID 设备号的字符集。
func ValidDeviceID(value string) bool { return deviceIDPattern.MatchString(value) }

// CollectionInfo 集合目录的一项。
type CollectionInfo struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Kind   string `json:"kind"`
}

// SyncDomain 一个内容领域（客户端按它分组展示）。
type SyncDomain struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// SyncDomains 内容领域目录。
var SyncDomains = []SyncDomain{
	{Key: "music", Name: "听歌"},
	{Key: "video", Name: "影视"},
	{Key: "anime", Name: "番剧"},
	{Key: "drama", Name: "短剧"},
	{Key: "novel", Name: "小说"},
	{Key: "audiobook", Name: "听书"},
	{Key: "comic", Name: "漫画"},
	{Key: "app", Name: "应用"},
}

// SyncCollections 内置的集合目录。目录外的集合也能同步（名字合法即可），只是按 state 种类处理、没有中文名。
var SyncCollections = []CollectionInfo{
	{Key: "music.history", Name: "听歌记录", Domain: "music", Kind: CollectionKindHistory},
	{Key: "music.favorites", Name: "收藏的歌", Domain: "music", Kind: CollectionKindLibrary},
	{Key: "music.playlists", Name: "歌单", Domain: "music", Kind: CollectionKindLibrary},
	{Key: "music.playlist-tracks", Name: "歌单里的歌", Domain: "music", Kind: CollectionKindLibrary},
	{Key: "video.history", Name: "影视观看记录", Domain: "video", Kind: CollectionKindHistory},
	{Key: "video.favorites", Name: "影视收藏", Domain: "video", Kind: CollectionKindLibrary},
	{Key: "anime.history", Name: "番剧观看记录", Domain: "anime", Kind: CollectionKindHistory},
	{Key: "anime.follows", Name: "追番", Domain: "anime", Kind: CollectionKindLibrary},
	{Key: "drama.history", Name: "短剧观看记录", Domain: "drama", Kind: CollectionKindHistory},
	{Key: "drama.follows", Name: "追剧", Domain: "drama", Kind: CollectionKindLibrary},
	{Key: "novel.history", Name: "阅读记录", Domain: "novel", Kind: CollectionKindHistory},
	{Key: "novel.bookshelf", Name: "书架", Domain: "novel", Kind: CollectionKindLibrary},
	{Key: "audiobook.history", Name: "收听记录", Domain: "audiobook", Kind: CollectionKindHistory},
	{Key: "audiobook.shelf", Name: "听书书架", Domain: "audiobook", Kind: CollectionKindLibrary},
	{Key: "comic.history", Name: "漫画阅读记录", Domain: "comic", Kind: CollectionKindHistory},
	{Key: "comic.shelf", Name: "漫画书架", Domain: "comic", Kind: CollectionKindLibrary},
	{Key: "app.favorites", Name: "收藏的工具", Domain: "app", Kind: CollectionKindLibrary},
	{Key: "app.settings", Name: "偏好设置", Domain: "app", Kind: CollectionKindState},
}

// LookupCollection 目录里的集合；不在目录里时按名字推一份（state 种类）。
func LookupCollection(key string) CollectionInfo {
	for _, item := range SyncCollections {
		if item.Key == key {
			return item
		}
	}
	domain := key
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			domain = key[:i]
			break
		}
	}
	return CollectionInfo{Key: key, Name: key, Domain: domain, Kind: CollectionKindState}
}

// HistoryCollections 目录里全部历史类集合（后台按档位裁剪历史时用）。
func HistoryCollections() []string {
	out := []string{}
	for _, item := range SyncCollections {
		if item.Kind == CollectionKindHistory {
			out = append(out, item.Key)
		}
	}
	return out
}

// SyncRecord 一条同步记录。
type SyncRecord struct {
	Collection string          `json:"collection"`
	ID         string          `json:"id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	// UpdatedAt 客户端修改这条记录的时间（毫秒级），最后写入者胜的依据。
	UpdatedAt time.Time `json:"updatedAt"`
	Deleted   bool      `json:"deleted"`
	// DeviceID 写入这一版的设备；DeviceName 读时补的展示名。
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	Seq        int64  `json:"seq"`
	Size       int64  `json:"size"`
	// CreatedAt 这条记录第一次出现在云端的时间。
	CreatedAt time.Time `json:"createdAt"`
}

// SyncChange 客户端推上来的一条改动。
type SyncChange struct {
	Collection string          `json:"collection"`
	ID         string          `json:"id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	UpdatedAt  time.Time       `json:"updatedAt"`
	Deleted    bool            `json:"deleted"`
}

// SyncPushResult 一批改动的结果。
type SyncPushResult struct {
	// Accepted 被接受的条数；Seq 本批之后用户的最新 seq。
	Accepted int   `json:"accepted"`
	Seq      int64 `json:"seq"`
	// Rejected 云端更新、本次没被接受的记录：交回云端那一版，客户端直接采用。
	Rejected []SyncRecord `json:"rejected"`
	// Applied 被接受的记录 → 拿到的 seq（客户端据此记已同步）。
	Applied []SyncAck `json:"applied"`
}

// SyncAck 一条被接受的改动。
type SyncAck struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
	Seq        int64  `json:"seq"`
}

// SyncChangesQuery 增量拉取。
type SyncChangesQuery struct {
	AppID       int64
	UserID      int64
	Since       int64
	Collections []string
	Limit       int
}

// SyncChangesPage 增量拉取的一页。
type SyncChangesPage struct {
	Records []SyncRecord `json:"records"`
	// Next 下次拉取带的 since；HasMore 为真时立刻接着拉。
	Next    int64 `json:"next"`
	HasMore bool  `json:"hasMore"`
	// Reset 游标比墓碑地平线还旧：清掉本地「已同步」标记，从 0 全量拉取再合并。
	Reset      bool      `json:"reset"`
	ServerTime time.Time `json:"serverTime"`
}

// SyncRecordQuery 按集合 / 设备浏览记录（查看别的设备同步了什么）。
type SyncRecordQuery struct {
	AppID          int64
	UserID         int64
	Collection     string
	Domain         string
	DeviceID       string
	Keyword        string
	IncludeDeleted bool
	Page           int
	Limit          int
}

// SyncRecordPage 记录分页。
type SyncRecordPage struct {
	Items []SyncRecord `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
}

// CollectionUsage 一个集合的用量，带按设备的分布。
type CollectionUsage struct {
	CollectionInfo
	Count         int64            `json:"count"`
	Bytes         int64            `json:"bytes"`
	LastUpdatedAt *time.Time       `json:"lastUpdatedAt,omitempty"`
	ByDevice      map[string]int64 `json:"byDevice"`
	// Allowed 当前档位能不能同步它。
	Allowed bool `json:"allowed"`
}

// DeviceInfo 设备登记时上报的展示信息（会话里有的以会话为准）。
type DeviceInfo struct {
	DeviceID   string `json:"deviceId"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	Model      string `json:"model"`
	OS         string `json:"os"`
	OSVersion  string `json:"osVersion"`
	AppVersion string `json:"appVersion"`
}

// TruncateDeviceName 名字的长度上限。
func TruncateDeviceName(value string) string {
	runes := []rune(value)
	if len(runes) > deviceNameLimit {
		return string(runes[:deviceNameLimit])
	}
	return value
}

// Activity 设备此刻在播放 / 阅读的内容。
type Activity struct {
	Collection string          `json:"collection"`
	RecordID   string          `json:"recordId,omitempty"`
	State      string          `json:"state"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	At         time.Time       `json:"at"`
}

// Activity 的状态。
const (
	ActivityPlaying = "playing"
	ActivityPaused  = "paused"
	ActivityReading = "reading"
	ActivityIdle    = "idle"
)

// ValidActivityState 是否是受支持的状态。
func ValidActivityState(value string) bool {
	switch value {
	case ActivityPlaying, ActivityPaused, ActivityReading, ActivityIdle:
		return true
	}
	return false
}

// SyncDevice 一台登记过的同步设备。
type SyncDevice struct {
	DeviceInfo
	Current     bool       `json:"current"`
	Online      bool       `json:"online"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	LastSyncAt  *time.Time `json:"lastSyncAt,omitempty"`
	LastSeq     int64      `json:"lastSeq"`
	Activity    *Activity  `json:"activity,omitempty"`
	RecordCount int64      `json:"recordCount"`
	// Collections 这台设备写入的（未删除）记录按集合计数。
	Collections map[string]int64 `json:"collections"`
	CreatedAt   time.Time        `json:"createdAt"`
}

// Handoff 一次接力：把一条记录发到另一台设备上继续。
type Handoff struct {
	ID           int64           `json:"id"`
	FromDeviceID string          `json:"fromDeviceId"`
	FromName     string          `json:"fromName,omitempty"`
	ToDeviceID   string          `json:"toDeviceId"`
	Collection   string          `json:"collection"`
	RecordID     string          `json:"recordId"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	ExpiresAt    time.Time       `json:"expiresAt"`
	AckedAt      *time.Time      `json:"ackedAt,omitempty"`
}

// SyncOverview 云服务首页：档位、用量、设备、各集合。
type SyncOverview struct {
	Plan        PlanView          `json:"plan"`
	Usage       Usage             `json:"usage"`
	Devices     []SyncDevice      `json:"devices"`
	Collections []CollectionUsage `json:"collections"`
	Domains     []SyncDomain      `json:"domains"`
	Seq         int64             `json:"seq"`
	ServerTime  time.Time         `json:"serverTime"`
}

// PlanView 下发给客户端的档位：档位本身 + 档位名 + 是否当前档位。
type PlanView struct {
	Plan
	TierName string `json:"tierName,omitempty"`
	Current  bool   `json:"current"`
	// Override 用户行上有管理员设的配额覆盖值（此时 quotaBytes 是覆盖值）。
	Override bool `json:"override,omitempty"`
}

// Usage 用量：文档条目与同步记录共用一份配额。
type Usage struct {
	QuotaBytes  int64 `json:"quotaBytes"`
	UsedBytes   int64 `json:"usedBytes"`
	ItemBytes   int64 `json:"itemBytes"`
	SyncBytes   int64 `json:"syncBytes"`
	SyncRecords int64 `json:"syncRecords"`
	Devices     int   `json:"devices"`
}
