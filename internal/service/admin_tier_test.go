package service

import (
	"context"
	"testing"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
)

func accessOf(id int64, tier string, assignments ...admindomain.Assignment) *admindomain.AccessContext {
	return &admindomain.AccessContext{
		Session: admindomain.Session{
			AdminID:       id,
			IsSuperAdmin:  tier != admindomain.TierAdmin,
			IsSystemAdmin: tier == admindomain.TierSystem,
			Tier:          tier,
		},
		Assignments: assignments,
	}
}

func accountOf(id int64, tier string) admindomain.Account {
	return admindomain.Account{
		ID: id, Status: "active",
		IsSuperAdmin: tier != admindomain.TierAdmin, IsSystemAdmin: tier == admindomain.TierSystem, Tier: tier,
	}
}

// 档位守卫：系统管理员 > 超级管理员 > 管理员；谁都不能停用 / 改授权自己。
func TestManageDecisionTiers(t *testing.T) {
	t.Parallel()
	const (
		sys   = admindomain.TierSystem
		super = admindomain.TierSuper
		admin = admindomain.TierAdmin
	)
	cases := []struct {
		name     string
		actor    *admindomain.AccessContext
		target   admindomain.Account
		action   AdminAction
		want     bool
		wantCode int
	}{
		{"超管不能停用系统管理员", accessOf(2, super), accountOf(1, sys), AdminActionStatus, false, 40394},
		{"超管不能停用自己", accessOf(2, super), accountOf(2, super), AdminActionStatus, false, 40392},
		{"系统管理员也不能停用自己", accessOf(1, sys), accountOf(1, sys), AdminActionStatus, false, 40392},
		{"超管不能停用另一位超管", accessOf(2, super), accountOf(3, super), AdminActionStatus, false, 40395},
		{"超管可以停用普通管理员", accessOf(2, super), accountOf(9, admin), AdminActionStatus, true, 0},
		{"系统管理员可以停用超管", accessOf(1, sys), accountOf(2, super), AdminActionStatus, true, 0},
		{"系统管理员之间可以互相管理", accessOf(1, sys), accountOf(4, sys), AdminActionStatus, true, 0},
		{"普通管理员不能管理超管", accessOf(9, admin), accountOf(2, super), AdminActionAccess, false, 40395},
		{"普通管理员可以管理普通管理员", accessOf(9, admin), accountOf(10, admin), AdminActionAccess, true, 0},
		{"不能修改自己的授权", accessOf(9, admin), accountOf(9, admin), AdminActionAccess, false, 40393},
		{"不能给自己个人授权", accessOf(2, super), accountOf(2, super), AdminActionGrant, false, 40393},
		{"可以下线自己的会话", accessOf(2, super), accountOf(2, super), AdminActionSession, true, 0},
		{"超管不能踢系统管理员下线", accessOf(2, super), accountOf(1, sys), AdminActionSession, false, 40394},
		{"超管不能给系统管理员临时权限", accessOf(2, super), accountOf(1, sys), AdminActionTempPerm, false, 40394},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, code, message := manageDecision(tc.actor, tc.target, tc.action)
			if ok != tc.want || code != tc.wantCode {
				t.Fatalf("得到 ok=%v code=%d（%s），期望 ok=%v code=%d", ok, code, message, tc.want, tc.wantCode)
			}
			if !ok && message == "" {
				t.Fatal("拒绝时必须说明原因")
			}
		})
	}
}

// 档位变更：只有系统管理员能授予 / 收回超管与系统管理员身份。
func TestTierChangeRequiresSystemAdmin(t *testing.T) {
	t.Parallel()
	svc := newAuthorizationTestService(t)
	ctx := context.Background()
	admin := accountOf(9, admindomain.TierAdmin)
	super := accountOf(2, admindomain.TierSuper)
	if err := svc.ensureTierChangeAllowed(ctx, accessOf(2, admindomain.TierSuper), &admin, admindomain.TierSuper); err == nil {
		t.Fatal("超管不能把别人提升为超管")
	}
	if err := svc.ensureTierChangeAllowed(ctx, accessOf(2, admindomain.TierSuper), &super, admindomain.TierAdmin); err == nil {
		t.Fatal("超管不能收回别人的超管身份")
	}
	if err := svc.ensureTierChangeAllowed(ctx, accessOf(2, admindomain.TierSuper), nil, admindomain.TierSystem); err == nil {
		t.Fatal("超管不能新建系统管理员")
	}
	if err := svc.ensureTierChangeAllowed(ctx, accessOf(1, admindomain.TierSystem), &admin, admindomain.TierSuper); err != nil {
		t.Fatalf("系统管理员可以授予超管身份：%v", err)
	}
	if err := svc.ensureTierChangeAllowed(ctx, accessOf(2, admindomain.TierSuper), &admin, admindomain.TierAdmin); err != nil {
		t.Fatalf("档位不变时不受限制：%v", err)
	}
}

