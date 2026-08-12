package authz

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/persist"
	"github.com/casbin/casbin/v2/util"
	"go.uber.org/zap"
)

// 策略来源。决定一组策略归谁管，也决定重启时哪些会被覆盖。
const (
	// SourceBuiltin 内置角色的权限定义。**每次启动按代码里的定义整组重刷**，
	// 于是「升级后给 app_admin 加了个权限点」能到达所有既有部署。
	// 代价是这一档不接受人工编辑 —— 要调整内置角色请用 SourceOverride。
	SourceBuiltin = "builtin"
	// SourceCustom 自定义角色（admin_roles）的权限定义，由角色 CRUD 维护。
	SourceCustom = "custom"
	// SourceOverride 对**任意**角色（含内置角色）的人工增减。启动时不动它，
	// 因此它是"内置角色不可编辑"的出口：加一条 allow 就是扩权，
	// 加一条 deny 就是在不动角色定义的前提下砍掉一项能力。
	SourceOverride = "override"
	// SourceGrant 直接授予/禁止到某一个管理员，不经过角色。
	SourceGrant = "grant"
	// SourceOrg 组织角色的权限定义（组织域）。
	SourceOrg = "org"
)

// PolicyRule 是落库的一行策略。
//
// PType 为 "p" 时 Values = [sub, dom, obj, eft]；为 "g" 时 Values = [child, parent]。
type PolicyRule struct {
	PType  string   `json:"ptype"`
	Values []string `json:"values"`
	Source string   `json:"source"`
	// Owner 归属键（角色主体 / 管理员主体）。整组替换的粒度就是 (Source, Owner)，
	// 少了它就只能按 sub 猜归属，而 g 规则的 sub 与 p 规则的 sub 含义并不相同。
	Owner string `json:"owner"`
	Note  string `json:"note,omitempty"`
}

// Store 是策略的持久化出口，由 repository 层实现。
type Store interface {
	ListAuthzPolicies(ctx context.Context) ([]PolicyRule, error)
	ReplaceAuthzPolicyGroup(ctx context.Context, source, owner string, rules []PolicyRule, updatedBy *int64) error
	DeleteAuthzPolicyGroup(ctx context.Context, source, owner string) error
}

// Decision 一次判定的完整结论，**带判据**。
//
// 带判据不是锦上添花：拒绝时只说"无权限"，运维要靠翻代码才能知道
// 是角色没配、是域不匹配、还是被一条 deny 压住了。
type Decision struct {
	Allowed bool `json:"allowed"`
	// Effect 取 allow / deny / none。deny 表示被显式拒绝，none 表示没有任何策略命中。
	Effect string `json:"effect"`
	// Subject 命中的主体（哪个角色 / 哪个人）。
	Subject string `json:"subject,omitempty"`
	// Rule 命中的策略行原文。
	Rule []string `json:"rule,omitempty"`
}

// Engine 授权判定引擎。
type Engine struct {
	log      *zap.Logger
	store    Store
	enforcer *casbin.SyncedEnforcer
	watcher  persist.Watcher

	// reloadMu 串行化重载：并发重载会让两次 LoadPolicy 交错，
	// 中间态是一份"少了一半策略"的表，而那半秒里的判定会拒掉合法请求。
	reloadMu sync.Mutex
}

// New 构造引擎并完成首次装载。
//
// builtin 是内置角色的策略（来自代码），启动时整组重刷进库；
// store 为 nil 时引擎退化成纯内存模式（导出 OpenAPI 等不连库的场景）。
func New(ctx context.Context, log *zap.Logger, store Store, builtin []PolicyRule) (*Engine, error) {
	if log == nil {
		log = zap.NewNop()
	}
	m, err := newModel()
	if err != nil {
		return nil, fmt.Errorf("授权模型构造失败: %w", err)
	}
	engine := &Engine{log: log, store: store}
	adapter := &policyAdapter{store: store}
	enforcer, err := casbin.NewSyncedEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("授权引擎构造失败: %w", err)
	}
	// 写入一律走 Engine 的方法（要记 source / owner / updated_by），
	// 让 Casbin 自己回写会产出没有归属的行 —— 那种行在下次重刷内置策略时
	// 既不会被更新也不会被清掉，最后沉淀成谁也不敢删的幽灵授权。
	enforcer.EnableAutoSave(false)
	engine.enforcer = enforcer

	if store != nil {
		if err := engine.SeedBuiltin(ctx, builtin); err != nil {
			return nil, err
		}
		if err := engine.Reload(ctx); err != nil {
			return nil, err
		}
	} else {
		if err := engine.loadInMemory(builtin); err != nil {
			return nil, err
		}
	}
	return engine, nil
}

