package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"aegis/internal/authz"
	"aegis/internal/config"
	admindomain "aegis/internal/domain/admin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 管理员分级 + 角色模型的真库验证（全部迁移跑两遍，含 000153 的存量提升）。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55433/postgres go test ./internal/service -run TestAdminPermissionIntegration
func TestAdminPermissionIntegration(t *testing.T) {
	if os.Getenv("AEGIS_TEST_PG_DSN") == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx, pool := openTestDatabase(t)

	// ── 存量部署：两个超管、没有系统管理员，迁移应把最早的那位提升为系统管理员 ──
	if _, err := pool.Exec(ctx, `UPDATE admin_accounts SET is_system_admin = FALSE`); err != nil {
		t.Fatal(err)
	}
	var first, second, operator, viewer int64
	for _, row := range []struct {
		account string
		super   bool
		target  *int64
	}{{"root-a", true, &first}, {"root-b", true, &second}, {"ops", false, &operator}, {"viewer", false, &viewer}} {
		if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ($1, 'x', $1, $2) RETURNING id`, row.account, row.super).Scan(row.target); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/postgres/000153_admin_tiers_and_role_model.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ { // 每次启动都会重放
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("重放 000153 第 %d 次失败：%v", round+1, err)
		}
	}
	var promoted int64
	if err := pool.QueryRow(ctx, `SELECT id FROM admin_accounts WHERE is_system_admin`).Scan(&promoted); err != nil {
		t.Fatal(err)
	}
	if promoted != first {
		t.Fatalf("应提升最早的超管 #%d，实际是 #%d", first, promoted)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_accounts SET is_system_admin = TRUE, is_super_admin = FALSE WHERE id = $1`, viewer); err == nil {
		t.Fatal("系统管理员必须同时是超管（CHECK 约束）")
	}

	for _, id := range []int64{101, 102, 103} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1::bigint, 'perm-test-'||$1::bigint::text) ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE apps SET created_by = $1 WHERE id = 103`, operator); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cfg := config.Config{AdminSessionTTL: time.Hour}
	cfg.JWT.Secret = "admin-permission-integration-secret"
	cfg.JWT.Issuer = "aegis-test"
	pg := pgrepo.New(pool)
	engine, err := authz.New(ctx, zap.NewNop(), pg, authz.BuiltinPolicies())
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewAdminService(cfg, zap.NewNop(), pg, redisrepo.NewSessionRepository(rdb, "t"), engine)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetupRoleSync()
	if err := svc.LoadCustomRoles(ctx); err != nil {
		t.Fatal(err)
	}
	load := func(id int64) *admindomain.AccessContext {
		profile, err := svc.GetProfile(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return svc.accessFromProfile(ctx, profile)
	}
	wantCode := func(err error, code int, what string) {
		t.Helper()
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != code {
			t.Fatalf("%s：期望错误码 %d，得到 %v", what, code, err)
		}
	}
	system, super := load(first), load(second)
	if system.Tier != admindomain.TierSystem || super.Tier != admindomain.TierSuper {
		t.Fatalf("档位推导错误：%s / %s", system.Tier, super.Tier)
	}

	// ── 停用守卫 ──
	wantCode(svc.UpdateAdminStatus(ctx, super, first, "disabled"), 40394, "超管停用系统管理员")
	wantCode(svc.UpdateAdminStatus(ctx, super, second, "disabled"), 40392, "超管停用自己")
	wantCode(svc.UpdateAdminStatus(ctx, system, first, "disabled"), 40392, "系统管理员停用自己")
	wantCode(svc.UpdateAdminAccess(ctx, system, first, admindomain.UpdateAccessInput{Tier: admindomain.TierSuper}), 40393, "系统管理员降级自己")
	if err := svc.UpdateAdminStatus(ctx, system, second, "disabled"); err != nil {
		t.Fatalf("系统管理员可以停用超管：%v", err)
	}
	if err := svc.UpdateAdminStatus(ctx, system, second, "active"); err != nil {
		t.Fatal(err)
	}
	wantCode(svc.UpdateAdminAccess(ctx, super, viewer, admindomain.UpdateAccessInput{Tier: admindomain.TierSuper}), 40397, "超管提升别人为超管")

	// ── 自定义角色：允许 / 禁止 / 数据权限 ──
	role, err := svc.CreateCustomRole(ctx, system, admindomain.CreateCustomRoleInput{
		RoleKey: "custom_lottery_ops", Name: "抽奖运营", Level: 10, Scope: "app",
		Permissions: []string{"app:lottery:*", "app:info:read"}, Deny: []string{"app:lottery:delete"},
		DataScope: admindomain.DataScopeCustom, DataAppIDs: []int64{101, 102},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Deny) != 1 {
		t.Fatalf("禁止项未落库：%+v", role)
	}
	if err := svc.UpdateAdminAccess(ctx, system, operator, admindomain.UpdateAccessInput{
		Assignments: []admindomain.AssignmentMutation{{RoleKey: "custom_lottery_ops"}},
	}); err != nil {
		t.Fatalf("指定应用数据权限的角色分配时不带应用：%v", err)
	}
	ops := load(operator)
	app101, app103 := int64(101), int64(103)
	if !svc.HasPermission(ctx, ops, "app:lottery:write", &app101) {
		t.Fatal("指定应用 101 内应有抽奖编辑")
	}
	if svc.HasPermission(ctx, ops, "app:lottery:write", &app103) {
		t.Fatal("应用 103 不在数据权限内")
	}
	if svc.HasPermission(ctx, ops, "app:lottery:delete", &app101) {
		t.Fatal("禁止项必须压过允许")
	}
	if svc.HasPermission(ctx, ops, "app:lottery:read", nil) {
		t.Fatal("应用级角色不能满足平台级请求")
	}

	// 改成「本人创建」：只在 103 生效
	if _, err := svc.UpdateCustomRole(ctx, system, "custom_lottery_ops", admindomain.UpdateCustomRoleInput{
		Name: "抽奖运营", Level: 10, Permissions: []string{"app:lottery:*", "app:info:read"},
		Deny: []string{"app:lottery:delete"}, DataScope: admindomain.DataScopeCreated,
	}); err != nil {
		t.Fatal(err)
	}
	ops = load(operator)
	if svc.HasPermission(ctx, ops, "app:lottery:write", &app101) || !svc.HasPermission(ctx, ops, "app:lottery:write", &app103) {
		t.Fatal("本人创建的数据权限应只覆盖操作者创建的应用 103")
	}

	// 停用角色：权限立即消失，授权关系保留
	if err := svc.SetRoleStatus(ctx, system, "custom_lottery_ops", admindomain.RoleStatusDisabled); err != nil {
		t.Fatal(err)
	}
	ops = load(operator)
	if svc.HasPermission(ctx, ops, "app:lottery:read", &app103) {
		t.Fatal("停用的角色不能再贡献权限")
	}
	if members, err := svc.ListRoleMembers(ctx, "custom_lottery_ops"); err != nil || len(members) != 1 {
		t.Fatalf("停用不应删除授权关系：%v %v", members, err)
	}
	if err := svc.SetRoleStatus(ctx, system, "custom_lottery_ops", admindomain.RoleStatusActive); err != nil {
		t.Fatal(err)
	}

	// ── 授权上限：持有「角色授权」的普通管理员不能授出自己没有的权限 ──
	if _, err := pool.Exec(ctx, `INSERT INTO admin_assignments (admin_id, role_key, appid) VALUES ($1, 'app_operator', 101)`, viewer); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAdminGrants(ctx, system, viewer, []AdminGrantInput{{Permission: authz.PermSystemAdminEdit}}); err != nil {
		t.Fatal(err)
	}
	delegate := load(viewer)
	if !svc.HasPermission(ctx, delegate, authz.PermSystemAdminEdit, nil) {
		t.Fatal("个人授权应生效")
	}
	err = svc.UpdateAdminAccess(ctx, delegate, operator, admindomain.UpdateAccessInput{
		Assignments: []admindomain.AssignmentMutation{{RoleKey: "custom_lottery_ops"}, {RoleKey: authz.RoleAppAdmin, AppID: &app101}},
	})
	wantCode(err, 40399, "授出比自己更大的角色")
	wantCode(svc.UpdateAdminAccess(ctx, delegate, viewer, admindomain.UpdateAccessInput{}), 40393, "修改自己的授权")
	wantCode(svc.UpdateAdminStatus(ctx, delegate, second, "disabled"), 40395, "普通管理员停用超管")

	// ── 角色成员与内置角色 override ──
	if _, err := svc.AddRoleMembers(ctx, system, authz.RoleAppViewer, []int64{operator}, nil); err == nil {
		t.Fatal("按授权绑定的应用级角色加成员时必须指定应用")
	}
	if added, err := svc.AddRoleMembers(ctx, system, authz.RoleAppViewer, []int64{operator}, &app101); err != nil || added != 1 {
		t.Fatalf("加成员失败：%d %v", added, err)
	}
	if _, err := svc.AddRoleMembers(ctx, system, authz.RoleAppViewer, []int64{second}, &app101); err == nil {
		t.Fatal("超管不需要角色授权，不能加进角色")
	}
	if err := svc.RemoveRoleMember(ctx, system, authz.RoleAppViewer, operator, &app101); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRolePermissions(ctx, system, authz.RoleAppViewer, []string{"app:lottery:write"}, []string{"app:report:export"}); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetRoleDetail(ctx, system, authz.RoleAppViewer)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(detail.OverrideAllow, "app:lottery:write") || !hasCode(detail.OverrideDeny, "app:report:export") {
		t.Fatalf("内置角色的增减应落进 override：%+v / %+v", detail.OverrideAllow, detail.OverrideDeny)
	}
	if hasCode(detail.Effective, "app:report:export") || !hasCode(detail.Effective, "app:lottery:write") {
		t.Fatal("生效集合应反映 override")
	}
	if !hasCode(detail.Legacy, authz.LegacyAppRead) {
		t.Fatalf("内置角色仍用旧合并权限点，详情应如实列出：%v", detail.Legacy)
	}

	// ── 复制与删除 ──
	copied, err := svc.CopyRole(ctx, system, authz.RoleAppOperator, "custom_operator_copy", "运营副本")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range copied.Permissions {
		if authz.IsLegacyPermission(code) || strings.HasSuffix(code, "*") {
			t.Fatalf("复制出的角色应只含细分权限点，得到 %s", code)
		}
	}
	if err := svc.DeleteCustomRole(ctx, "custom_lottery_ops", false); err == nil {
		t.Fatal("有成员的角色不能直接删除")
	}
	if err := svc.DeleteCustomRole(ctx, "custom_lottery_ops", true); err != nil {
		t.Fatal(err)
	}
	if count, _ := pg.CountAdminsByRoleKey(ctx, "custom_lottery_ops"); count != 0 {
		t.Fatalf("强制删除后应清掉成员授权，还剩 %d 条", count)
	}

	// ── 兜底：没有系统管理员时启动会自动指定 ──
	if _, err := pool.Exec(ctx, `UPDATE admin_accounts SET is_system_admin = FALSE`); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureSystemAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := pg.CountActiveSystemAdmins(ctx); count != 1 {
		t.Fatalf("应自动提升一位系统管理员，现有 %d 位", count)
	}
}

func hasCode(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
