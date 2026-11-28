package cloudstorage

import (
	"errors"
	"fmt"
	"strings"
)

// 云服务档位：同一个应用里，不同会员档位拿到不同的存储与同步服务。
//
// 应用配置上的 quotaBytes / maxItemBytes / … 是「基础档」（没有为免费用户单独配一档时就是它）。
// Plans 按优先级排列：用户此刻持有的会员档位（vip.Holdings.Tiers）里，排在最前面的那一档生效；
// TierID 为 0 的一档是免费档，覆盖基础档。管理员在用户行上设的配额覆盖值仍然优先于档位配额。
//
// 档位决定的不只是空间：
//
//	空间       配额、单条目上限、条目数、修订保留数、回收站保留天数
//	同步       能登记几台设备、同步记录上限、历史类记录在云端留多少天
//	实时       是否实时推送（关掉时客户端按 pollSeconds 轮询）
//	接力       能否把正在播放 / 阅读的内容发到另一台设备继续
//	范围       能同步哪些集合（空 = 全部）
type Plan struct {
	// TierID 会员档位 ID；0 表示免费档（未持有任何档位的用户）。
	TierID int64 `json:"tierId"`
	// Name 展示名（「免费」「VIP」……）；留空时由服务层用档位名补上。
	Name string `json:"name"`
	// Description 一句话说明，客户端档位对比卡片展示。
	Description string `json:"description,omitempty"`

	QuotaBytes         int64 `json:"quotaBytes"`
	MaxItemBytes       int64 `json:"maxItemBytes"`
	MaxItems           int   `json:"maxItems"`
	MaxRevisions       int   `json:"maxRevisions"`
	TrashRetentionDays int   `json:"trashRetentionDays"`

	SyncEnabled bool `json:"syncEnabled"`
	MaxDevices  int  `json:"maxDevices"`
	// MaxSyncRecords 同步记录（不含墓碑）条数上限。
	MaxSyncRecords int `json:"maxSyncRecords"`
	// HistoryRetentionDays 历史类集合（播放 / 观看 / 阅读记录）在云端保留的天数；0 = 永久。
	HistoryRetentionDays int  `json:"historyRetentionDays"`
	Realtime             bool `json:"realtime"`
	// PollSeconds 非实时档位的客户端轮询间隔（实时档位也用它做兜底）。
	PollSeconds int  `json:"pollSeconds"`
	Handoff     bool `json:"handoff"`
	// Collections 允许同步的集合；空 = 全部。
	Collections []string `json:"collections"`
}

// 档位的默认值与上限。
const (
	DefaultMaxDevices             = 3
	DefaultMaxSyncRecords         = 20000
	DefaultPollSeconds            = 300
	MinPollSeconds                = 15
	MaxPollSeconds                = 86400
	MaxDevicesCap                 = 50
	MaxSyncRecordsCap             = 1000000
	MaxHistoryRetentionDays       = 3650
	MaxPlans                      = 16
	planNameLimit                 = 32
	planDescriptionLimit          = 120
	FreeTierID              int64 = 0
)

// BasePlan 应用配置本身对应的基础档：没有为免费用户配档位时所有人都是它。
// 同步相关的取默认值：开同步、实时、能接力，与升级前的行为（不限设备）尽量一致。
func (c Config) BasePlan() Plan {
	return Plan{
		TierID:             FreeTierID,
		Name:               "基础",
		QuotaBytes:         c.QuotaBytes,
		MaxItemBytes:       c.MaxItemBytes,
		MaxItems:           c.MaxItems,
		MaxRevisions:       c.MaxRevisions,
		TrashRetentionDays: c.TrashRetentionDays,
		SyncEnabled:        true,
		MaxDevices:         DefaultMaxDevices,
		MaxSyncRecords:     DefaultMaxSyncRecords,
		Realtime:           true,
		PollSeconds:        DefaultPollSeconds,
		Handoff:            true,
		Collections:        []string{},
	}
}

// ResolvePlan 用户此刻生效的档位：持有的档位里排在 Plans 最前的那一档；都没有时用免费档，
// 没配免费档时用基础档。heldTierIDs 来自 vip.Holdings.Tiers（此刻仍生效的会员期）。
func (c Config) ResolvePlan(heldTierIDs []int64) Plan {
	held := make(map[int64]bool, len(heldTierIDs))
	for _, id := range heldTierIDs {
		held[id] = true
	}
	for _, plan := range c.Plans {
		if plan.TierID != FreeTierID && held[plan.TierID] {
			return plan
		}
	}
	return c.FreePlan()
}

// FreePlan 免费档（没配时为基础档）。
func (c Config) FreePlan() Plan {
	for _, plan := range c.Plans {
		if plan.TierID == FreeTierID {
			return plan
		}
	}
	return c.BasePlan()
}

// WithPlan 把档位的空间限制套到配置上：写入、配额、回收站等既有路径据此生效。
func (c Config) WithPlan(plan Plan) Config {
	c.QuotaBytes = plan.QuotaBytes
	c.MaxItemBytes = plan.MaxItemBytes
	c.MaxItems = plan.MaxItems
	c.MaxRevisions = plan.MaxRevisions
	c.TrashRetentionDays = plan.TrashRetentionDays
	return c
}

