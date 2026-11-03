package levels

import (
	"time"

	"aegis/internal/domain/itemgrant"
)

// 升级记录的原因。
const (
	ReasonExperience = "experience"
	ReasonSync       = "sync"
	ReasonRecalc     = "recalc"
	ReasonAdmin      = "admin"
	// ReasonPenalty 管理员等级封顶并降级（成长调控，docs/growth-control.md）
	ReasonPenalty = "penalty"
)

// 升级通知的发送状态。
const (
	NotifyNone    = "none"
	NotifyPending = "pending"
	NotifySending = "sending"
	NotifySent    = "sent"
	NotifyFailed  = "failed"
)

// 达成奖励记录的状态。
const (
	RewardQueued    = "queued"
	RewardClaimable = "claimable"
	RewardGranted   = "granted"
	RewardPending   = "pending"
	RewardFailed    = "failed"
)

// 达成奖励的来源。
const (
	OriginLevelUp  = "level_up"
	OriginBackfill = "backfill"
)

// 后台任务。
const (
	JobRecalc = "recalc"

	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

// SchemeRecord 控制台看到的一个应用的等级体系。
type SchemeRecord struct {
	AppID          int64      `json:"appid"`
	Draft          Graph      `json:"draft"`
	DraftRevision  int        `json:"draftRevision"`
	DraftUpdatedBy string     `json:"draftUpdatedBy,omitempty"`
	DraftUpdatedAt *time.Time `json:"draftUpdatedAt,omitempty"`
	// PublishedVersion 0 表示从未发布（正在使用默认体系）
	PublishedVersion int        `json:"publishedVersion"`
	PublishedBy      string     `json:"publishedBy,omitempty"`
	PublishedAt      *time.Time `json:"publishedAt,omitempty"`
	// Dirty 草稿与已发布的不同
	Dirty bool `json:"dirty"`
	// IsDefault 还没有保存过任何草稿，Draft 是默认体系
	IsDefault bool `json:"isDefault"`
}

// Summary 一套体系的摘要。
type Summary struct {
	Levels        int    `json:"levels"`
	MaxThreshold  int64  `json:"maxThreshold"`
	Rewards       int    `json:"rewards"`
	Privileges    int    `json:"privileges"`
	Gates         int    `json:"gates"`
	Name          string `json:"name,omitempty"`
	FirstLevel    string `json:"firstLevel,omitempty"`
	TopLevel      string `json:"topLevel,omitempty"`
	ManualRewards int    `json:"manualRewards"`
}

// Summarize 体系摘要。
func Summarize(s *Scheme) Summary {
	if s == nil {
		return Summary{}
	}
	out := Summary{Levels: len(s.Levels), Name: s.Name}
	for _, level := range s.Levels {
		out.MaxThreshold = max(out.MaxThreshold, level.Threshold)
		out.Rewards += len(level.Rewards)
		out.Privileges += len(level.Privileges)
		if level.Gate != nil && level.Gate.program != nil {
			out.Gates++
		}
		for _, reward := range level.Rewards {
			if reward.Claim == ClaimManual {
				out.ManualRewards++
			}
		}
	}
	if len(s.Levels) > 0 {
		out.FirstLevel = s.Levels[0].Name
		out.TopLevel = s.Levels[len(s.Levels)-1].Name
	}
	return out
}

// Version 一次发布。
type Version struct {
	Version     int       `json:"version"`
	Summary     Summary   `json:"summary"`
	Note        string    `json:"note,omitempty"`
	PublishedBy string    `json:"publishedBy,omitempty"`
	PublishedAt time.Time `json:"publishedAt"`
	Graph       *Graph    `json:"graph,omitempty"`
}

// LevelRow 编译后的一级（控制台的等级表、校验结果）。
type LevelRow struct {
	Level       int         `json:"level"`
	NodeID      string      `json:"nodeId"`
	Name        string      `json:"name"`
	Threshold   int64       `json:"threshold"`
	Icon        string      `json:"icon,omitempty"`
	Color       string      `json:"color,omitempty"`
	Description string      `json:"description,omitempty"`
	Gate        *GateRow    `json:"gate,omitempty"`
	Rewards     []Reward    `json:"rewards"`
	Privileges  []Privilege `json:"privileges"`
	// Effective 这一级实际享有的全部权益（含继承）
	Effective []Privilege `json:"effective"`
}

// GateRow 晋级条件的摘要。
type GateRow struct {
	NodeID string `json:"nodeId"`
	Label  string `json:"label,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Expr   string `json:"expr,omitempty"`
}

// Rows 体系逐级展开。
func Rows(s *Scheme) []LevelRow {
	if s == nil {
		return []LevelRow{}
	}
	out := make([]LevelRow, 0, len(s.Levels))
	for _, level := range s.Levels {
		row := LevelRow{
			Level: level.Level, NodeID: level.NodeID, Name: level.Name, Threshold: level.Threshold,
			Icon: level.Icon, Color: level.Color, Description: level.Description,
			Rewards: nonNilRewards(level.Rewards), Privileges: nonNilPrivileges(level.Privileges),
			Effective: s.Effective(level.Level),
		}
		if level.Gate != nil {
			row.Gate = &GateRow{NodeID: level.Gate.NodeID, Label: level.Gate.Label, Hint: level.Gate.Hint, Expr: level.Gate.Source}
		}
		out = append(out, row)
	}
	return out
}

func nonNilRewards(in []Reward) []Reward {
	if in == nil {
		return []Reward{}
	}
	return in
}

func nonNilPrivileges(in []Privilege) []Privilege {
	if in == nil {
		return []Privilege{}
	}
	return in
}

// UserLevel 控制台的用户等级列表一行。
type UserLevel struct {
	UserID        int64      `json:"userId"`
	Account       string     `json:"account"`
	Nickname      string     `json:"nickname,omitempty"`
	Avatar        string     `json:"avatar,omitempty"`
	Level         int        `json:"level"`
	LevelName     string     `json:"levelName,omitempty"`
	Experience    int64      `json:"experience"`
	Progress      float64    `json:"progress"`
	HighestLevel  int        `json:"highestLevel"`
	LevelUpCount  int        `json:"levelUpCount"`
	LastLevelUpAt *time.Time `json:"lastLevelUpAt,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
	// LockedLevel 管理员锁定的保底等级，0 = 没有锁定
	LockedLevel int `json:"lockedLevel"`
	// Claimable 待领取的奖励数
	Claimable int `json:"claimable"`
}

// UserQuery 用户等级筛选。
type UserQuery struct {
	AppID    int64
	Level    int
	MinLevel int
	MaxLevel int
	Keyword  string
	// Sort level（默认，等级高在前）/ experience / recent（最近升级）
	Sort  string
	Page  int
	Limit int
}

// Event 一条升级记录。
type Event struct {
	ID            int64     `json:"id"`
	AppID         int64     `json:"appid"`
	UserID        int64     `json:"userId"`
	Account       string    `json:"account,omitempty"`
	Nickname      string    `json:"nickname,omitempty"`
	FromLevel     int       `json:"fromLevel"`
	ToLevel       int       `json:"toLevel"`
	FromName      string    `json:"fromName,omitempty"`
	ToName        string    `json:"toName,omitempty"`
	Experience    int64     `json:"experience"`
	Reason        string    `json:"reason"`
	SchemeVersion int       `json:"schemeVersion"`
	Operator      string    `json:"operator,omitempty"`
	Note          string    `json:"note,omitempty"`
	NotifyStatus  string    `json:"notifyStatus"`
	CreatedAt     time.Time `json:"createdAt"`
}

// EventQuery 升级记录筛选。
type EventQuery struct {
	AppID  int64
	UserID int64
	Reason string
	Level  int
	From   *time.Time
	To     *time.Time
	Page   int
	Limit  int
}

// RewardRecord 一条达成奖励记录。
type RewardRecord struct {
	ID            int64                  `json:"id"`
	AppID         int64                  `json:"appid"`
	UserID        int64                  `json:"userId"`
	Account       string                 `json:"account,omitempty"`
	Nickname      string                 `json:"nickname,omitempty"`
	Level         int                    `json:"level"`
	NodeID        string                 `json:"nodeId"`
	Label         string                 `json:"label"`
	Items         []itemgrant.Item       `json:"items"`
	Results       []itemgrant.ItemResult `json:"results"`
	ItemsText     []string               `json:"itemsText"`
	Claim         string                 `json:"claim"`
	Status        string                 `json:"status"`
	Error         string                 `json:"error,omitempty"`
	Attempts      int                    `json:"attempts"`
	SchemeVersion int                    `json:"schemeVersion"`
	Origin        string                 `json:"origin"`
	GrantedAt     *time.Time             `json:"grantedAt,omitempty"`
	HandledBy     string                 `json:"handledBy,omitempty"`
	HandledNote   string                 `json:"handledNote,omitempty"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
}

// RewardQuery 奖励记录筛选。
type RewardQuery struct {
	AppID  int64
	UserID int64
	Status string
	Level  int
	NodeID string
	Page   int
	Limit  int
}

// Job 后台任务。
type Job struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	Kind          string         `json:"kind"`
	Params        map[string]any `json:"params"`
	Status        string         `json:"status"`
	SchemeVersion int            `json:"schemeVersion"`
	Total         int64          `json:"total"`
	Processed     int64          `json:"processed"`
	Changed       int64          `json:"changed"`
	Cursor        int64          `json:"cursor"`
	Message       string         `json:"message,omitempty"`
	CreatedBy     string         `json:"createdBy,omitempty"`
	HeartbeatAt   time.Time      `json:"heartbeatAt"`
	CreatedAt     time.Time      `json:"createdAt"`
	FinishedAt    *time.Time     `json:"finishedAt,omitempty"`
}

// Page 分页。
type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

// LevelStat 一级的人数。
type LevelStat struct {
	Level     int    `json:"level"`
	Name      string `json:"name"`
	Icon      string `json:"icon,omitempty"`
	Color     string `json:"color,omitempty"`
	Threshold int64  `json:"threshold"`
	Users     int64  `json:"users"`
}

// TrendPoint 一天的升级人次。
type TrendPoint struct {
	Date     string `json:"date"`
	LevelUps int64  `json:"levelUps"`
	Rewards  int64  `json:"rewards"`
}

// RewardCounts 奖励记录按状态计数。
type RewardCounts struct {
	Queued    int64 `json:"queued"`
	Claimable int64 `json:"claimable"`
	Granted   int64 `json:"granted"`
	Pending   int64 `json:"pending"`
	Failed    int64 `json:"failed"`
}

// Overview 等级总览。
type Overview struct {
	Version       int          `json:"version"`
	Summary       Summary      `json:"summary"`
	TotalUsers    int64        `json:"totalUsers"`
	RankedUsers   int64        `json:"rankedUsers"`
	Distribution  []LevelStat  `json:"distribution"`
	LevelUpsToday int64        `json:"levelUpsToday"`
	LevelUps7d    int64        `json:"levelUps7d"`
	Trend         []TrendPoint `json:"trend"`
	Rewards       RewardCounts `json:"rewards"`
	RunningJob    *Job         `json:"runningJob,omitempty"`
	AverageLevel  float64      `json:"averageLevel"`
}

// Simulation 按经验值推演的结果（控制台的「经验推演」）。
type Simulation struct {
	Experience int64       `json:"experience"`
	Level      int         `json:"level"`
	Name       string      `json:"name"`
	Progress   float64     `json:"progress"`
	ToNext     int64       `json:"toNext"`
	Blocked    *GateRow    `json:"blocked,omitempty"`
	Effective  []Privilege `json:"effective"`
	Rewards    []Reward    `json:"rewards"`
	// Trace 单人推演：每一道晋级条件的判定
	Trace []GateTrace `json:"trace,omitempty"`
}

// GateTrace 一道晋级条件对某个人的判定。
type GateTrace struct {
	Level  int    `json:"level"`
	NodeID string `json:"nodeId"`
	Label  string `json:"label,omitempty"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}
