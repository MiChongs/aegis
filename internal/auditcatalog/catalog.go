// Package auditcatalog 是后台审计日志的「操作目录」：每个管理端接口（方法 + 路由模板）
// 对人意味着什么 —— 叫什么、属于哪个模块、作用在什么对象上、是查看还是变更、风险多高。
//
// 为什么是显式目录而不是从 URL 推断：推断只能把路径段拼回去，得到的是
// 「查询 apps.channels appkey=b7784e2f…」「app.releases.assets.create」这类给机器看的东西；
// 而「上传安装包」「调整灰度比例」这种说法只有写代码的人知道。目录让每个接口的说法只写一次，
// 列表、详情、导出、统计全部从这里取。
//
// 目录在**读取时**生效：日志行里存了 method 与 route，展示字段现查目录得出。
// 因此改一处说法，历史日志随之更新；新接口漏登记会被 TestCatalogCoversEveryAdminRoute 拦下。
//
// 本包不依赖任何业务包，中间件（写入）与服务层（读取、回填）都可以引用。
package auditcatalog

import (
	"fmt"
	"sort"
	"strings"
)

// 操作类型。
const (
	KindRead   = "read"   // 查看：列表、详情、统计。默认不出现在操作列表里
	KindWrite  = "write"  // 变更：新建、修改、删除、状态流转
	KindExport = "export" // 导出：数据离开系统，单独标出
	KindAuth   = "auth"   // 登录、登出、二次验证等身份事件
)

// 风险等级，与 systemdomain.AuditSeverity* 取值一致。
const (
	SeverityInfo     = "info"
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Module 一个业务模块。
type Module struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Operation 一个接口的审计语义。
type Operation struct {
	// Name 中文操作名，动宾短语：「上传安装包」「冻结用户」「查看版本列表」
	Name string `json:"name"`
	// Module 模块键，见 Modules
	Module string `json:"module"`
	// Target 操作对象的类型：「安装包」「用户」；整体性操作（查看统计、修改设置）可为空
	Target string `json:"target,omitempty"`
	// Kind 见 Kind* 常量
	Kind string `json:"kind"`
	// Severity 见 Severity* 常量；查看类一律 info
	Severity string `json:"severity"`
}

// modules 的顺序就是控制台筛选项的顺序。
var modules = []Module{
	{Key: "auth", Label: "登录与认证"},
	{Key: "account", Label: "个人中心"},
	{Key: "admin", Label: "管理员与权限"},
	{Key: "org", Label: "组织架构"},
	{Key: "app", Label: "应用管理"},
	{Key: "user", Label: "用户管理"},
	{Key: "content", Label: "内容运营"},
	{Key: "release", Label: "发布中心"},
	{Key: "commerce", Label: "商业化"},
	{Key: "function", Label: "云函数与 AI"},
	{Key: "storage", Label: "存储"},
	{Key: "notify", Label: "消息通知"},
	{Key: "security", Label: "安全与风控"},
	{Key: "support", Label: "工单"},
	{Key: "report", Label: "数据报表"},
	{Key: "platform", Label: "平台与系统"},
}

var moduleLabels = func() map[string]string {
	out := make(map[string]string, len(modules))
	for _, m := range modules {
		out[m.Key] = m.Label
	}
	return out
}()

// Modules 全部模块，按展示顺序。
func Modules() []Module { return append([]Module(nil), modules...) }

// ModuleLabel 模块中文名；未知模块原样返回。
func ModuleLabel(key string) string {
	if label, ok := moduleLabels[key]; ok {
		return label
	}
	return key
}

var registry = map[string]Operation{}

// Spec 一条登记。用 View / Write / Export / Auth 构造。
type Spec struct {
	key string
	op  Operation
}

// View 查看类接口。key 形如 "GET /api/admin/apps/:appkey/releases"。
func View(key, name, target string) Spec {
	return Spec{key: key, op: Operation{Name: name, Target: target, Kind: KindRead, Severity: SeverityInfo}}
}

// Write 变更类接口。
func Write(key, name, target, severity string) Spec {
	return Spec{key: key, op: Operation{Name: name, Target: target, Kind: KindWrite, Severity: severity}}
}

// Export 导出类接口：数据离开系统，至少 medium。
func Export(key, name, target string) Spec {
	return Spec{key: key, op: Operation{Name: name, Target: target, Kind: KindExport, Severity: SeverityMedium}}
}

// Auth 身份事件（登录、登出、二次验证、改密码）。
func Auth(key, name, severity string) Spec {
	return Spec{key: key, op: Operation{Name: name, Kind: KindAuth, Severity: severity}}
}

// Register 登记一个模块下的全部接口。在各 ops_*.go 的 init 里调用。
// 重复登记、未知模块、非法取值一律 panic：目录错了宁可启动失败，也不要静默写出错误的审计。
func Register(module string, specs ...Spec) {
	if _, ok := moduleLabels[module]; !ok {
		panic(fmt.Sprintf("auditcatalog: unknown module %q", module))
	}
	for _, spec := range specs {
		key := normalizeKey(spec.key)
		if _, exists := registry[key]; exists {
			panic(fmt.Sprintf("auditcatalog: duplicate entry %q", key))
		}
		if strings.TrimSpace(spec.op.Name) == "" {
			panic(fmt.Sprintf("auditcatalog: empty name for %q", key))
		}
		switch spec.op.Severity {
		case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		default:
			panic(fmt.Sprintf("auditcatalog: invalid severity %q for %q", spec.op.Severity, key))
		}
		spec.op.Module = module
		registry[key] = spec.op
	}
}

func normalizeKey(key string) string {
	method, route, ok := strings.Cut(strings.TrimSpace(key), " ")
	if !ok {
		panic(fmt.Sprintf("auditcatalog: malformed key %q (want \"METHOD /route\")", key))
	}
	return strings.ToUpper(method) + " " + strings.TrimSpace(route)
}

// Lookup 按方法与 gin 路由模板查目录。
func Lookup(method, route string) (Operation, bool) {
	op, ok := registry[strings.ToUpper(strings.TrimSpace(method))+" "+strings.TrimSpace(route)]
	return op, ok
}

// Keys 已登记的全部键，排好序（测试与回填用）。
func Keys() []string {
	keys := make([]string, 0, len(registry))
	for key := range registry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Revision 目录版本。改了任何说法或模块归属就加一：服务启动时据此回填历史日志的模块与类型。
const Revision = 1