// AllowsCollection 档位是否允许同步这个集合。
func (p Plan) AllowsCollection(collection string) bool {
	if len(p.Collections) == 0 {
		return true
	}
	for _, item := range p.Collections {
		if item == collection {
			return true
		}
	}
	return false
}

// MinTrashRetentionDays / MinHistoryRetentionDays 全部档位里最短的保留期（后台清理据此圈定候选）。
func (c Config) MinTrashRetentionDays() int {
	value := c.FreePlan().TrashRetentionDays
	for _, plan := range c.Plans {
		value = min(value, plan.TrashRetentionDays)
	}
	return value
}

// MinHistoryRetentionDays 最短的历史保留天数（0 = 永久的档位不参与）；全部永久时返回 0。
func (c Config) MinHistoryRetentionDays() int {
	value := 0
	consider := func(days int) {
		if days > 0 && (value == 0 || days < value) {
			value = days
		}
	}
	consider(c.FreePlan().HistoryRetentionDays)
	for _, plan := range c.Plans {
		consider(plan.HistoryRetentionDays)
	}
	return value
}

// normalizePlans 校验档位表。档位里的空间限制与基础档同一套上限。
func (c *Config) normalizePlans() error {
	if len(c.Plans) > MaxPlans {
		return fmt.Errorf("档位最多 %d 个", MaxPlans)
	}
	seen := make(map[int64]bool, len(c.Plans))
	cleaned := make([]Plan, 0, len(c.Plans))
	for _, plan := range c.Plans {
		if plan.TierID < 0 {
			return errors.New("档位 ID 无效")
		}
		if seen[plan.TierID] {
			if plan.TierID == FreeTierID {
				return errors.New("免费档只能有一个")
			}
			return fmt.Errorf("会员档位 %d 重复", plan.TierID)
		}
		seen[plan.TierID] = true
		label := plan.Name
		if label == "" {
			label = fmt.Sprintf("档位 %d", plan.TierID)
		}
		plan.Name = strings.TrimSpace(plan.Name)
		plan.Description = strings.TrimSpace(plan.Description)
		if len([]rune(plan.Name)) > planNameLimit || len([]rune(plan.Description)) > planDescriptionLimit {
			return fmt.Errorf("「%s」的名称或说明过长", label)
		}
		if plan.QuotaBytes <= 0 || plan.QuotaBytes > MaxQuotaBytes {
			return fmt.Errorf("「%s」的配额需在 1 字节到 %s 之间", label, FormatBytes(MaxQuotaBytes))
		}
		if plan.MaxItemBytes <= 0 || plan.MaxItemBytes > MaxItemBytesCap || plan.MaxItemBytes > plan.QuotaBytes {
			return fmt.Errorf("「%s」的单条目上限需在 1 字节到 %s 之间且不超过配额", label, FormatBytes(MaxItemBytesCap))
		}
		if plan.MaxItems <= 0 || plan.MaxItems > MaxItemsCap {
			return fmt.Errorf("「%s」的条目数上限需在 1 到 %d 之间", label, MaxItemsCap)
		}
		if plan.MaxRevisions <= 0 || plan.MaxRevisions > MaxRevisionsCap {
			return fmt.Errorf("「%s」的保留修订数需在 1 到 %d 之间", label, MaxRevisionsCap)
		}
		if plan.TrashRetentionDays < 0 || plan.TrashRetentionDays > MaxTrashRetentionDays {
			return fmt.Errorf("「%s」的回收站保留天数需在 0 到 %d 之间", label, MaxTrashRetentionDays)
		}
		if plan.MaxDevices <= 0 || plan.MaxDevices > MaxDevicesCap {
			return fmt.Errorf("「%s」的设备数需在 1 到 %d 之间", label, MaxDevicesCap)
		}
		if plan.MaxSyncRecords <= 0 || plan.MaxSyncRecords > MaxSyncRecordsCap {
			return fmt.Errorf("「%s」的同步记录上限需在 1 到 %d 之间", label, MaxSyncRecordsCap)
		}
		if plan.HistoryRetentionDays < 0 || plan.HistoryRetentionDays > MaxHistoryRetentionDays {
			return fmt.Errorf("「%s」的历史保留天数需在 0 到 %d 之间", label, MaxHistoryRetentionDays)
		}
		if plan.PollSeconds == 0 {
			plan.PollSeconds = DefaultPollSeconds
		}
		if plan.PollSeconds < MinPollSeconds || plan.PollSeconds > MaxPollSeconds {
			return fmt.Errorf("「%s」的轮询间隔需在 %d 到 %d 秒之间", label, MinPollSeconds, MaxPollSeconds)
		}
		collections := make([]string, 0, len(plan.Collections))
		picked := map[string]bool{}
		for _, collection := range plan.Collections {
			collection = strings.TrimSpace(collection)
			if !ValidCollection(collection) {
				return fmt.Errorf("「%s」的集合 %q 格式无效", label, collection)
			}
			if !picked[collection] {
				picked[collection] = true
				collections = append(collections, collection)
			}
		}
		plan.Collections = collections
		cleaned = append(cleaned, plan)
	}
	c.Plans = cleaned
	return nil
}