// SetWatcher 接上跨实例广播。改策略的实例发一条消息，其余实例重新装载。
//
// 没有它，多实例部署下改一次角色只有处理那次请求的实例知道，
// 其余实例要等到重启 —— 而"改完发现只有偶尔生效"是最难查的一类故障。
func (e *Engine) SetWatcher(watcher persist.Watcher) error {
	if e == nil || watcher == nil {
		return nil
	}
	e.watcher = watcher
	return watcher.SetUpdateCallback(func(string) {
		if err := e.Reload(context.Background()); err != nil {
			e.log.Warn("收到策略变更广播后重载失败", zap.Error(err))
		}
	})
}

// SeedBuiltin 把代码里的内置角色策略整组重刷进库。
func (e *Engine) SeedBuiltin(ctx context.Context, builtin []PolicyRule) error {
	if e.store == nil {
		return nil
	}
	byOwner := map[string][]PolicyRule{}
	for _, rule := range builtin {
		rule.Source = SourceBuiltin
		byOwner[rule.Owner] = append(byOwner[rule.Owner], rule)
	}
	for owner, rules := range byOwner {
		if err := e.store.ReplaceAuthzPolicyGroup(ctx, SourceBuiltin, owner, rules, nil); err != nil {
			return fmt.Errorf("内置策略写入失败(%s): %w", owner, err)
		}
	}
	return nil
}

// Reload 从库里重新装载全部策略。
func (e *Engine) Reload(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	if e.store == nil {
		return nil
	}
	if err := e.enforcer.LoadPolicy(); err != nil {
		return fmt.Errorf("授权策略装载失败: %w", err)
	}
	policies, _ := e.enforcer.GetPolicy()
	links, _ := e.enforcer.GetGroupingPolicy()
	e.log.Info("授权策略已装载", zap.Int("policies", len(policies)), zap.Int("roleLinks", len(links)))
	return nil
}

