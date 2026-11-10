package suspension

import (
	"strings"
	"time"
)

// 申诉状态
const (
	AppealPending   = "pending"
	AppealReviewing = "reviewing"
	AppealApproved  = "approved"
	AppealRejected  = "rejected"
	AppealWithdrawn = "withdrawn"
)

// 决策模型的结论
const (
	VerdictApprove  = "approve"
	VerdictReject   = "reject"
	VerdictEscalate = "escalate"
)

// 风控证据强弱
const (
	RiskStrong = "strong"
	RiskMedium = "medium"
	RiskWeak   = "weak"
)

// 自动复审模式
const (
	AutoReviewOff     = "off"
	AutoReviewSuggest = "suggest"
	AutoReviewAuto    = "auto"
)

// 不能申诉的原因
const (
	BlockedNotAppealable = "not_appealable"
	BlockedOpen          = "open"
	BlockedLimit         = "limit"
	BlockedCooldown      = "cooldown"
	BlockedDisabled      = "disabled"
)

// View 给客户端的封禁详情（错误响应 data.suspension 与 POST /suspension）
type View struct {
	ID          int64      `json:"id"`
	Level       string     `json:"level"`
	Type        string     `json:"type"`
	Scope       string     `json:"scope"`
	ReasonCode  string     `json:"reasonCode"`
	ReasonTitle string     `json:"reasonTitle"`
	Reason      string     `json:"reason"`
	StartAt     time.Time  `json:"startAt"`
	EndAt       *time.Time `json:"endAt"`
	Appeal      AppealInfo `json:"appeal"`
}

// AppealInfo 封禁详情里的申诉状态
type AppealInfo struct {
	Allowed       bool       `json:"allowed"`
	BlockedReason string     `json:"blockedReason"`
	Token         string     `json:"token"`
	Status        string     `json:"status"`
	AppealID      *int64     `json:"appealId"`
	SubmittedAt   *time.Time `json:"submittedAt"`
	DecidedAt     *time.Time `json:"decidedAt"`
	Reply         string     `json:"reply"`
	Remaining     int        `json:"remaining"`
	NextAllowedAt *time.Time `json:"nextAllowedAt"`
	// MinLength / MaxLength 申诉内容的字数范围（按 Unicode 字符数）
	MinLength int `json:"minLength"`
	MaxLength int `json:"maxLength"`
}

// AppealView 给客户端的一条申诉
type AppealView struct {
	ID           int64      `json:"id"`
	Status       string     `json:"status"`
	Content      string     `json:"content"`
	ContactEmail string     `json:"contactEmail"`
	SubmittedAt  time.Time  `json:"submittedAt"`
	DecidedAt    *time.Time `json:"decidedAt"`
	DecidedBy    string     `json:"decidedBy"`
	Reply        string     `json:"reply"`
}

