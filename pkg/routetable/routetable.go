// Package routetable 渲染 HTTP 路由清单。
//
// 分工与 pkg/banner 一致：本包只管「怎么画」——分组表格、树形、Markdown、CSV、
// HTML、JSON 六种形态，以及方法着色、宽度自适应与终端能力降级；
// 「画什么」（哪条路由归哪个命名空间、要标什么鉴权）由调用方组装成 Inventory 传进来。
// 因此本包不 import gin，也不认识任何业务前缀，可以被任何 HTTP 框架复用。
//
// 存在的理由：gin 在 debug 档会把每条路由打成一行
//
//	[GIN-debug] GET /api/v1/apps/:appkey/config --> pkg.(*Handler).AppConfig-fm (14 handlers)
//
// 路由上千条时这就是上千行无法阅读的滚屏，而且 release 档下它整段消失，
// 于是「这个部署到底暴露了哪些接口」在生产环境反而无从查证。
// 把它换成一张分组、可过滤、可导出的清单，两种档位下都能看。
//
// 全部交给依赖库，不自己造轮子：
//
//	github.com/jedib0t/go-pretty/v6/table  表格排版、纵向合并、空列抑制、
//	                                       Markdown / CSV / HTML 导出
//	github.com/jedib0t/go-pretty/v6/list   树形排版（连接线样式与缩进）
//	github.com/jedib0t/go-pretty/v6/text   ANSI 着色与 CJK 宽度计算
//	aegis/pkg/banner                       终端能力探测（TTY / 列宽 / 着色开关）
package routetable

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Route 是清单里的一条路由。
type Route struct {
	Method string `json:"method"`
	// Path 是框架原样的注册路径，含 :param 占位符。
	// 刻意不做美化：这一列的用途是被复制进 curl 或 grep，改写会让它不可用。
	Path string `json:"path"`
	// Handler 是处理器短名（去掉包路径与 gin 的 -fm 后缀）。
	Handler string `json:"handler,omitempty"`
	// Chain 是中间件链长度（含最终 handler）。0 表示未采集到——
	// gin 只在 debug 档回调 DebugPrintRouteFunc，release 档拿不到这个数。
	Chain int `json:"chain,omitempty"`
	// Auth 是鉴权标注，由调用方按命名空间规则给出。
	Auth string `json:"auth,omitempty"`
	Note string `json:"note,omitempty"`
}

// Group 是一个命名空间分组。
type Group struct {
	// Realm 是更粗一档的顶层域（公开 / 网关 / 管理端 …），
	// 供只放得下几行的场合（启动横幅）聚合使用。
	Realm string `json:"realm,omitempty"`
	Title string `json:"title"`
	// Prefix 是该组的公共路径前缀，仅用于展示与排序，不参与匹配。
	Prefix string  `json:"prefix,omitempty"`
	Auth   string  `json:"auth,omitempty"`
	Note   string  `json:"note,omitempty"`
	Routes []Route `json:"routes"`
}

// Inventory 是一次渲染的完整输入。
type Inventory struct {
	Title  string  `json:"title,omitempty"`
	Groups []Group `json:"groups"`
}

// Total 返回路由总条数。
func (inv Inventory) Total() int {
	n := 0
	for _, g := range inv.Groups {
		n += len(g.Routes)
	}
	return n
}

// RealmCount 是一个顶层域的聚合结果。
type RealmCount struct {
	Realm  string
	Routes int
	Groups int
	// Auth 是该域内出现过的鉴权标注，按首次出现顺序去重。
	Auth []string
}

// Realms 按顶层域聚合，保持 Groups 的原始顺序。
// 启动横幅只放得下几行，用它而不是 38 个分组。
func (inv Inventory) Realms() []RealmCount {
	index := map[string]int{}
	out := make([]RealmCount, 0, 8)
	for _, g := range inv.Groups {
		if len(g.Routes) == 0 {
			continue
		}
		realm := g.Realm
		if realm == "" {
			realm = g.Title
		}
		i, ok := index[realm]
		if !ok {
			index[realm] = len(out)
			out = append(out, RealmCount{Realm: realm})
			i = len(out) - 1
		}
		out[i].Routes += len(g.Routes)
		out[i].Groups++
		if g.Auth != "" && !slices.Contains(out[i].Auth, g.Auth) {
			out[i].Auth = append(out[i].Auth, g.Auth)
		}
	}
	return out
}

