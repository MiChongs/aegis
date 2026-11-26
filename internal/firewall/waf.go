// Package firewall 是 Aegis 防火墙的引擎层（与 gin 无关）：Coraza WAF 的编译与检测、
// 规则库目录、自定义规则（expr 表达式）与网段集合。gin 适配在 internal/middleware/firewall.go。
//
// 设计说明见 docs/firewall.md。最重要的一条：WAF 在**解密之后**检测——
// Transport v2（sealed）与旧 X-Aegis-Encrypted 两种加密请求在入口处只是一段密文，
// 在那里跑 CRS 等于什么都没检查，还会让 base64 密文偶发误报。
package firewall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/experimental"
	"github.com/corazawaf/coraza/v3/experimental/plugins/plugintypes"
	"github.com/corazawaf/coraza/v3/types"
)

// Options WAF 编译参数（由 config.FirewallConfig 与例外表组装，见 middleware.firewallOptions）。
type Options struct {
	DetectionOnly      bool
	Paranoia           int
	DetectionParanoia  int
	InboundThreshold   int
	OutboundThreshold  int
	ResponseInspection bool
	ResponseBodyLimit  int
	RequestBodyLimit   int
	RequestBodyMemory  int
	ArgumentsLimit     int
	DisabledGroups     []string
	RemovedRules       []string
	InspectAdminBody   bool
	BodyExemptPrefixes []string
	SensitiveArgs      []string
	RichTextArgs       []string
	ExtraContentTypes  []string
	Exclusions         []Exclusion
}

func (o Options) normalized() directiveOptions {
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	paranoia := clamp(o.Paranoia, 1, 4)
	detection := clamp(o.DetectionParanoia, paranoia, 4)
	inbound := o.InboundThreshold
	if inbound <= 0 {
		inbound = 5
	}
	outbound := o.OutboundThreshold
	if outbound <= 0 {
		outbound = 4
	}
	bodyLimit := o.RequestBodyLimit
	if bodyLimit <= 0 {
		bodyLimit = 13 * 1024 * 1024
	}
	bodyMemory := o.RequestBodyMemory
	if bodyMemory <= 0 || bodyMemory > bodyLimit {
		bodyMemory = min(256*1024, bodyLimit)
	}
	respLimit := o.ResponseBodyLimit
	if respLimit <= 0 {
		respLimit = 512 * 1024
	}
	args := o.ArgumentsLimit
	if args <= 0 {
		args = 1000
	}
	disabled := make(map[string]bool, len(o.DisabledGroups))
	for _, g := range o.DisabledGroups {
		disabled[strings.TrimSpace(g)] = true
	}
	return directiveOptions{
		DetectionOnly:      o.DetectionOnly,
		Paranoia:           paranoia,
		DetectionParanoia:  detection,
		InboundThreshold:   inbound,
		OutboundThreshold:  outbound,
		ResponseInspection: o.ResponseInspection,
		ResponseBodyLimit:  respLimit,
		RequestBodyLimit:   bodyLimit,
		RequestBodyMemory:  bodyMemory,
		ArgumentsLimit:     args,
		DisabledGroups:     disabled,
		RemovedRules:       o.RemovedRules,
		InspectAdminBody:   o.InspectAdminBody,
		BodyExemptPrefixes: o.BodyExemptPrefixes,
		SensitiveArgs:      o.SensitiveArgs,
		RichTextArgs:       o.RichTextArgs,
		ExtraContentTypes:  o.ExtraContentTypes,
		Exclusions:         o.Exclusions,
		TempDir:            strings.ReplaceAll(os.TempDir(), "\\", "/"),
	}
}

// WAF 一份编译好的 Coraza 实例 + 它的规则目录。不可变，热更新时整份替换。
type WAF struct {
	waf        coraza.WAF
	opts       directiveOptions
	directives string
	catalog    *Catalog
	sensitive  *regexp.Regexp
}

// CompileWAF 生成指令并编译。失败时返回的错误带着 Coraza 的解析信息（哪一行、哪条规则）。
func CompileWAF(o Options) (*WAF, error) {
	opts := o.normalized()
	directives := buildDirectives(opts)
	cfg := coraza.NewWAFConfig().
		WithRootFS(mergedFS{crs: coreruleset.FS}).
		WithDirectives(directives)
	waf, err := coraza.NewWAF(cfg)
	if err != nil {
		return nil, fmt.Errorf("编译 WAF 规则失败: %w", err)
	}
	w := &WAF{
		waf:        waf,
		opts:       opts,
		directives: directives,
		catalog:    buildCatalog(opts),
	}
	if rx := argNameRegex(o.SensitiveArgs); rx != "" {
		w.sensitive, _ = regexp.Compile(strings.Trim(rx, "/"))
	}
	return w, nil
}