// Appeal 一条申诉（管理端与内部）
type Appeal struct {
	ID              int64          `json:"id"`
	AppID           int64          `json:"appid"`
	AppName         string         `json:"appName,omitempty"`
	UserID          int64          `json:"userId"`
	BanID           *int64         `json:"banId"`
	PlatformBanID   *int64         `json:"platformBanId"`
	Status          string         `json:"status"`
	Content         string         `json:"content"`
	ContactEmail    string         `json:"contactEmail"`
	Locale          string         `json:"locale"`
	Source          string         `json:"source"`
	ClientIP        string         `json:"clientIp"`
	UserAgent       string         `json:"userAgent"`
	ModelVerdict    string         `json:"modelVerdict"`
	ModelConfidence float64        `json:"modelConfidence"`
	ModelSummary    string         `json:"modelSummary"`
	ModelReply      string         `json:"modelReply"`
	ModelName       string         `json:"modelName"`
	ModelError      string         `json:"modelError"`
	RiskLevel       string         `json:"riskLevel"`
	RiskEvidence    map[string]any `json:"riskEvidence"`
	ModelReviewedAt *time.Time     `json:"modelReviewedAt"`
	ReviewAttempts  int            `json:"reviewAttempts"`
	DecidedBy       string         `json:"decidedBy"`
	ReviewerAdminID *int64         `json:"reviewerAdminId"`
	ReviewerName    string         `json:"reviewerName"`
	ReviewNote      string         `json:"reviewNote"`
	Reply           string         `json:"reply"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	DecidedAt       *time.Time     `json:"decidedAt"`
	// 列表与详情里补的账号信息
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}

// Open 申诉还在进行中
func (a Appeal) Open() bool { return a.Status == AppealPending || a.Status == AppealReviewing }

// ToView 给客户端看的部分
func (a Appeal) ToView() AppealView {
	return AppealView{ID: a.ID, Status: a.Status, Content: a.Content, ContactEmail: a.ContactEmail, SubmittedAt: a.CreatedAt,
		DecidedAt: a.DecidedAt, DecidedBy: a.DecidedBy, Reply: a.Reply}
}

// AppealQuery 管理端列表
type AppealQuery struct {
	AppID    int64 // 0 = 全部应用
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// AppealStats 各队列数量（open 进行中且模型未给结论，suggested 进行中且模型已给结论）
type AppealStats struct {
	Open         int64 `json:"open"`
	Suggested    int64 `json:"suggested"`
	Approved     int64 `json:"approved"`
	Rejected     int64 `json:"rejected"`
	Withdrawn    int64 `json:"withdrawn"`
	Total        int64 `json:"total"`
	AutoApproved int64 `json:"autoApproved"`
	AutoRejected int64 `json:"autoRejected"`
}

// Settings 应用申诉设置
type Settings struct {
	Enabled           bool    `json:"enabled"`
	AutoReview        string  `json:"autoReview"`
	ApproveThreshold  float64 `json:"approveThreshold"`
	RejectThreshold   float64 `json:"rejectThreshold"`
	AutoApproveManual bool    `json:"autoApproveManual"`
	AllowDays         int     `json:"allowDays"`
	MaxAppealsPerBan  int     `json:"maxAppealsPerBan"`
	CooldownHours     int     `json:"cooldownHours"`
	MinLength         int     `json:"minLength"`
	MaxLength         int     `json:"maxLength"`
	AppealURL         string  `json:"appealUrl"`
	AIConfigID        int64   `json:"aiConfigId"`
}

// DefaultSettings 默认开放申诉、模型只给建议
func DefaultSettings() Settings {
	return Settings{Enabled: true, AutoReview: AutoReviewSuggest, ApproveThreshold: 0.85, RejectThreshold: 0.9, AllowDays: 30,
		MaxAppealsPerBan: 2, CooldownHours: 24, MinLength: 10, MaxLength: 2000}
}

// Normalize 补默认值、夹紧范围；返回不合法的说明
func (s Settings) Normalize() (Settings, string) {
	d := DefaultSettings()
	switch s.AutoReview {
	case "":
		s.AutoReview = d.AutoReview
	case AutoReviewOff, AutoReviewSuggest, AutoReviewAuto:
	default:
		return s, "自动复审只能是 off / suggest / auto"
	}
	if s.ApproveThreshold <= 0 {
		s.ApproveThreshold = d.ApproveThreshold
	}
	if s.RejectThreshold <= 0 {
		s.RejectThreshold = d.RejectThreshold
	}
	if s.ApproveThreshold < 0.5 || s.ApproveThreshold > 1 || s.RejectThreshold < 0.5 || s.RejectThreshold > 1 {
		return s, "置信度门槛在 0.5 到 1 之间"
	}
	if s.AllowDays < 0 || s.AllowDays > 365 {
		return s, "白名单保护 0 到 365 天"
	}
	if s.MaxAppealsPerBan <= 0 {
		s.MaxAppealsPerBan = d.MaxAppealsPerBan
	}
	if s.MaxAppealsPerBan > 10 {
		return s, "每条封禁最多申诉 10 次"
	}
	if s.CooldownHours < 0 || s.CooldownHours > 24*30 {
		return s, "冷却时间 0 到 720 小时"
	}
	if s.MinLength <= 0 {
		s.MinLength = d.MinLength
	}
	if s.MaxLength <= 0 {
		s.MaxLength = d.MaxLength
	}
	if s.MinLength > s.MaxLength || s.MaxLength > 10000 {
		return s, "申诉字数范围不合法"
	}
	s.AppealURL = strings.TrimSpace(s.AppealURL)
	if s.AppealURL != "" && !strings.HasPrefix(s.AppealURL, "https://") {
		return s, "申诉页地址必须是 https 链接"
	}
	if s.AIConfigID < 0 {
		s.AIConfigID = 0
	}
	return s, ""
}

// PlatformBan 全站封禁
type PlatformBan struct {
	ID               int64          `json:"id"`
	BanType          string         `json:"banType"`
	Status           string         `json:"status"`
	Reason           string         `json:"reason"`
	PublicReason     string         `json:"publicReason"`
	ReasonCode       string         `json:"reasonCode"`
	Source           string         `json:"source"`
	Appealable       bool           `json:"appealable"`
	OriginAppID      *int64         `json:"originAppId"`
	OriginUserID     *int64         `json:"originUserId"`
	Evidence         map[string]any `json:"evidence"`
	CreatedByAdminID *int64         `json:"createdByAdminId"`
	CreatedByName    string         `json:"createdByName"`
	RevokedByName    string         `json:"revokedByName"`
	RevokeReason     string         `json:"revokeReason"`
	StartAt          time.Time      `json:"startAt"`
	EndAt            *time.Time     `json:"endAt"`
	RevokedAt        *time.Time     `json:"revokedAt"`
	CreatedAt        time.Time      `json:"createdAt"`
	// 列表与详情补的
	Subjects []PlatformBanSubject `json:"subjects,omitempty"`
	Accounts []PlatformBanAccount `json:"accounts,omitempty"`
	// AccountCount 连带的账号数（生效中）
	AccountCount int `json:"accountCount"`
	SubjectCount int `json:"subjectCount"`
}

// PlatformBanSubject 全站封禁的一个主体（只有打码值）
type PlatformBanSubject struct {
	Kind   string `json:"kind"`
	Masked string `json:"masked"`
}

// PlatformBanAccount 全站封禁连带的一个账号
type PlatformBanAccount struct {
	AppID    int64  `json:"appid"`
	AppName  string `json:"appName"`
	UserID   int64  `json:"userId"`
	Account  string `json:"account"`
	Nickname string `json:"nickname"`
	BanID    int64  `json:"banId"`
	Status   string `json:"status"`
}
