// docsgen 生成「路由 → 请求模型」映射表（internal/transport/http/docs_route_models.go）。
//
// 这张表回答的是一个 OpenAPI 库回答不了的问题：**这条路由的请求体是什么类型**。
// gin 的 handler 签名是 `func(*gin.Context)`，请求类型在运行时已经被擦掉，
// 因此 kin-openapi 那类库只能从「已知的 Go 类型」反射出 schema，
// 却无从知道哪条路由对应哪个类型。缺了这一层，写接口在生成式客户端里
// 就是一个不带参数的空方法 —— 路由再全也调不通。
//
// 结论只有两条路：静态分析源码，或改造全部 handler 的签名。这里走前者。
//
// 做法是把两份事实交叉：
//
//	运行时  装配一次真实路由表，拿到 method + path → handler 符号名
//	静态    加载同一个包的 AST 与类型信息，拿到 handler → 它绑定的具名类型
//
// 两边都不是人手维护的清单，因此不会各自漂移。用法：
//
//	go generate ./internal/transport/http/   重新生成
//	go run ./scripts/docsgen -check          校验磁盘上的表是否已过期（CI 用）
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	httptransport "aegis/internal/transport/http"

	"golang.org/x/tools/go/packages"
)

const (
	targetPackage = "aegis/internal/transport/http"
	defaultOut    = "internal/transport/http/docs_route_models.go"
)

func main() {
	var (
		out   = flag.String("out", defaultOut, "输出文件（相对仓库根）")
		check = flag.Bool("check", false, "只校验磁盘内容是否与生成结果一致，不写文件")
	)
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	outPath := filepath.Join(root, filepath.FromSlash(*out))

	routes, err := loadRoutes()
	if err != nil {
		fail(fmt.Errorf("装配路由表: %w", err))
	}
	bindings, err := loadBindings(root)
	if err != nil {
		fail(fmt.Errorf("分析 handler 绑定: %w", err))
	}

	entries, stats := crossReference(routes, bindings)
	source, err := render(entries, stats)
	if err != nil {
		fail(fmt.Errorf("生成代码: %w", err))
	}

	if *check {
		current, err := os.ReadFile(outPath)
		if err != nil {
			fail(fmt.Errorf("读取 %s: %w", *out, err))
		}
		if !bytes.Equal(normalizeEOL(current), normalizeEOL(source)) {
			fmt.Fprintf(os.Stderr,
				"%s 已过期：handler 的请求绑定变了而映射表没跟上。\n"+
					"跑一次 `go generate ./internal/transport/http/` 并提交产物。\n", *out)
			os.Exit(1)
		}
		fmt.Printf("docsgen: %s 是最新的（%d 条映射）\n", *out, len(entries))
		return
	}

	if err := os.WriteFile(outPath, source, 0o644); err != nil {
		fail(fmt.Errorf("写入 %s: %w", *out, err))
	}
	fmt.Printf("docsgen: 已写入 %s\n", *out)
	report(stats, len(entries))
}

func report(stats genStats, mapped int) {
	fmt.Printf("  映射 %d 条；写接口（POST/PUT/PATCH）覆盖 %d/%d\n",
		mapped, stats.covered, stats.bodyRoutes)
	fmt.Printf("  未覆盖 %d 条，其中 %d 条无请求解析（reset / rotate 这类无参接口，属正常）\n",
		stats.bodyRoutes-stats.covered, stats.noBinding)
	if len(stats.unnameable) == 0 {
		return
	}
	// 这一类是能修好的：把 `var body struct{...}` 提成包级具名类型，
	// 下次生成就自动覆盖到了。列出来才有人会去修。
	fmt.Printf("  另有 %d 条绑定到匿名 struct，提成具名类型即可纳入规范：\n", len(stats.unnameable))
	for _, name := range stats.unnameable {
		fmt.Printf("    %s\n", name)
	}
}

// ── 运行时：真实路由表 ────────────────────────────────────────────────

type routeInfo struct {
	method  string
	path    string
	handler string
}

