package service

import (
	"testing"
	"time"

	admindomain "aegis/internal/domain/admin"
	platformdomain "aegis/internal/domain/platform"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

func newTestGovernance(states ...platformdomain.Governance) *PlatformGovernanceService {
	s := NewPlatformGovernanceService(nil, nil, nil)
	for _, item := range states {
		s.states[item.AppID] = item
	}
	return s
}

// 冻结档必须挡住用户侧能力，但**不能**挡住应用管理员的写操作 ——
// 否则被冻结的应用连排查配置、联系平台都做不了。
// 停运 / 封禁 / 归档才把管理端一并只读化。
func TestPresetRestrictionsSeparateUserAndAdminSurface(t *testing.T) {
	frozen := platformdomain.PresetRestrictions(platformdomain.StateFrozen)
	if !frozen.BlockLogin || !frozen.BlockRegister || !frozen.BlockAPI {
		t.Fatalf("冻结必须挡住登录 / 注册 / 接口，得到 %+v", frozen)
	}
	if frozen.BlockAdminWrite {
		t.Error("冻结不该把应用管理员一起锁死：他还要排查问题和提交申诉")
	}

	for _, state := range []string{platformdomain.StateSuspended, platformdomain.StateBanned, platformdomain.StateArchived} {
		r := platformdomain.PresetRestrictions(state)
		if !r.BlockAdminWrite {
			t.Errorf("%s 必须把管理端写操作一并只读化，得到 %+v", state, r)
		}
		if !r.BlockNotification {
			t.Errorf("%s 必须停掉对外发信，得到 %+v", state, r)
		}
	}

	if platformdomain.PresetRestrictions(platformdomain.StateActive).Any() {
		t.Error("active 不该带任何限制项")
	}
}

// 判定必须逐能力生效：只勾了「禁注册」的应用，登录不受影响。
func TestDecideIsPerCapability(t *testing.T) {
	s := newTestGovernance(platformdomain.Governance{
		AppID:        7,
		State:        platformdomain.StateRestricted,
		Reason:       "批量注册风险",
		Restrictions: platformdomain.Restrictions{BlockRegister: true},
	})

	if d := s.Decide(7, platformdomain.CapabilityRegister); d.Allowed {
		t.Error("注册应被拒绝")
	}
	if d := s.Decide(7, platformdomain.CapabilityLogin); !d.Allowed {
		t.Errorf("登录未被限制，应放行，得到 %+v", d)
	}
	// 无治理记录的应用一律放行
	if d := s.Decide(99, platformdomain.CapabilityLogin); !d.Allowed {
		t.Error("无治理记录的应用必须放行")
	}
}

// 快照里残留的过期记录必须按时间放行，否则用户要多等一个 tick 才恢复。
func TestSnapshotIgnoresExpiredEntry(t *testing.T) {
	past := timeutil.NowUTC().Add(-time.Minute)
	s := newTestGovernance(platformdomain.Governance{
		AppID:        3,
		State:        platformdomain.StateFrozen,
		Restrictions: platformdomain.PresetRestrictions(platformdomain.StateFrozen),
		EndAt:        &past,
	})
	if got := s.Snapshot(3); got != nil {
		t.Fatalf("已过期的治理不该继续生效，得到 %+v", got)
	}
	if err := s.EnsureCapability(3, platformdomain.CapabilityLogin); err != nil {
		t.Fatalf("已过期的冻结不该再拦登录：%v", err)
	}
}

// EnsureCapability 必须返回可直接下发的业务错误，且错误码随能力区分 ——
// 客户端要能分辨"这个应用被冻了"和"我这次操作没权限"。
func TestEnsureCapabilityErrorCodes(t *testing.T) {
	s := newTestGovernance(platformdomain.Governance{
		AppID:        1,
		State:        platformdomain.StateSuspended,
		Reason:       "涉嫌违规",
		Restrictions: platformdomain.PresetRestrictions(platformdomain.StateSuspended),
	})
	cases := map[string]int{
		platformdomain.CapabilityLogin:      errCodeGovLoginBlocked,
		platformdomain.CapabilityRegister:   errCodeGovRegisterBlocked,
		platformdomain.CapabilityAPI:        errCodeGovAPIBlocked,
		platformdomain.CapabilityPayment:    errCodeGovPaymentBlocked,
		platformdomain.CapabilityStorage:    errCodeGovStorageBlocked,
		platformdomain.CapabilityAdminWrite: errCodeGovAdminWriteBlocked,
	}
	for capability, wantCode := range cases {
		err := s.EnsureCapability(1, capability)
		if err == nil {
			t.Fatalf("%s 应被拒绝", capability)
		}
		appErr, ok := err.(*apperrors.AppError)
		if !ok {
			t.Fatalf("%s 应返回 AppError，得到 %T", capability, err)
		}
		if appErr.Code != wantCode {
			t.Errorf("%s 错误码应为 %d，得到 %d", capability, wantCode, appErr.Code)
		}
		if appErr.Message == "" {
			t.Errorf("%s 必须带面向调用方的文案", capability)
		}
	}
}

// 拒绝文案给的是被治理方看的信息，不能带操作者与内部证据。
func TestBlockMessageDoesNotLeakOperator(t *testing.T) {
	s := newTestGovernance(platformdomain.Governance{
		AppID:        5,
		State:        platformdomain.StateFrozen,
		Reason:       "批量薅羊毛",
		OperatorName: "风控组-张三",
		Restrictions: platformdomain.PresetRestrictions(platformdomain.StateFrozen),
	})
	message := s.Decide(5, platformdomain.CapabilityLogin).Message
	if message == "" {
		t.Fatal("应有拒绝文案")
	}
	if contains(message, "张三") {
		t.Errorf("拒绝文案不得泄露操作者身份：%q", message)
	}
	if !contains(message, "批量薅羊毛") {
		t.Errorf("拒绝文案应带上理由，便于被治理方自查：%q", message)
	}
}

func TestResolveGovernanceDeadline(t *testing.T) {
	// 永久档不接受到期时间：否则会出现"永久封禁但三天后自动解封"
	if _, err := resolveGovernanceDeadline(platformdomain.StateBanned, platformdomain.ActionInput{DurationSeconds: 3600}); err == nil {
		t.Error("封禁不该接受到期时间")
	}
	if endAt, err := resolveGovernanceDeadline(platformdomain.StateBanned, platformdomain.ActionInput{}); err != nil || endAt != nil {
		t.Errorf("封禁应为永久，得到 endAt=%v err=%v", endAt, err)
	}

	// 过去的时间点等于"立刻解除"，属于误操作
	past := timeutil.NowUTC().Add(-time.Hour)
	if _, err := resolveGovernanceDeadline(platformdomain.StateFrozen, platformdomain.ActionInput{EndAt: &past}); err == nil {
		t.Error("过去的到期时间应被拒绝")
	}

	// 上限守住"临时冻结写成一百年"
	if _, err := resolveGovernanceDeadline(platformdomain.StateFrozen, platformdomain.ActionInput{
		DurationSeconds: int64((10 * 365 * 24 * time.Hour).Seconds()),
	}); err == nil {
		t.Error("超过上限的期限应被拒绝")
	}

	endAt, err := resolveGovernanceDeadline(platformdomain.StateFrozen, platformdomain.ActionInput{DurationSeconds: 7 * 86400})
	if err != nil {
		t.Fatalf("正常期限不该报错：%v", err)
	}
	if endAt == nil || !endAt.After(timeutil.NowUTC()) {
		t.Errorf("到期时间应落在未来，得到 %v", endAt)
	}

	// 解除治理时到期时间恒为空
	if endAt, err := resolveGovernanceDeadline(platformdomain.StateActive, platformdomain.ActionInput{DurationSeconds: 3600}); err != nil || endAt != nil {
		t.Errorf("恢复正常不该带到期时间，得到 %v / %v", endAt, err)
	}
}

// 可见范围推导：任一全局角色即全站可见；没有任何分配的管理员必须看到空集，
// 而不是被当成"不限范围"。
func TestAllowedGovernanceAppIDs(t *testing.T) {
	appID := int64(42)
	if ids := AllowedGovernanceAppIDs(true, nil); ids != nil {
		t.Errorf("超管应为不限范围（nil），得到 %v", ids)
	}
	if ids := AllowedGovernanceAppIDs(false, []admindomain.Assignment{{RoleKey: "platform_admin"}}); ids != nil {
		t.Errorf("持全局角色应为不限范围（nil），得到 %v", ids)
	}
	ids := AllowedGovernanceAppIDs(false, []admindomain.Assignment{{RoleKey: "app_admin", AppID: &appID}})
	if len(ids) != 1 || ids[0] != 42 {
		t.Errorf("应用级角色应只可见绑定应用，得到 %v", ids)
	}
	empty := AllowedGovernanceAppIDs(false, nil)
	if len(empty) != 1 || empty[0] != -1 {
		t.Errorf("无任何分配必须收敛为空集（哨兵 -1），得到 %v", empty)
	}
}

// 动作与状态的映射是控制台按钮与后端判定的共同来源，改一处必须两边一致。
func TestActionTargetState(t *testing.T) {
	cases := map[string]string{
		platformdomain.ActionFreeze:         platformdomain.StateFrozen,
		platformdomain.ActionRestrict:       platformdomain.StateRestricted,
		platformdomain.ActionSuspend:        platformdomain.StateSuspended,
		platformdomain.ActionBan:            platformdomain.StateBanned,
		platformdomain.ActionArchive:        platformdomain.StateArchived,
		platformdomain.ActionRestore:        platformdomain.StateActive,
		platformdomain.ActionExpire:         platformdomain.StateActive,
		platformdomain.ActionAppealApproved: platformdomain.StateActive,
	}
	for action, want := range cases {
		if got := platformdomain.ActionTargetState(action); got != want {
			t.Errorf("%s 应映射到 %s，得到 %s", action, want, got)
		}
	}
	if got := platformdomain.ActionTargetState("nope"); got != "" {
		t.Errorf("未知动作应返回空串，得到 %s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
