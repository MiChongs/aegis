package main

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// render.go：栈文件与 Traefik 模板的渲染。
//
// 栈文件按 docker compose 的插值语法（$$、$VAR、${VAR}、${VAR:-默认}、${VAR-默认}、
// ${VAR:?提示}、${VAR?提示}）在本机渲染，渲染结果再交给 `docker stack deploy -c -`。
// docker 会对收到的内容再插值一遍，所以输出保持 docker 的转义：原文的 $$ 原样保留，
// 代入的值里的 $ 写成 $$。这样 docker 第二遍插值后得到的正好是想要的值。
// 整行注释不渲染（docker 先解析 YAML 再插值，注释本来就不参与）。

// InterpolateError 缺少必填变量等
type InterpolateError struct {
	Line int
	Msg  string
}

func (e InterpolateError) Error() string { return fmt.Sprintf("第 %d 行：%s", e.Line, e.Msg) }

// Interpolate 渲染 compose 文本。escape=true 时输出保持 docker 转义（见文件头）
func Interpolate(text string, lookup func(string) (string, bool), escape bool) (string, []error) {
	var out strings.Builder
	var errs []error
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			out.WriteString(line)
			continue
		}
		rendered, err := interpolateLine(line, lookup, escape)
		if err != nil {
			errs = append(errs, InterpolateError{Line: i + 1, Msg: err.Error()})
		}
		out.WriteString(rendered)
	}
	return out.String(), errs
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isNameChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }

func interpolateLine(line string, lookup func(string) (string, bool), escape bool) (string, error) {
	var b strings.Builder
	var firstErr error
	emit := func(v string) {
		if escape {
			v = strings.ReplaceAll(v, "$", "$$")
		}
		b.WriteString(v)
	}
	for i := 0; i < len(line); {
		c := line[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(line) {
			b.WriteByte('$')
			i++
			continue
		}
		next := line[i+1]
		switch {
		case next == '$':
			if escape {
				b.WriteString("$$")
			} else {
				b.WriteByte('$')
			}
			i += 2
		case isNameStart(next):
			j := i + 1
			for j < len(line) && isNameChar(line[j]) {
				j++
			}
			v, _ := lookup(line[i+1 : j])
			emit(v)
			i = j
		case next == '{':
			end := strings.IndexByte(line[i+2:], '}')
			if end < 0 {
				if firstErr == nil {
					firstErr = fmt.Errorf("变量引用缺少右括号")
				}
				b.WriteString(line[i:])
				i = len(line)
				continue
			}
			expr := line[i+2 : i+2+end]
			v, err := evalBraced(expr, lookup)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			emit(v)
			i = i + 2 + end + 1
		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("非法的 $ 用法（要写字面的 $ 请写成 $$）")
			}
			b.WriteByte('$')
			i++
		}
	}
	return b.String(), firstErr
}

func evalBraced(expr string, lookup func(string) (string, bool)) (string, error) {
	j := 0
	for j < len(expr) && isNameChar(expr[j]) {
		j++
	}
	name := expr[:j]
	if name == "" || !isNameStart(name[0]) {
		return "", fmt.Errorf("非法的变量名 ${%s}", expr)
	}
	rest := expr[j:]
	val, set := lookup(name)
	switch {
	case rest == "":
		return val, nil
	case strings.HasPrefix(rest, ":-"):
		if val == "" {
			return rest[2:], nil
		}
		return val, nil
	case strings.HasPrefix(rest, "-"):
		if !set {
			return rest[1:], nil
		}
		return val, nil
	case strings.HasPrefix(rest, ":?"):
		if val == "" {
			return "", missingVarError(name, rest[2:])
		}
		return val, nil
	case strings.HasPrefix(rest, "?"):
		if !set {
			return "", missingVarError(name, rest[1:])
		}
		return val, nil
	}
	return "", fmt.Errorf("不支持的写法 ${%s}", expr)
}

func missingVarError(name, msg string) error {
	if msg == "" {
		return fmt.Errorf("缺少必填变量 %s", name)
	}
	return fmt.Errorf("缺少必填变量 %s：%s", name, msg)
}

var varRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:?[-?][^}]*)?\}`)

// VarRef 模板里引用的变量
type VarRef struct {
	Name     string
	Required bool
	Default  string
}

// ReferencedVars 列出模板引用的变量（跳过注释行与 $$ 转义），同名取最严格的那次
func ReferencedVars(text string) []VarRef {
	refs := map[string]*VarRef{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		line = strings.ReplaceAll(line, "$$", "")
		for _, m := range varRefPattern.FindAllStringSubmatch(line, -1) {
			r := refs[m[1]]
			if r == nil {
				r = &VarRef{Name: m[1]}
				refs[m[1]] = r
			}
			op := m[2]
			if strings.HasPrefix(op, ":?") || strings.HasPrefix(op, "?") {
				r.Required = true
			}
			if strings.HasPrefix(op, ":-") {
				r.Default = op[2:]
			}
		}
	}
	out := make([]VarRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ─────────────────────────── 服务子集 ───────────────────────────

var serviceHeader = regexp.MustCompile(`^  ([A-Za-z0-9][A-Za-z0-9_.-]*):\s*$`)

// splitServices 把栈文件切成 services 节之前、各服务块、services 节之后三部分
func splitServices(text string) (head string, blocks []serviceBlock, tail string, err error) {
	lines := strings.SplitAfter(text, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, "\r\n") == "services:" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", nil, "", fmt.Errorf("栈文件里没有顶层的 services:")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r\n")
		if l != "" && l[0] != ' ' && l[0] != '#' {
			end = i
			break
		}
	}
	head = strings.Join(lines[:start+1], "")
	tail = strings.Join(lines[end:], "")
	var cur *serviceBlock
	var preamble strings.Builder
	for _, l := range lines[start+1 : end] {
		if m := serviceHeader.FindStringSubmatch(strings.TrimRight(l, "\r\n")); m != nil {
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			cur = &serviceBlock{Name: m[1]}
		}
		if cur == nil {
			preamble.WriteString(l)
			continue
		}
		cur.Text += l
	}
	if cur != nil {
		blocks = append(blocks, *cur)
	}
	head += preamble.String()
	return head, blocks, tail, nil
}

type serviceBlock struct {
	Name string
	Text string
}

// StackServices 栈文件里的服务名（按出现顺序）
func StackServices(text string) ([]string, error) {
	_, blocks, _, err := splitServices(text)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(blocks))
	for i, b := range blocks {
		names[i] = b.Name
	}
	return names, nil
}

// SelectServices 只保留指定的服务（配合 --prune=false 部署，其余服务不受影响）
func SelectServices(text string, keep map[string]bool) (string, error) {
	head, blocks, tail, err := splitServices(text)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(head)
	n := 0
	for _, blk := range blocks {
		if keep[blk.Name] {
			b.WriteString(blk.Text)
			n++
		}
	}
	if n == 0 {
		return "", fmt.Errorf("选中的服务一个都不在栈文件里")
	}
	b.WriteString(tail)
	return b.String(), nil
}

// ─────────────────────────── Traefik 模板 ───────────────────────────

var traefikNamePattern = regexp.MustCompile(`^[A-Za-z0-9@._-]+$`)

var domainPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`)

// routeGroup 一组域名对应一个后端
type routeGroup struct {
	Name           string
	DomainsKey     string
	Backend        string
	MiddlewaresKey string
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RenderTraefik 渲染 Traefik 动态配置模板
func RenderTraefik(tmplText string, env map[string]string) (string, error) {
	var renderErr error
	fail := func(err error) {
		if renderErr == nil {
			renderErr = err
		}
	}
	funcs := template.FuncMap{
		"env": func(k string) string { return env[k] },
		"envOr": func(k, def string) string {
			if v := env[k]; v != "" {
				return v
			}
			return def
		},
		"hosts": func(k string) string {
			var parts []string
			for _, d := range splitList(env[k]) {
				if !domainPattern.MatchString(d) {
					fail(fmt.Errorf("%s 里的域名不合法：%q", k, d))
					continue
				}
				parts = append(parts, "Host(`"+d+"`)")
			}
			return strings.Join(parts, " || ")
		},
		"list": func(k string) string {
			items := splitList(env[k])
			q := make([]string, len(items))
			for i, it := range items {
				q[i] = strconv.Quote(it)
			}
			return "[" + strings.Join(q, ", ") + "]"
		},
		"groups": func(args ...string) []routeGroup {
			var out []routeGroup
			for i := 0; i+3 < len(args); i += 4 {
				g := routeGroup{Name: args[i], DomainsKey: args[i+1], Backend: args[i+2], MiddlewaresKey: args[i+3]}
				if len(splitList(env[g.DomainsKey])) > 0 {
					out = append(out, g)
				}
			}
			return out
		},
	}
	t, err := template.New("traefik").Funcs(funcs).Option("missingkey=error").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("Traefik 模板解析失败：%w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("Traefik 模板渲染失败：%w", err)
	}
	if renderErr != nil {
		return "", renderErr
	}
	for _, k := range []string{"AEGIS_TRAEFIK_ENTRYPOINT_HTTP", "AEGIS_TRAEFIK_ENTRYPOINT_HTTPS", "AEGIS_TRAEFIK_CERT_RESOLVER", "AEGIS_TRAEFIK_REDIRECT_MIDDLEWARE", "AEGIS_TEMPORAL_UI_MIDDLEWARES"} {
		for _, item := range splitList(env[k]) {
			if !traefikNamePattern.MatchString(item) {
				return "", fmt.Errorf("%s 里有不合法的名字：%q", k, item)
			}
		}
	}
	if p := env["AEGIS_TRAEFIK_PRIORITY"]; p != "" {
		if _, err := strconv.Atoi(p); err != nil {
			return "", fmt.Errorf("AEGIS_TRAEFIK_PRIORITY 要是整数：%q", p)
		}
	}
	switch mode := env["AEGIS_TRAEFIK_HTTP_MODE"]; mode {
	case "", "redirect", "serve", "off":
	default:
		return "", fmt.Errorf("AEGIS_TRAEFIK_HTTP_MODE 只能是 redirect、serve 或 off，现在是 %q", mode)
	}
	return buf.String(), nil
}