// loadRoutes 装配一次真实路由引擎并取出路由表。
//
// 走运行时而不是静态解析注册代码：路径是由 router.Group 逐层拼出来的，
// 静态还原等于把 gin 的分组语义重写一遍，而那份重写会和 gin 的真实行为漂移。
func loadRoutes() ([]routeInfo, error) {
	engine, err := httptransport.NewRouter(httptransport.RouterDeps{})
	if err != nil {
		return nil, err
	}
	routes := make([]routeInfo, 0, len(engine.Routes()))
	for _, r := range engine.Routes() {
		routes = append(routes, routeInfo{
			method:  strings.ToUpper(r.Method),
			path:    normalizeOpenAPIPath(r.Path),
			handler: shortHandlerName(r.Handler),
		})
	}
	return routes, nil
}

// normalizeOpenAPIPath 把 gin 的 `:param` 换成 OpenAPI 的 `{param}`。
// 与 httptransport 里的同名函数保持一致（那个是包私有的）。
func normalizeOpenAPIPath(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// shortHandlerName 从 gin 记下的运行时符号名里取出方法名：
// `aegis/internal/transport/http.(*Handler).AppConfig-fm` → `AppConfig`。
func shortHandlerName(full string) string {
	segments := strings.Split(full, ".")
	if len(segments) == 0 {
		return full
	}
	return strings.TrimSuffix(segments[len(segments)-1], "-fm")
}

// ── 静态：handler 绑定了哪个类型 ──────────────────────────────────────

// binding 是一次请求绑定。
type binding struct {
	typeName string
	// pkgPath 为空表示类型就在传输层包内，非空时产物需要 import 它。
	// 相当一部分 handler 直接绑定 service 层的输入结构（PolicyOverrideInput
	// 这类），把它们当成「引用不到」会白白丢掉一批 schema。
	pkgPath string
	// pkgName 是该包声明的真实包名，未必等于路径末段。
	// 靠它才能判断 import 要不要写别名。
	pkgName string
	// query 为真表示这次绑定读的是 query string（ShouldBindQuery），
	// 为假表示读请求体。同一个 handler 同时挂在 GET 与 POST 上时靠它分流。
	query bool
}

// funcNode 是调用图上的一个函数。
//
// 光记绑定是不够的：大量 handler 把请求解析**委托**出去，
// 例如 `AdminEmailConfigCreate` 只有一行 `h.adminEmailConfigSave(c, 0)`，
// `ExperienceTransactions` 只有一行 `h.writeTransactions(c, ...)`。
// 只看 handler 自己的函数体，这些接口会全部漏成「没有请求体」。
type funcNode struct {
	bindings []binding
	// calls 是本包内被调用者的名字，按源码顺序。跨包调用不记：
	// 请求绑定只会发生在传输层自己的代码里。
	calls []string
	// unnameable 表示这里确实绑定了，但目标类型引用不到（匿名 struct）。
	// 与「压根没绑定」分开记，因为只有这一类是能修好的。
	unnameable bool
}

func loadBindings(root string) (map[string]*funcNode, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, targetPackage)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("未加载到包 %s", targetPackage)
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		// 包本身编译不过时静态分析毫无意义，直接把第一条错误抛出去。
		return nil, fmt.Errorf("包 %s 存在编译错误：%v", targetPackage, pkg.Errors[0])
	}

	result := map[string]*funcNode{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			node := collectNode(pkg.TypesInfo, fn.Body)
			if existing, ok := result[fn.Name.Name]; ok {
				existing.bindings = append(existing.bindings, node.bindings...)
				existing.calls = append(existing.calls, node.calls...)
				continue
			}
			result[fn.Name.Name] = node
		}
	}
	return result, nil
}

// collectNode 扫一遍函数体，分出「自己做的绑定」与「转手给了谁」。
//
// 绑定只认五种写法 —— 它们覆盖了本仓库全部 450 处绑定。刻意不做成
// 「任何名字里含 bind 的调用」：那样会把 bindObjectListQuery 这类
// 自己也调绑定的辅助函数算成一次绑定，同一次绑定被记两遍。
func collectNode(info *types.Info, body *ast.BlockStmt) *funcNode {
	node := &funcNode{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if arg, isQuery, ok := bindingTarget(call); ok {
			b, unnameable, ok := namedTypeOf(info, arg)
			switch {
			case ok:
				b.query = isQuery
				node.bindings = append(node.bindings, b)
			case unnameable:
				node.unnameable = true
			}
			return true
		}
		if callee, ok := localCallee(info, call); ok {
			node.calls = append(node.calls, callee)
		}
		return true
	})
	return node
}