// Directives 生成的完整指令文本（控制台「规则集」页可以查看，排查例外有没有拼对）。
func (w *WAF) Directives() string { return w.directives }

// Catalog 规则目录。
func (w *WAF) Catalog() *Catalog { return w.catalog }

// DetectionOnly 是否观察模式。
func (w *WAF) DetectionOnly() bool { return w.opts.DetectionOnly }

// InboundThreshold 入站阈值。
func (w *WAF) InboundThreshold() int { return w.opts.InboundThreshold }

// ResponseInspection 是否开启出站检测。
func (w *WAF) ResponseInspection() bool { return w.opts.ResponseInspection }

// RequestBodyLimit 请求体检查上限。
func (w *WAF) RequestBodyLimit() int { return w.opts.RequestBodyLimit }

// ResponseBodyLimit 出站检查上限。
func (w *WAF) ResponseBodyLimit() int { return w.opts.ResponseBodyLimit }

// Close 释放 Coraza 持有的资源（实现了 io.Closer 的版本才有）。
func (w *WAF) Close() error {
	if closer, ok := w.waf.(experimental.WAFCloser); ok {
		return closer.Close()
	}
	return nil
}

// Request 一次待检请求（已是明文：解密中间件之后的状态）。
type Request struct {
	RemoteIP   string
	RemotePort int
	Method     string
	// URI 路径 + 原始查询串（sealed GET 的查询串此时已是解开的明文）。
	URI     string
	Proto   string
	Host    string
	Headers http.Header
	// Transport 载荷来源：plain / sealed / legacy，写进 TX:AEGIS_TRANSPORT 供专属规则使用。
	Transport string
	// ContentLength 实际要喂给引擎的请求体字节数（-1 = 不知道）。解密中间件换过请求体，
	// 原来的 Content-Length 头是密文长度或已被删掉，这里按明文重写，920180 等规则才不会误报。
	ContentLength int64
}

// Match 一条命中的规则（已截断、已脱敏）。
type Match struct {
	RuleID   int      `json:"id"`
	Message  string   `json:"message"`
	Data     string   `json:"data,omitempty"`
	Severity string   `json:"severity"`
	Phase    int      `json:"phase"`
	Group    string   `json:"group"`
	Tags     []string `json:"tags,omitempty"`
	Targets  []string `json:"targets,omitempty"`
}

// Result 一次检测（请求或响应阶段）的结论。
type Result struct {
	// Interrupted 引擎实际中断了请求（拦截模式下越线，或基线规则 deny）。
	Interrupted bool
	// WouldBlock 观察模式下本该被拦（入站分越线或基线规则命中）。拦截模式下与 Interrupted 相同。
	WouldBlock bool
	RuleID     int
	Status     int
	Action     string
	// InboundScore / OutboundScore 计入拦截的异常分（blocking_*_anomaly_score）。
	InboundScore  int
	OutboundScore int
	Matches       []Match
}

// Txn 一次请求的 WAF 事务：请求阶段 → （可选）响应阶段 → Close。
type Txn struct {
	waf *WAF
	tx  types.Transaction
}