// 数据权限：全部应用不满足平台级请求；指定应用展开成具体应用；停用的角色不贡献任何东西。
func TestResolveAssignmentsDataScope(t *testing.T) {
	t.Parallel()
	svc := newAuthorizationTestService(t)
	svc.customRoles = map[string]admindomain.RoleDefinition{
		"custom_ops_all":    {Key: "custom_ops_all", Scope: "app", IsCustom: true, Status: "active", DataScope: admindomain.DataScopeAll},
		"custom_ops_pick":   {Key: "custom_ops_pick", Scope: "app", IsCustom: true, Status: "active", DataScope: admindomain.DataScopeCustom, DataAppIDs: []int64{7, 8}},
		"custom_ops_frozen": {Key: "custom_ops_frozen", Scope: "app", IsCustom: true, Status: "disabled", DataScope: admindomain.DataScopeAssigned},
	}
	app5 := int64(5)
	resolved := svc.resolveAssignments(context.Background(), 1, []admindomain.Assignment{
		{RoleKey: "custom_ops_all"},
		{RoleKey: "custom_ops_pick"},
		{RoleKey: "custom_ops_frozen", AppID: &app5},
		{RoleKey: authz.RoleAppViewer, AppID: &app5},
		{RoleKey: authz.RoleAppViewer}, // 「按授权绑定」却没绑应用：不能当成全局授权
	})
	var all, picked, frozen, viewer int
	for _, item := range resolved {
		switch item.RoleKey {
		case "custom_ops_all":
			all++
			if !item.AllApps || item.AppID != nil {
				t.Errorf("全部应用应展开成 AllApps：%+v", item)
			}
			if assignmentMatches(item, nil) {
				t.Error("全部应用的数据权限不能满足平台级请求")
			}
			if !assignmentMatches(item, &app5) {
				t.Error("全部应用的数据权限应在任意应用生效")
			}
		case "custom_ops_pick":
			picked++
			if item.AppID == nil || (*item.AppID != 7 && *item.AppID != 8) {
				t.Errorf("指定应用应展开成具体应用：%+v", item)
			}
		case "custom_ops_frozen":
			frozen++
		case authz.RoleAppViewer:
			viewer++
		}
	}
	if all != 1 || picked != 2 || frozen != 0 || viewer != 1 {
		t.Fatalf("展开结果不对：all=%d picked=%d frozen=%d viewer=%d", all, picked, frozen, viewer)
	}
}

// 授权上限：非超管只能授予自己持有的权限；通配不能绕过。
func TestEnsureCanGrantCeiling(t *testing.T) {
	t.Parallel()
	svc := newAuthorizationTestService(t)
	ctx := context.Background()
	app5 := int64(5)
	operator := accessOf(9, admindomain.TierAdmin, admindomain.Assignment{RoleKey: authz.RoleAppOperator, AppID: &app5})

	if err := svc.EnsureCanGrant(ctx, operator, []string{"app:lottery:read"}, &app5); err != nil {
		t.Fatalf("运营管理员持有抽奖查看，应能授予：%v", err)
	}
	err := svc.EnsureCanGrant(ctx, operator, []string{"app:lottery:write"}, &app5)
	if appErr, ok := err.(*apperrors.AppError); !ok || appErr.Code != 40399 {
		t.Fatalf("运营管理员没有抽奖编辑，应拒绝（40399），得到 %v", err)
	}
	if err := svc.EnsureCanGrant(ctx, operator, []string{"app:*"}, &app5); err == nil {
		t.Fatal("通配 app:* 展开后包含操作者没有的项，必须拒绝")
	}
	if err := svc.EnsureCanGrant(ctx, operator, []string{"app:lottery:read"}, nil); err == nil {
		t.Fatal("只在应用 5 持有的权限不能授给全局（会扩到所有应用）")
	}
	if err := svc.EnsureCanGrant(ctx, accessOf(2, admindomain.TierSuper), []string{"*"}, nil); err != nil {
		t.Fatalf("超管不受授权上限约束：%v", err)
	}
	if err := svc.EnsureCanAssignRole(ctx, operator, authz.RoleAppAdmin, &app5); err == nil {
		t.Fatal("运营管理员不能把权限更大的应用管理员角色授给别人")
	}
	if err := svc.EnsureCanAssignRole(ctx, operator, authz.RoleAppViewer, &app5); err != nil {
		t.Fatalf("应用观察员是运营管理员的子集，应能授予：%v", err)
	}
}