// localCallee 返回被调用的本包函数名，跨包调用与非函数调用返回 false。
func localCallee(info *types.Info, call *ast.CallExpr) (string, bool) {
	var ident *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		ident = fn
	case *ast.SelectorExpr:
		ident = fn.Sel
	default:
		return "", false
	}
	obj, ok := info.Uses[ident]
	if !ok {
		return "", false
	}
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != targetPackage {
		return "", false
	}
	return fn.Name(), true
}

// bindingTarget 判断一次调用是不是请求绑定，并返回被绑定的实参。
func bindingTarget(call *ast.CallExpr) (arg ast.Expr, isQuery bool, ok bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// 包级辅助函数：bind(c, &req) / bindLimitedJSON(c, &req, limit)
		switch fn.Name {
		case "bind", "bindLimitedJSON":
			if len(call.Args) >= 2 {
				return call.Args[1], false, true
			}
		}
	case *ast.SelectorExpr:
		// gin 原生：c.ShouldBindJSON(&req) 等
		if len(call.Args) < 1 {
			return nil, false, false
		}
		switch fn.Sel.Name {
		case "ShouldBindJSON", "ShouldBind", "BindJSON":
			return call.Args[0], false, true
		case "ShouldBindQuery":
			return call.Args[0], true, true
		}
	}
	return nil, false, false
}

// namedTypeOf 从 `&req` 这样的实参推出具名 struct 类型。
//
// 接受本包与跨包的具名 struct（跨包的由产物 import 进来）。
// 唯一接不住的是匿名 struct —— 它根本没有名字可引用。
//
// unnameable 把「这压根不是一次绑定」与「是绑定但没名字」分开。后者要报出来：
// 它是唯一一类**能修好**的未覆盖（把 `var body struct{...}` 提成具名类型），
// 混进前者就永远没人知道该修哪。
func namedTypeOf(info *types.Info, arg ast.Expr) (b binding, unnameable bool, ok bool) {
	unary, isUnary := arg.(*ast.UnaryExpr)
	if !isUnary {
		return binding{}, false, false
	}
	typ := info.TypeOf(unary.X)
	if typ == nil {
		return binding{}, false, false
	}
	named, isNamed := typ.(*types.Named)
	if !isNamed {
		if _, isStruct := typ.Underlying().(*types.Struct); isStruct {
			return binding{}, true, false
		}
		return binding{}, false, false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return binding{}, false, false
	}
	if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
		return binding{}, false, false
	}
	// 泛型实例化后的类型没法用一个裸名字写出来，当作没名字处理。
	if named.TypeArgs() != nil && named.TypeArgs().Len() > 0 {
		return binding{}, true, false
	}
	pkgPath := obj.Pkg().Path()
	if pkgPath == targetPackage {
		return binding{typeName: obj.Name()}, false, true
	}
	return binding{
		typeName: obj.Name(),
		pkgPath:  pkgPath,
		pkgName:  obj.Pkg().Name(),
	}, false, true
}

// ── 交叉：路由 × 绑定 ─────────────────────────────────────────────────

type entry struct {
	method  string
	path    string
	model   binding
	handler string
}

type genStats struct {
	bodyRoutes int
	covered    int
	// unnameable 是「绑定了但类型没名字」的写接口，是唯一能修好的那一类缺口。
	unnameable []string
	// noBinding 是压根没解析请求的写接口。绝大多数是 reset / rotate / logout
	// 这类无参 POST，本来就不该有请求体，因此只报数量不逐条列。
	noBinding int
}

