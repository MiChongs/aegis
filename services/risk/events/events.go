// Package events 风控事件总线（NATS JetStream）。
//
// 流 RISK：risk.event.>（主服务的业务结果）、risk.decision.>（每次决策）、risk.cep.alert.>（Flink 告警），
// 按时间保留，RisingWave / Flink / risk-ingest 各自用持久消费者读。
// 流 RISK_CMD：risk.command.>，工作队列语义，主服务与 risk-ingest 分别消费自己那部分命令。
//
// 事件是扁平 JSON：RisingWave 的 JSON 源与 Flink 作业直接按字段解析，不要嵌套关键字段。
package events

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"sync/atomic"
	"time"
)

// 流与主题
const (
	StreamEvents   = "RISK"
	StreamCommands = "RISK_CMD"

	SubjectEventPrefix    = "risk.event."
	SubjectDecisionPrefix = "risk.decision."
	SubjectAlertPrefix    = "risk.cep.alert."
	SubjectCommandPrefix  = "risk.command."
)

// 事件类型
const (
	TypeDecision       = "decision"
	TypeLoginSuccess   = "login.success"
	TypeLoginFail      = "login.fail"
	TypeRegister       = "register.success"
	TypePasswordReset  = "password.reset"
	TypePaymentPaid    = "payment.paid"
	TypePaymentRefund  = "payment.refund"
	TypeInviteBind     = "invite.bind"
	TypeSignin         = "signin"
	TypeLotteryDraw    = "lottery.draw"
	TypeCardkeyRedeem  = "cardkey.redeem"
	TypePointsRedeem   = "points.redeem"
	TypeAdReward       = "ad.reward"
	TypeProfileUpdate  = "profile.update"
	TypeAccountBan     = "account.ban"
	TypeAccountUnban   = "account.unban"
	TypeVerifyCodeSent = "verify_code.sent"
	TypeCEPAlert       = "cep.alert"
	TypeLoginLocked    = "login.locked" // 登录防爆破锁定
	TypeGeoAnomaly     = "geo.anomaly"  // 登录地理异常（不可能旅行、新国家……）
)

// Types 全部业务事件类型（控制台的事件下拉框、流式特征与 CEP 模式的事件选择）
func Types() []TypeInfo {
	return []TypeInfo{
		{TypeDecision, "风控决策"}, {TypeLoginSuccess, "登录成功"}, {TypeLoginFail, "登录失败"}, {TypeRegister, "注册成功"},
		{TypePasswordReset, "重置密码"}, {TypePaymentPaid, "支付成功"}, {TypePaymentRefund, "退款"}, {TypeInviteBind, "邀请绑定"},
		{TypeSignin, "签到"}, {TypeLotteryDraw, "抽奖"}, {TypeCardkeyRedeem, "卡密兑换"}, {TypePointsRedeem, "积分兑换"},
		{TypeAdReward, "激励广告发奖"}, {TypeProfileUpdate, "资料变更"}, {TypeAccountBan, "账号封禁"}, {TypeAccountUnban, "账号解封"},
		{TypeVerifyCodeSent, "验证码发送"}, {TypeCEPAlert, "行为模式告警"}, {TypeLoginLocked, "登录防爆破锁定"},
		{TypeGeoAnomaly, "登录地理异常"},
	}
}

// TypeInfo 事件类型说明
type TypeInfo struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

// Event 扁平事件
type Event struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Scene        string             `json:"scene,omitempty"`
	TS           int64              `json:"ts"` // 毫秒
	AppID        int64              `json:"app_id"`
	UserID       int64              `json:"user_id,omitempty"`
	Account      string             `json:"account,omitempty"`
	IP           string             `json:"ip,omitempty"`
	IPSubnet     string             `json:"ip_subnet,omitempty"`
	DeviceID     string             `json:"device_id,omitempty"`
	UA           string             `json:"ua,omitempty"`
	Country      string             `json:"country,omitempty"`
	Province     string             `json:"province,omitempty"`
	City         string             `json:"city,omitempty"`
	ISP          string             `json:"isp,omitempty"`
	ASN          int64              `json:"asn,omitempty"`
	Phone        string             `json:"phone,omitempty"`
	PhonePrefix  string             `json:"phone_prefix,omitempty"`
	Email        string             `json:"email,omitempty"`
	EmailDomain  string             `json:"email_domain,omitempty"`
	Amount       float64            `json:"amount,omitempty"`
	TargetUserID int64              `json:"target_user_id,omitempty"`
	Action       string             `json:"action,omitempty"`
	Level        string             `json:"level,omitempty"`
	Score        int                `json:"score,omitempty"`
	Release      string             `json:"release,omitempty"`
	Hits         []string           `json:"hits,omitempty"`
	Reasons      []string           `json:"reasons,omitempty"`
	Latency      int64              `json:"latency_us,omitempty"`
	Device       map[string]string  `json:"device,omitempty"` // 指纹要素（品牌、型号、屏幕……，向量与图谱用）
	Props        map[string]any     `json:"props,omitempty"`
	Features     map[string]float64 `json:"features,omitempty"`
	Models       map[string]float64 `json:"models,omitempty"`
	Trace        string             `json:"trace,omitempty"` // 决策完整轨迹（JSON 字符串），只进 ClickHouse
	Shadow       bool               `json:"shadow,omitempty"`
}

// Subject 事件发到哪个主题
func (e Event) Subject() string {
	switch e.Type {
	case TypeDecision:
		scene := e.Scene
		if scene == "" {
			scene = "unknown"
		}
		return SubjectDecisionPrefix + token(scene)
	case TypeCEPAlert:
		return SubjectAlertPrefix + token(e.Scene)
	}
	return SubjectEventPrefix + token(e.Type)
}

// token 主题里的一段只能用字母数字与 _ -（点会被当成层级）
func token(s string) string {
	if s == "" {
		return "_"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		case r == '.':
			return '_'
		}
		return -1
	}, s)
}

var seq atomic.Uint64

// NewID 时间有序的 128 位 ID（毫秒时间戳 + 进程内序号 + 随机），十六进制
func NewID() string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixMilli())<<16|seq.Add(1)&0xffff)
	_, _ = rand.Read(b[8:])
	return hex.EncodeToString(b[:])
}

// Command 处置命令（RISK_CMD 流）
type Command struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`   // list_add / account_ban / firewall_ban / revoke_sessions / account_unban / firewall_unban / list_remove
	Target          string `json:"target"` // device / ip / account / user
	Value           string `json:"value"`
	AppID           int64  `json:"app_id"`
	DurationSeconds int64  `json:"duration_seconds,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Source          string `json:"source"` // decision / cep / review / manual
	DecisionID      string `json:"decision_id,omitempty"`
	Operator        string `json:"operator,omitempty"`
	TS              int64  `json:"ts"`
}

// CommandSubject 命令主题：主服务执行的（account_* / firewall_* / revoke_sessions）与风控自己执行的（list_*）分开
func CommandSubject(kind string) string { return SubjectCommandPrefix + token(kind) }

// MainServerCommands 由主服务执行的命令
func MainServerCommands() []string {
	return []string{"account_ban", "account_unban", "firewall_ban", "firewall_unban", "revoke_sessions"}
}