// loadInMemory 无库模式：直接把内置策略灌进 enforcer。
func (e *Engine) loadInMemory(builtin []PolicyRule) error {
	for _, rule := range builtin {
		var err error
		switch rule.PType {
		case "g":
			_, err = e.enforcer.AddGroupingPolicy(rule.Values)
		default:
			_, err = e.enforcer.AddPolicy(rule.Values)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// notify 广播策略变更（失败只记日志：本实例已经生效，广播失败不该让写操作失败）。
func (e *Engine) notify() {
	if e.watcher == nil {
		return
	}
	if err := e.watcher.Update(); err != nil {
		e.log.Warn("策略变更广播失败，其余实例将等待下次自动装载", zap.Error(err))
	}
}

// ── 判定 ──

// Decide 判定一组主体在某个域下是否拥有某权限点。
//
// 主体是一组而不是一个：一个管理员同时是"他自己"（admin:N，直接授予/禁止的落点）
// 与他所持角色的集合。**显式拒绝跨主体生效** —— 任何一个主体上的 deny 都压倒
// 其余主体的 allow，否则"禁止某人删工单"这条规则只要他再有一个别的角色就失效了。
func (e *Engine) Decide(subjects []string, domain, permission string) Decision {
	if e == nil || len(subjects) == 0 || permission == "" {
		return Decision{Effect: "none"}
	}
	if denied, subject, rule := e.matchDeny(subjects, domain, permission); denied {
		return Decision{Allowed: false, Effect: EffectDeny, Subject: subject, Rule: rule}
	}
	for _, subject := range subjects {
		allowed, explain, err := e.enforcer.EnforceEx(subject, domain, permission)
		if err != nil {
			e.log.Warn("授权判定失败", zap.String("subject", subject),
				zap.String("domain", domain), zap.String("permission", permission), zap.Error(err))
			continue
		}
		if allowed {
			return Decision{Allowed: true, Effect: EffectAllow, Subject: subject, Rule: explain}
		}
	}
	return Decision{Effect: "none"}
}

// Allow 是 Decide 的布尔形态。
func (e *Engine) Allow(subjects []string, domain, permission string) bool {
	return e.Decide(subjects, domain, permission).Allowed
}

// matchDeny 扫描显式拒绝策略。
//
// 单独扫而不是靠 Casbin 的 deny-override：后者只在**单次 Enforce**（单个主体
// 及其继承链）内生效，跨主体的拒绝会被另一个主体的放行盖过去。
// 拒绝策略在实践中很少（多数部署为零条），因此这里先取过滤后的集合，
// 空集直接返回，热路径上不产生任何额外开销。
func (e *Engine) matchDeny(subjects []string, domain, permission string) (bool, string, []string) {
	denies, err := e.enforcer.GetFilteredPolicy(3, EffectDeny)
	if err != nil || len(denies) == 0 {
		return false, "", nil
	}
	roleManager := e.enforcer.GetRoleManager()
	for _, rule := range denies {
		if len(rule) < 4 {
			continue
		}
		if !util.KeyMatch(domain, rule[1]) || !util.KeyMatch(permission, rule[2]) {
			continue
		}
		for _, subject := range subjects {
			if subject == rule[0] {
				return true, subject, rule
			}
			if roleManager == nil {
				continue
			}
			if linked, linkErr := roleManager.HasLink(subject, rule[0]); linkErr == nil && linked {
				return true, subject, rule
			}
		}
	}
	return false, "", nil
}

// PermissionsFor 展开一组主体在某个域下**实际生效**的权限点集合。
//
// 逐条跑 Decide 而不是读策略再自己展开通配符：控制台按这份集合决定按钮显隐，
// 一旦它与真正的判定不是同一段代码，就会出现"按钮在、点了 403"（或更糟的反向）。
// 目录规模在百级，判定是纯内存的，这点开销换的是"前端看到的与服务端判的永远一致"。
func (e *Engine) PermissionsFor(subjects []string, domain string, catalog []string) []string {
	if e == nil || len(subjects) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(catalog))
	for _, permission := range catalog {
		if e.Decide(subjects, domain, permission).Allowed {
			result = append(result, permission)
		}
	}
	sort.Strings(result)
	return result
}

// ── 策略维护 ──

// RolePolicy 描述一个角色的完整策略：允许集、拒绝集、继承的父角色。
type RolePolicy struct {
	// Allow 允许的权限点，支持前缀通配（ticket:* / *）。
	Allow []string
	// Deny 显式拒绝的权限点，压倒任何来源的允许（含继承来的）。
	Deny []string
	// Inherits 继承的父角色主体（用 RoleSubject / OrgRoleSubject 构造）。
	Inherits []string
	// Domain 策略生效的域，留空即 AnyDomain。角色权限通常与域无关（作用域由授权关系决定），
	// 组织自定义角色是例外 —— 它天然只属于一个组织。
	Domain string
	Note   string
}

// SetRolePolicy 整组替换某个角色在某个来源下的策略。
//
// 整组替换而不是增量改：增量接口（加一条/删一条）在并发编辑下会得到
// 谁也没想要的第三种结果，而角色编辑本来就是"提交一份完整的勾选"。
func (e *Engine) SetRolePolicy(ctx context.Context, source, subject string, policy RolePolicy, updatedBy *int64) error {
	if e == nil {
		return nil
	}
	domain := strings.TrimSpace(policy.Domain)
	if domain == "" {
		domain = AnyDomain
	}
	rules := make([]PolicyRule, 0, len(policy.Allow)+len(policy.Deny)+len(policy.Inherits))
	for _, permission := range dedupe(policy.Allow) {
		rules = append(rules, PolicyRule{PType: "p", Values: []string{subject, domain, permission, EffectAllow}, Source: source, Owner: subject, Note: policy.Note})
	}
	for _, permission := range dedupe(policy.Deny) {
		rules = append(rules, PolicyRule{PType: "p", Values: []string{subject, domain, permission, EffectDeny}, Source: source, Owner: subject, Note: policy.Note})
	}
	for _, parent := range dedupe(policy.Inherits) {
		if parent == "" || parent == subject {
			continue // 自继承会在角色管理器里形成自环
		}
		rules = append(rules, PolicyRule{PType: "g", Values: []string{subject, parent}, Source: source, Owner: subject, Note: policy.Note})
	}
	if e.store == nil {
		return nil
	}
	if err := e.store.ReplaceAuthzPolicyGroup(ctx, source, subject, rules, updatedBy); err != nil {
		return err
	}
	if err := e.Reload(ctx); err != nil {
		return err
	}
	e.notify()
	return nil
}

// RemoveRolePolicy 删掉某个角色在某个来源下的全部策略。
func (e *Engine) RemoveRolePolicy(ctx context.Context, source, subject string) error {
	if e == nil || e.store == nil {
		return nil
	}
	if err := e.store.DeleteAuthzPolicyGroup(ctx, source, subject); err != nil {
		return err
	}
	if err := e.Reload(ctx); err != nil {
		return err
	}
	e.notify()
	return nil
}

// DirectGrant 直接授予（或禁止）到某个管理员的一条策略。
type DirectGrant struct {
	// Domain 生效域：AnyDomain / AppDomain(n) / OrgDomain(n) / PlatformDomain。
	Domain string `json:"domain"`
	// Permission 权限点，支持前缀通配。
	Permission string `json:"permission"`
	// Effect allow / deny。
	Effect string `json:"effect"`
	Note   string `json:"note,omitempty"`
}

// SetAdminGrants 整组替换某个管理员的直接授予/禁止。
//
// 这是旧实现完全没有的一档能力：以前想给某个人单独加一个权限点，
// 要么专门造一个只有他一个人的角色，要么用带过期时间的临时权限顶着。
func (e *Engine) SetAdminGrants(ctx context.Context, adminID int64, grants []DirectGrant, updatedBy *int64) error {
	if e == nil || e.store == nil {
		return nil
	}
	subject := AdminSubject(adminID)
	rules := make([]PolicyRule, 0, len(grants))
	for _, grant := range grants {
		domain := strings.TrimSpace(grant.Domain)
		if domain == "" {
			domain = AnyDomain
		}
		effect := strings.TrimSpace(grant.Effect)
		if effect != EffectDeny {
			effect = EffectAllow
		}
		permission := strings.TrimSpace(grant.Permission)
		if permission == "" {
			continue
		}
		rules = append(rules, PolicyRule{
			PType: "p", Values: []string{subject, domain, permission, effect},
			Source: SourceGrant, Owner: subject, Note: grant.Note,
		})
	}
	if err := e.store.ReplaceAuthzPolicyGroup(ctx, SourceGrant, subject, rules, updatedBy); err != nil {
		return err
	}
	if err := e.Reload(ctx); err != nil {
		return err
	}
	e.notify()
	return nil
}

// Policies 返回当前生效的全部策略行（供管理端审阅与导出）。
func (e *Engine) Policies() [][]string {
	if e == nil {
		return nil
	}
	items, _ := e.enforcer.GetPolicy()
	return items
}

// RoleLinks 返回当前生效的角色继承边。
func (e *Engine) RoleLinks() [][]string {
	if e == nil {
		return nil
	}
	items, _ := e.enforcer.GetGroupingPolicy()
	return items
}

func dedupe(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