func crossReference(routes []routeInfo, funcs map[string]*funcNode) ([]entry, genStats) {
	var (
		entries []entry
		stats   genStats
	)
	for _, r := range routes {
		// HEAD 与 GET 共用契约，不单独进规范，也就不需要模型。
		if r.method == "HEAD" {
			continue
		}
		if !allowsQueryModel(r.method) {
			stats.bodyRoutes++
		}
		picked, ok := resolveBinding(funcs, r.handler, r.method)
		if !ok {
			if !allowsQueryModel(r.method) {
				if node := funcs[r.handler]; node != nil && node.unnameable {
					stats.unnameable = append(stats.unnameable,
						fmt.Sprintf("%s %s  [%s]", r.method, r.path, r.handler))
				} else {
					stats.noBinding++
				}
			}
			continue
		}
		if !allowsQueryModel(r.method) {
			stats.covered++
		}
		entries = append(entries, entry{
			method:  r.method,
			path:    r.path,
			model:   picked,
			handler: r.handler,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].path != entries[j].path {
			return entries[i].path < entries[j].path
		}
		return entries[i].method < entries[j].method
	})
	return entries, stats
}

// maxCallDepth 限制沿调用图下探的层数。
//
// 实测最深的一条是 handler → 业务辅助方法 → bind 包装，三层足够；
// 留到 6 层是给未来的中间层，再深就不像「这个 handler 解析了什么请求」，
// 更像误入了某个通用工具函数。
const maxCallDepth = 6

// resolveBinding 沿调用图找出该 handler 实际解析的请求类型。
//
// 优先级是**自己的绑定压过转手的**：handler 若自己 bind 了，那就是答案；
// 只有它整个身体只是一句转发时才跟下去。反过来会出错 —— 不少 handler
// 先 bind 自己的入参、再调用一个同样 bind 的辅助函数。
func resolveBinding(funcs map[string]*funcNode, handler, method string) (binding, bool) {
	visited := map[string]bool{}

	var walk func(name string, depth int) (binding, bool)
	walk = func(name string, depth int) (binding, bool) {
		if depth > maxCallDepth || visited[name] {
			return binding{}, false
		}
		visited[name] = true

		node, ok := funcs[name]
		if !ok {
			return binding{}, false
		}
		if picked, ok := pickBinding(node.bindings, method); ok {
			return picked, true
		}
		// 按源码顺序跟进：先出现的调用更可能是「解析请求」那一步。
		for _, callee := range node.calls {
			if picked, ok := walk(callee, depth+1); ok {
				return picked, true
			}
		}
		return binding{}, false
	}

	return walk(handler, 0)
}

// pickBinding 在多次绑定里挑出与该 HTTP 方法相称的那次。
//
// 同一个 handler 挂在 GET 与 POST 上是本仓库的常态（兼容命名空间大量如此）。
// 不按方法分流的话，GET 会拿到请求体模型、被渲染成一串 query 参数，
// 而那些字段根本不是 query 参数。
func pickBinding(found []binding, method string) (binding, bool) {
	if len(found) == 0 {
		return binding{}, false
	}
	wantQuery := allowsQueryModel(method)
	for _, b := range found {
		if b.query == wantQuery {
			return b, true
		}
	}
	// 没有相称的就退回第一次绑定：handler 用 bind() 统一收 query 与 body
	// （bind 内部就是 ShouldBind，两者都吃），这时不存在分流问题。
	return found[0], true
}

func allowsQueryModel(method string) bool {
	return method == "GET" || method == "HEAD"
}

// ── 渲染 ──────────────────────────────────────────────────────────────

// importedPkg 是产物要 import 的一个包。
type importedPkg struct {
	path string
	// alias 为空表示按包名直接引用，不写别名。
	alias string
	// ref 是在代码里限定类型时用的前缀（别名或包名）。
	ref string
}