// Begin 开启事务并喂入连接与请求头（phase 1）。返回的 Txn 必须 Close。
func (w *WAF) Begin(req Request) (*Txn, *Result) {
	tx := w.waf.NewTransaction()
	t := &Txn{waf: w, tx: tx}
	if tx.IsRuleEngineOff() {
		return t, &Result{}
	}
	if state, ok := tx.(plugintypes.TransactionState); ok {
		transport := req.Transport
		if transport == "" {
			transport = "plain"
		}
		state.Variables().TX().Set("aegis_transport", []string{transport})
	}
	tx.ProcessConnection(req.RemoteIP, req.RemotePort, "", 0)
	proto := req.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	tx.ProcessURI(req.URI, req.Method, proto)
	for key, values := range req.Headers {
		if strings.EqualFold(key, "Content-Length") && req.ContentLength >= 0 {
			continue
		}
		for _, value := range values {
			tx.AddRequestHeader(key, value)
		}
	}
	if req.ContentLength > 0 {
		tx.AddRequestHeader("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	}
	if req.Host != "" {
		tx.AddRequestHeader("Host", req.Host)
		tx.SetServerName(req.Host)
	}
	if it := tx.ProcessRequestHeaders(); it != nil {
		return t, t.result(it)
	}
	return t, nil
}

// BodyAccessible phase 1 之后引擎是否还要请求体（管理端、AI 中转等调优档会关掉）。
func (t *Txn) BodyAccessible() bool {
	return t.tx.IsRequestBodyAccessible()
}

// RequestBody 喂入请求体并完成 phase 2。body 为 nil 表示无请求体或刻意跳过请求体检查。
func (t *Txn) RequestBody(body io.Reader) (*Result, error) {
	if t.tx.IsRuleEngineOff() {
		return &Result{}, nil
	}
	if body != nil && t.tx.IsRequestBodyAccessible() {
		it, _, err := t.tx.ReadRequestBodyFrom(body)
		if err != nil {
			return nil, fmt.Errorf("写入请求体: %w", err)
		}
		if it != nil {
			return t.result(it), nil
		}
	}
	it, err := t.tx.ProcessRequestBody()
	if err != nil {
		return nil, fmt.Errorf("处理请求体: %w", err)
	}
	return t.result(it), nil
}

// ResponseAccessible 是否需要响应体（出站检测开启且 Content-Type 在检查清单里）。
func (t *Txn) ResponseAccessible() bool {
	return t.waf.opts.ResponseInspection && t.tx.IsResponseBodyAccessible()
}

// Response 喂入响应头与（截断后的）响应体，完成 phase 3、4。出站检测只记录，不会改写已发出的响应。
func (t *Txn) Response(status int, headers http.Header, body []byte) (*Result, error) {
	if !t.waf.opts.ResponseInspection || t.tx.IsRuleEngineOff() {
		return &Result{}, nil
	}
	for key, values := range headers {
		for _, value := range values {
			t.tx.AddResponseHeader(key, value)
		}
	}
	if it := t.tx.ProcessResponseHeaders(status, "HTTP/1.1"); it != nil {
		return t.result(it), nil
	}
	if len(body) > 0 && t.tx.IsResponseBodyProcessable() {
		if it, _, err := t.tx.WriteResponseBody(body); err != nil {
			return nil, fmt.Errorf("写入响应体: %w", err)
		} else if it != nil {
			return t.result(it), nil
		}
	}
	it, err := t.tx.ProcessResponseBody()
	if err != nil {
		return nil, fmt.Errorf("处理响应体: %w", err)
	}
	return t.result(it), nil
}

// Close 写日志阶段并关闭事务。
func (t *Txn) Close() error {
	t.tx.ProcessLogging()
	return t.tx.Close()
}

// Snapshot 不推进阶段，只读取当前的命中与分数（请求阶段已结束、响应阶段之前用）。
func (t *Txn) Snapshot() *Result { return t.result(nil) }

// blockingRuleIDs 「越线即拦」的评估规则：CRS 949110（入站）、959100（出站）。
var blockingRuleIDs = map[int]bool{949110: true, 959100: true}

func (t *Txn) result(it *types.Interruption) *Result {
	res := &Result{}
	if it != nil {
		res.Interrupted = true
		res.WouldBlock = true
		res.RuleID = it.RuleID
		res.Status = it.Status
		res.Action = it.Action
	}
	if state, ok := t.tx.(plugintypes.TransactionState); ok {
		col := state.Variables().TX()
		res.InboundScore = firstInt(col.Get("blocking_inbound_anomaly_score"))
		res.OutboundScore = firstInt(col.Get("blocking_outbound_anomaly_score"))
	}
	seen := make(map[int]int)
	for _, matched := range t.tx.MatchedRules() {
		meta := matched.Rule()
		id := meta.ID()
		if id <= 0 {
			continue
		}
		disruptiveBaseline := (id >= 200002 && id <= 200005) || id == 200009
		// pass,nolog 的辅助规则（初始化、跳转、ctl）也在 MatchedRules 里，没有 msg 的一律不算命中。
		if matched.Message() == "" && !disruptiveBaseline {
			continue
		}
		if blockingRuleIDs[id] || disruptiveBaseline {
			if !res.WouldBlock {
				res.WouldBlock = true
				res.RuleID = id
				if res.Status == 0 {
					res.Status = http.StatusForbidden
					if disruptiveBaseline {
						res.Status = http.StatusBadRequest
					}
				}
				if res.Action == "" {
					res.Action = "deny"
				}
			}
		}
		if idx, ok := seen[id]; ok {
			res.Matches[idx].Targets = appendTargets(res.Matches[idx].Targets, matched.MatchedDatas(), 6)
			continue
		}
		m := Match{
			RuleID:   id,
			Message:  truncate(matched.Message(), 200),
			Data:     t.maskedData(matched),
			Severity: severityLabel(meta.Severity()),
			Phase:    int(meta.Phase()),
			Group:    groupOf(meta.File()),
			Tags:     interestingTags(meta.Tags()),
			Targets:  appendTargets(nil, matched.MatchedDatas(), 6),
		}
		if m.Group == "" && strings.Contains(meta.File(), "aegis") {
			m.Group = "aegis"
		}
		if m.Group == "" && id >= 1100000 {
			m.Group = "aegis"
		}
		if m.Group == "" && id >= 200000 && id < 300000 {
			m.Group = "baseline"
		}
		seen[id] = len(res.Matches)
		res.Matches = append(res.Matches, m)
		if len(res.Matches) >= 40 {
			break
		}
	}
	sort.SliceStable(res.Matches, func(i, j int) bool { return res.Matches[i].Phase < res.Matches[j].Phase })
	return res
}

// maskedData 命中数据：敏感字段（口令、密钥）只留字段名，值打码；其余截断到 200 字符。
func (t *Txn) maskedData(matched types.MatchedRule) string {
	data := matched.Data()
	if t.waf.sensitive != nil {
		for _, md := range matched.MatchedDatas() {
			if t.waf.sensitive.MatchString(md.Key()) && md.Value() != "" {
				data = strings.ReplaceAll(data, md.Value(), "******")
			}
		}
	}
	return truncate(data, 200)
}

func appendTargets(targets []string, datas []types.MatchData, limit int) []string {
	for _, md := range datas {
		if len(targets) >= limit {
			break
		}
		name := md.Variable().Name()
		if key := md.Key(); key != "" {
			name += ":" + key
		}
		name = truncate(name, 120)
		dup := false
		for _, existing := range targets {
			if existing == name {
				dup = true
				break
			}
		}
		if !dup {
			targets = append(targets, name)
		}
	}
	return targets
}

// interestingTags 只保留对排查有用的标签（攻击类别、paranoia、aegis/*），去掉 OWASP_CRS、capec 这类长串。
func interestingTags(tags []string) []string {
	out := make([]string, 0, 4)
	for _, tag := range tags {
		switch {
		case strings.HasPrefix(tag, "attack-"), strings.HasPrefix(tag, "paranoia-level/"), strings.HasPrefix(tag, "aegis/"), strings.HasPrefix(tag, "language-"), strings.HasPrefix(tag, "platform-"):
			out = append(out, tag)
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func severityLabel(s types.RuleSeverity) string {
	switch s {
	case types.RuleSeverityEmergency, types.RuleSeverityAlert, types.RuleSeverityCritical:
		return "critical"
	case types.RuleSeverityError:
		return "error"
	case types.RuleSeverityWarning:
		return "warning"
	case types.RuleSeverityNotice:
		return "notice"
	case types.RuleSeverityUnset:
		return ""
	default:
		return s.String()
	}
}

func firstInt(values []string) int {
	if len(values) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(values[0]))
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ErrBodyTooLarge 非上传类请求体超过检查上限。
var ErrBodyTooLarge = errors.New("request body exceeds firewall inspection limit")

// EffectiveContentType 交给 WAF 的内容类型。
//
// gin 的 ShouldBindJSON 不看 Content-Type：声明成 text/plain、octet-stream 或干脆不带类型、
// 内容却是 JSON 的请求，处理器照样当 JSON 解析。WAF 若按声明的类型走（RAW 或 urlencoded），
// 攻击串就在 JSON 字段里原封不动地过去了。这里按内容改判：看起来是 JSON 就当 JSON 检查。
// 只改 WAF 看到的头，不改请求本身。
func EffectiveContentType(declared string, body []byte) string {
	lower := strings.ToLower(strings.TrimSpace(declared))
	switch {
	case strings.HasPrefix(lower, "application/json"),
		strings.Contains(lower, "+json"),
		strings.HasPrefix(lower, "multipart/"),
		strings.HasPrefix(lower, "application/x-www-form-urlencoded"),
		strings.Contains(lower, "xml"):
		return declared
	}
	trimmed := bytesTrimLeftSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && jsonValid(body) {
		return "application/json"
	}
	return declared
}

func bytesTrimLeftSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	return b
}

func jsonValid(b []byte) bool { return json.Valid(b) }