// Query 描述一次过滤。零值表示不过滤。
type Query struct {
	// Path 是路径子串，大小写不敏感。
	Path string
	// Methods 是方法白名单；空表示全部。
	Methods []string
	// Group 是分组名或顶层域的子串，大小写不敏感。
	Group string
	// Auth 是鉴权标注的子串，大小写不敏感。
	Auth string
}

// Empty 报告该查询是否不做任何过滤。
func (q Query) Empty() bool {
	return q.Path == "" && len(q.Methods) == 0 && q.Group == "" && q.Auth == ""
}

// ParseMethods 把 "get,post" 这类输入拆成规范化的方法白名单。
func ParseMethods(raw string) []string {
	out := []string{}
	for part := range strings.SplitSeq(raw, ",") {
		if part = strings.ToUpper(strings.TrimSpace(part)); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Filter 返回只含命中路由的新清单。整组被过滤空时该组一并消失，
// 因此渲染出来的计数永远与看到的行数一致。
func (inv Inventory) Filter(q Query) Inventory {
	if q.Empty() {
		return inv
	}
	out := Inventory{Title: inv.Title, Groups: make([]Group, 0, len(inv.Groups))}
	for _, g := range inv.Groups {
		if q.Group != "" && !containsFold(g.Title, q.Group) && !containsFold(g.Realm, q.Group) {
			continue
		}
		kept := make([]Route, 0, len(g.Routes))
		for _, r := range g.Routes {
			if q.Path != "" && !containsFold(r.Path, q.Path) {
				continue
			}
			if len(q.Methods) > 0 && !slices.Contains(q.Methods, strings.ToUpper(r.Method)) {
				continue
			}
			// 路由自己没标鉴权时回落到组的标注，否则按组鉴权的路由会被 --auth 漏掉
			if q.Auth != "" && !containsFold(firstNonEmpty(r.Auth, g.Auth), q.Auth) {
				continue
			}
			kept = append(kept, r)
		}
		if len(kept) == 0 {
			continue
		}
		g.Routes = kept
		out.Groups = append(out.Groups, g)
	}
	return out
}

// methodOrder 是方法的展示顺序：按 HTTP 语义从「读」到「写」到「元信息」，
// 而不是字典序——字典序会把 DELETE 排在 GET 前面，读这张表的人一眼看到的
// 第一行是删除接口，观感与实际风险都不对。
var methodOrder = map[string]int{
	"GET": 0, "HEAD": 1, "POST": 2, "PUT": 3, "PATCH": 4, "DELETE": 5, "OPTIONS": 6,
}

// Sort 在组内按路径、再按方法语义序排序，并保持组的声明顺序不变。
//
// 组顺序刻意不排序：调用方声明规则表的顺序本身携带信息
// （公开 → 网关 → 管理端 → 用户端），按标题重排会把这个层次打散。
func (inv Inventory) Sort() Inventory {
	out := Inventory{Title: inv.Title, Groups: make([]Group, 0, len(inv.Groups))}
	for _, g := range inv.Groups {
		routes := make([]Route, len(g.Routes))
		copy(routes, g.Routes)
		sort.SliceStable(routes, func(i, j int) bool {
			if routes[i].Path != routes[j].Path {
				return routes[i].Path < routes[j].Path
			}
			return methodRank(routes[i].Method) < methodRank(routes[j].Method)
		})
		g.Routes = routes
		out.Groups = append(out.Groups, g)
	}
	return out
}

// methodRank 未知方法排在已知方法之后，但彼此保持稳定顺序。
func methodRank(method string) int {
	if rank, ok := methodOrder[strings.ToUpper(strings.TrimSpace(method))]; ok {
		return rank
	}
	return len(methodOrder)
}

// MarshalJSON 输出机器可读的清单，附带总数便于消费方校验。
func (inv Inventory) MarshalJSONIndent() ([]byte, error) {
	payload := struct {
		Title  string  `json:"title,omitempty"`
		Total  int     `json:"total"`
		Groups []Group `json:"groups"`
	}{Title: inv.Title, Total: inv.Total(), Groups: inv.Groups}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