// resolveImports 决定每个外部包怎么 import。
//
// 结果必须只由包路径决定：若受 entries 顺序影响，同一份源码两次生成会产出
// 不同的文件，-check 便会无缘无故地红。因此按路径排序后依次处理。
//
// 只在包名撞车时才起别名 —— 给 `aegis/internal/service` 写上
// `service "aegis/internal/service"` 是条冗余别名，gofmt 不管，
// 但读的人会以为这里有什么讲究。
func resolveImports(entries []entry) map[string]importedPkg {
	names := map[string]string{}
	for _, e := range entries {
		if e.model.pkgPath != "" {
			names[e.model.pkgPath] = e.model.pkgName
		}
	}
	paths := make([]string, 0, len(names))
	for p := range names {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	result := map[string]importedPkg{}
	used := map[string]bool{}
	for _, p := range paths {
		name := names[p]
		if name == "" {
			name = path.Base(p)
		}
		if !used[name] {
			used[name] = true
			result[p] = importedPkg{path: p, ref: name}
			continue
		}
		alias := name
		for i := 2; used[alias]; i++ {
			alias = fmt.Sprintf("%s%d", name, i)
		}
		used[alias] = true
		result[p] = importedPkg{path: p, alias: alias, ref: alias}
	}
	return result
}

func render(entries []entry, stats genStats) ([]byte, error) {
	var buf bytes.Buffer
	imports := resolveImports(entries)

	buf.WriteString(`package httptransport

// 本文件由 scripts/docsgen 生成，请勿手工编辑。
//
// 重新生成：
//
//	go generate ./internal/transport/http/
//
// 生成方式是把两份事实交叉，两边都不是人手维护的清单：
//
//	运行时  装配真实路由表，得到 method + path → handler
//	静态    用 x/tools 加载本包的类型信息，得到 handler → 它绑定的具名类型
//
// 它解决的是「生成式客户端拿到一堆没有参数的空方法」：openapi-generator 只认
// requestBody / parameters，路由再全，缺了 schema 也生成不出可用的客户端。
//
// 覆盖不到的情况有两类，都不是遗漏：
//   1. 请求体是匿名 struct（var body struct{...}），没有类型名可引用；
//   2. 该接口本来就没有请求体（rotate / reset / logout 这类无参 POST）。
//
// 手工登记的 manualRouteDocs 与网关的 gatewayRouteDocs 会覆盖本表同名条目。
`)

	if len(imports) > 0 {
		// 有 handler 直接绑定其他层的输入结构（service 的 XxxInput 之类），
		// 把它们 import 进来才能把那些接口的 schema 一并产出。
		buf.WriteString("\nimport (\n")
		paths := make([]string, 0, len(imports))
		for p := range imports {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if alias := imports[p].alias; alias != "" {
				fmt.Fprintf(&buf, "\t%s %q\n", alias, p)
				continue
			}
			fmt.Fprintf(&buf, "\t%q\n", p)
		}
		buf.WriteString(")\n")
	}

	buf.WriteString(`
func generatedRouteModels() map[string]any {
	return map[string]any{
`)

	// 让 `routeKey(...)` 与右侧的类型字面量各自对齐成一列。
	// gofmt 只对齐相邻行里已经存在的制表位，键长度差得多时它不会重排，
	// 因此这里自己算一次宽度，产物才稳定（否则每次生成都可能有空白差异）。
	keys := make([]string, len(entries))
	width := 0
	for i, e := range entries {
		keys[i] = fmt.Sprintf("routeKey(%q, %q):", e.method, e.path)
		if len(keys[i]) > width {
			width = len(keys[i])
		}
	}
	values := make([]string, len(entries))
	valueWidth := 0
	for i, e := range entries {
		name := e.model.typeName
		if e.model.pkgPath != "" {
			name = imports[e.model.pkgPath].ref + "." + name
		}
		values[i] = name + "{},"
		if len(values[i]) > valueWidth {
			valueWidth = len(values[i])
		}
	}
	for i, e := range entries {
		fmt.Fprintf(&buf, "\t\t%-*s %-*s // %s\n",
			width, keys[i], valueWidth, values[i], e.handler)
	}

	buf.WriteString("\t}\n}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt 失败: %w", err)
	}
	return formatted, nil
}

// ── 杂项 ──────────────────────────────────────────────────────────────

// repoRoot 从当前目录向上找到含 go.mod 的目录。
// 这样 `go generate`（工作目录是被生成的包）与手工在仓库根执行都能跑。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从 %s 向上找不到 go.mod", dir)
		}
		dir = parent
	}
}

// normalizeEOL 统一换行符后再比对：仓库在 Windows 上检出时是 CRLF，
// 生成结果永远是 LF，不归一化的话 -check 在 Windows 上永远失败。
func normalizeEOL(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "docsgen: %v\n", err)
	os.Exit(1)
}
