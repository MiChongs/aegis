package service

import (
	"context"
	"strings"
	"testing"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// newAuthorizationTestService 构造一个带内存授权引擎的 AdminService。
// 这里覆盖的判定（权限展开 / 自助配额 / 拒绝文案）都不打库，
// 因此 pg 留空是安全的 —— 一旦有人把打库的调用挪进这些函数，测试会立刻 panic 告诉他。
func newAuthorizationTestService(t *testing.T) *AdminService {
	t.Helper()
	engine, err := authz.New(context.Background(), zap.NewNop(), nil, authz.BuiltinPolicies())
	if err != nil {
		t.Fatalf("构造授权引擎失败：%v", err)
	}
	return &AdminService{log: zap.NewNop(), authz: engine, roles: authz.BuiltinRoles()}
}

// 自助建应用的配额判定 —— 这一段是整次改动里最不能出错的地方：
// 判早一格是"永远建不了第一个应用"（也就是改动前那个死锁），
// 判晚一格是"配额形同虚设"，而后者的入口是**免授权**的注册接口。
func TestEvaluateSelfServiceAppCreation(t *testing.T) {
	t.Parallel()

	open := systemdomain.SelfServiceSettingsView{AllowAppCreation: true, MaxAppsPerAdmin: 3}
	closed := systemdomain.SelfServiceSettingsView{AllowAppCreation: false, MaxAppsPerAdmin: 3}
	unlimited := systemdomain.SelfServiceSettingsView{AllowAppCreation: true, MaxAppsPerAdmin: 0}

	cases := []struct {
		name        string
		policy      systemdomain.SelfServiceSettingsView
		privileged  bool
		appsCreated int
		wantAllowed bool
		wantCode    int
	}{
		{"零角色新账号建第一个应用", open, false, 0, true, 0},
		{"配额内继续创建", open, false, 2, true, 0},
		{"正好用满配额后被挡住", open, false, 3, false, 40319},
		{"超出配额（数据回填等原因导致的越界）同样挡住", open, false, 9, false, 40319},
		{"平台关闭自助创建", closed, false, 0, false, 40318},
		{"配额为 0 表示不限而不是禁止", unlimited, false, 99, true, 0},
		{"持有 app:write 的管理员不受配额约束", open, true, 99, true, 0},
		{"关闭自助创建也拦不住持有 app:write 的管理员", closed, true, 0, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateSelfServiceAppCreation(tc.policy, tc.privileged, tc.appsCreated)
			if got.Allowed != tc.wantAllowed {
				t.Fatalf("allowed = %v，期望 %v（message=%q）", got.Allowed, tc.wantAllowed, got.Message)
			}
			if got.Code != tc.wantCode {
				t.Fatalf("code = %d，期望 %d", got.Code, tc.wantCode)
			}
			if !got.Allowed && strings.TrimSpace(got.Message) == "" {
				t.Fatal("拒绝时必须给出原因，否则控制台只能显示一个空的错误条")
			}
			if tc.privileged && !got.Privileged {
				t.Fatal("常规授权路径必须标记 Privileged，否则控制台会把配额摆给一个不受配额约束的人看")
			}
		})
	}
}

// 拒绝文案必须说得出缺什么。
//
// 旧文案是一句光秃秃的「当前管理员无权执行此操作」（截图里的那一条），
// 它同时隐去了缺失的权限点、判定作用域和补救办法 —— 而最需要这三条信息的人，
// 恰恰是刚注册完、什么权限都没有的那个账号。
func TestDenyPermissionExplainsWhatIsMissing(t *testing.T) {
	t.Parallel()

	appID := int64(7)
	cases := []struct {
		name       string
		access     *admindomain.AccessContext
		permission string
		appID      *int64
		wantParts  []string
	}{
		{
			name:       "零角色账号被告知该往哪走",
			access:     &admindomain.AccessContext{},
			permission: authz.PermAppWrite,
			wantParts:  []string{authz.PermAppWrite, "应用信息修改", "平台级", "尚未被授予任何角色"},
		},
		{
			name: "应用作用域指明是哪个应用",
			access: &admindomain.AccessContext{
				Assignments: []admindomain.Assignment{{RoleKey: "app_viewer", AppID: &appID}},
			},
			permission: "app:user:write",
			appID:      &appID,
			wantParts:  []string{"app:user:write", "应用用户管理", "应用 #7"},
		},
		{
			name: "平台级权限点说明应用级角色满足不了",
			access: &admindomain.AccessContext{
				Assignments: []admindomain.Assignment{{RoleKey: "app_admin", AppID: &appID}},
			},
			permission: authz.PermPlatformAppGovern,
			wantParts:  []string{authz.PermPlatformAppGovern, "平台级", "应用级角色无法满足"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := denyPermission(tc.access, tc.permission, tc.appID, authz.Decision{Effect: "none"})
			appErr, ok := err.(*apperrors.AppError)
			if !ok {
				t.Fatalf("期望 *apperrors.AppError，得到 %T", err)
			}
			if appErr.Code != 40311 {
				t.Fatalf("错误码应保持 40311（前端已按它分支），得到 %d", appErr.Code)
			}
			for _, part := range tc.wantParts {
				if !strings.Contains(appErr.Message, part) {
					t.Errorf("文案里应包含 %q，实际为：%s", part, appErr.Message)
				}
			}
		})
	}
}

