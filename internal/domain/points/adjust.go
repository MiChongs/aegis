package points

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 积分与经验的数值边界。
//
// 余额上限取 JavaScript 的 Number.MAX_SAFE_INTEGER（2^53-1）：控制台、Web SDK
// 与大多数 JSON 解析器把数字当双精度浮点数处理，超过这个值就会被悄悄舍入，
// 管理端看到的余额与库里的不再是同一个数。数据库里同样有 CHECK 约束兜底。
const (
	MaxBalance int64 = 9_007_199_254_740_991

	// MaxAdminAdjustAmount 管理员单次调整的数量上限（绝对值），防止手滑多打几个 0。
	MaxAdminAdjustAmount int64 = 100_000_000

	// MaxAdminBatchUsers 批量调整一次最多涉及的用户数。
	MaxAdminBatchUsers = 1000

	// MaxAdjustReasonRunes 调整原因的长度上限（字符数）。
	MaxAdjustReasonRunes = 200
)

// Kind 可调整的数值类型。
type Kind string

const (
	KindIntegral   Kind = "integral"
	KindExperience Kind = "experience"
)

// Label 中文名，用于错误提示与流水标题。
func (k Kind) Label() string {
	if k == KindExperience {
		return "经验"
	}
	return "积分"
}

var (
	// ErrBalanceInsufficient 变更后余额为负。
	ErrBalanceInsufficient = errors.New("points: balance insufficient")
	// ErrBalanceOverflow 变更后余额超过 MaxBalance（或 int64 溢出）。
	ErrBalanceOverflow = errors.New("points: balance overflow")
)

// ApplyDelta 计算 before + delta，保证结果落在 [0, MaxBalance]。
//
// 先判断再相加：int64 相加溢出会静默回绕成负数，事后检查 `after < 0`
// 会把「加爆了」误报成「余额不足」，甚至在回绕成正数时直接放行。
func ApplyDelta(before, delta int64) (int64, error) {
	if delta > 0 {
		if before > MaxBalance-delta {
			return 0, ErrBalanceOverflow
		}
		return before + delta, nil
	}
	// delta <= 0。不能写成 before < -delta：delta 为 MinInt64 时取反仍是 MinInt64。
	// before >= 0 时 before+delta 不会下溢（结果 >= MinInt64），直接相加再判断。
	if before < 0 {
		// 存量越界数据：只允许不变，不允许继续扣
		if delta == 0 {
			return before, nil
		}
		return 0, ErrBalanceInsufficient
	}
	after := before + delta
	if after < 0 {
		return 0, ErrBalanceInsufficient
	}
	return after, nil
}

// LevelMode 扣减经验之后的等级处理。
//
// 扣经验不收回已经发出去的等级达成奖励；之后再升回同一级也不会重复发（奖励按 用户 × 奖励键 去重）。
type LevelMode string

const (
	// LevelKeep 保留现有等级（已达到的等级是保底），只扣经验；等级进度按新经验显示。默认值。
	LevelKeep LevelMode = "keep"
	// LevelRecalc 按扣减后的经验重新判定，可以降级：不低于管理员锁定的保底，等级封顶照旧生效。
	LevelRecalc LevelMode = "recalc"
)

// Label 中文名。
func (m LevelMode) Label() string {
	if m == LevelRecalc {
		return "按经验重新判定等级"
	}
	return "保留现有等级"
}

// NormalizeLevelMode 空值按 keep 处理，其它值必须是 keep / recalc。
func NormalizeLevelMode(raw string) (LevelMode, error) {
	switch LevelMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", LevelKeep:
		return LevelKeep, nil
	case LevelRecalc:
		return LevelRecalc, nil
	}
	return "", invalid("等级处理方式只能是 keep（保留现有等级）或 recalc（按经验重新判定）")
}

// AdjustInput 一次管理员调整的输入（已解析、未校验）。
type AdjustInput struct {
	Kind      Kind
	AppID     int64
	UserID    int64
	Amount    int64
	Reason    string
	RequestID string
	// LevelMode 只对扣减经验有意义（见 LevelMode）
	LevelMode LevelMode
}

// ValidationError 输入不合法，Message 直接给到调用方。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ValidateAmount 校验单次调整数量：非零，绝对值不超过 MaxAdminAdjustAmount。
func ValidateAmount(kind Kind, amount int64) error {
	if amount == 0 {
		return invalid("调整数量不能为 0")
	}
	// -MinInt64 无法取绝对值，先单独挡掉
	if amount < -MaxAdminAdjustAmount || amount > MaxAdminAdjustAmount {
		return invalid("单次调整%s不能超过 %s", kind.Label(), GroupDigits(MaxAdminAdjustAmount))
	}
	return nil
}

// NormalizeReason 去掉首尾空白，限制长度；空原因由调用方给默认值。
func NormalizeReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if !utf8.ValidString(reason) {
		return "", invalid("调整原因包含无效字符")
	}
	if utf8.RuneCountInString(reason) > MaxAdjustReasonRunes {
		return "", invalid("调整原因不能超过 %d 个字", MaxAdjustReasonRunes)
	}
	return reason, nil
}

// Validate 校验单用户调整。
func (in *AdjustInput) Validate() error {
	if in.Kind != KindIntegral && in.Kind != KindExperience {
		return invalid("不支持的调整类型")
	}
	if in.AppID <= 0 {
		return invalid("缺少应用 ID（appid）")
	}
	if in.UserID <= 0 {
		return invalid("缺少用户 ID（user_id）")
	}
	if err := ValidateAmount(in.Kind, in.Amount); err != nil {
		return err
	}
	reason, err := NormalizeReason(in.Reason)
	if err != nil {
		return err
	}
	in.Reason = reason
	requestID, err := NormalizeRequestID(in.RequestID)
	if err != nil {
		return err
	}
	in.RequestID = requestID
	mode, err := NormalizeLevelMode(string(in.LevelMode))
	if err != nil {
		return err
	}
	in.LevelMode = mode
	return nil
}

// NormalizeRequestID 请求编号用于防重复提交：同一编号只生效一次，重放返回首次结果。
// 只接受 8-64 位的字母、数字、连字符与下划线（UUID 即可）。
func NormalizeRequestID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	if len(id) < 8 || len(id) > 64 {
		return "", invalid("请求编号长度应为 8-64 位")
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_') {
			return "", invalid("请求编号只能包含字母、数字、连字符与下划线")
		}
	}
	return id, nil
}

// GroupDigits 千分位格式化，供错误提示使用。
func GroupDigits(v int64) string {
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r != '-' && (len(s)-i)%3 == 0 && !(i == 1 && s[0] == '-') {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