// 权限点必须能翻回权限目录里的中文名，否则「缺少 xxx」对使用者仍然是一串代码。
func TestPermissionDisplayNameUsesCatalog(t *testing.T) {
	t.Parallel()

	if got := permissionDisplayName(authz.PermAppWrite); got != "应用信息修改" {
		t.Fatalf("app:write 应翻成「应用信息修改」，得到 %q", got)
	}
	// 未登记的权限点原样返回，绝不能翻成空串 —— 那会得到「缺少「」」这种句子。
	if got := permissionDisplayName("not:registered"); got != "not:registered" {
		t.Fatalf("未登记的权限点应原样返回，得到 %q", got)
	}
}

// 生效权限展开：apps[x] 必须是**该应用下的完整集合**（含全局权限），不是增量。
// scopeMatches 里「全局授权覆盖应用级操作」这条规则若要调用方自己复现，
// 就等于把同一条判定写了两遍，而两遍迟早会不一致。
func TestExpandPermissionsMergesGlobalIntoAppScope(t *testing.T) {
	t.Parallel()

	svc := newAuthorizationTestService(t)
	appID := int64(42)
	otherApp := int64(43)
	access := &admindomain.AccessContext{
		Assignments: []admindomain.Assignment{
			{RoleKey: "app_viewer", AppID: &appID},
			{RoleKey: "platform_admin"}, // 全局作用域
		},
	}
	snapshot := svc.expandPermissions(access, []admindomain.TempPermission{
		{Permission: "points:write", AppID: &otherApp},
	})

	if !containsPermission(snapshot.Global, authz.PermAppWrite) {
		t.Fatal("平台管理员的 app:write 应出现在全局集合里")
	}
	appScoped := snapshot.Apps["42"]
	if !containsPermission(appScoped, authz.PermAppRead) {
		t.Fatal("应用级角色自己的权限应出现在该应用的集合里")
	}
	if !containsPermission(appScoped, authz.PermAppWrite) {
		t.Fatal("全局权限必须并入每个应用的集合，否则控制台要自己复现 scopeMatches")
	}
	if !containsPermission(snapshot.Apps["43"], "points:write") {
		t.Fatal("临时权限与角色授权同权，展开时不能漏掉 —— 漏掉会把确实能点的按钮藏起来")
	}
	if snapshot.All {
		t.Fatal("非超管不应标记 All")
	}
}

// 超管不走权限点判定，快照里要如实说明这件事，而不是伪装成一份可撤销的权限集。
func TestExpandPermissionsMarksSuperAdmin(t *testing.T) {
	t.Parallel()

	svc := newAuthorizationTestService(t)
	snapshot := svc.expandPermissions(&admindomain.AccessContext{
		Session: admindomain.Session{IsSuperAdmin: true},
	}, nil)
	if !snapshot.All {
		t.Fatal("超管必须置 All")
	}
	if len(snapshot.Global) == 0 {
		t.Fatal("超管的全局集合应枚举出全部权限点，供控制台直接判断")
	}
}

// nil 会话不能 panic：它出现在中间件之外的调用点（例如免登录路由误用）。
func TestExpandPermissionsHandlesNilAccess(t *testing.T) {
	t.Parallel()

	snapshot := newAuthorizationTestService(t).expandPermissions(nil, nil)
	if snapshot.Apps == nil || snapshot.Global == nil {
		t.Fatal("空会话也要返回可序列化的空集合，否则前端会拿到 null 并在 .map 上崩掉")
	}
}

// 内置角色里必须始终存在一个 scope=app 的 app_admin：
// 它是自助创建者的默认角色，被改成全局角色会让「建了个应用的人」静默拿到平台级授权。
func TestSelfServiceCreatorRoleStaysAppScoped(t *testing.T) {
	t.Parallel()

	role, ok := authz.BuiltinRoles()[selfServiceDefaults.CreatorRoleKey]
	if !ok {
		t.Fatalf("默认创建者角色 %q 不在内置角色表里", selfServiceDefaults.CreatorRoleKey)
	}
	if role.Scope != "app" {
		t.Fatalf("创建者角色必须是应用级，得到 scope=%q —— 全局角色会造成静默提权", role.Scope)
	}
	// 角色权限里现在可以写前缀通配，因此判据是"展开后含 app:write"而不是字面包含。
	engine, err := authz.New(context.Background(), zap.NewNop(), nil, authz.BuiltinPolicies())
	if err != nil {
		t.Fatalf("构造授权引擎失败：%v", err)
	}
	if !engine.Allow([]string{authz.RoleSubject(role.Key)}, authz.AppDomain(1), authz.PermAppWrite) {
		t.Fatal("创建者角色必须含 app:write，否则建完应用连改个名字都做不到")
	}
}

func containsPermission(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
