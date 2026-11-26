package firewall

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// rulesFS 内嵌的 Aegis 规则文件，与 CRS 的文件系统合并后交给 Coraza（Include @aegis/...）。
//
//go:embed rules/*.conf
var rulesFS embed.FS

// AllowedMethods CRS 911100（METHOD-ENFORCEMENT）的放行方法集。
//
// CRS 默认只放行 GET HEAD POST OPTIONS，而 Aegis 是 REST 风格（路由表里 PUT、DELETE、PATCH 都有）。
// 刻意选择「设置放行集」而不是移除 911100：后者会连 TRACE / TRACK / CONNECT 一起放行。
// 这份清单必须跟随路由表，transport 层的 TestRegisteredRouteMethodsAreAllowedByWAF 反向核对。
const AllowedMethods = "GET HEAD POST PUT PATCH DELETE OPTIONS"

// 规则 ID 段（docs/firewall.md「规则编号」）：
//
//	200000-200009    引擎基线（与 coraza.conf-recommended 3.8.1 同号同义）
//	1100001-1100099  Aegis 对 CRS 的设置（阈值、偏执等级、放行集）
//	1100100-1100999  Aegis 调优档（按路径 / 字段的精确排除）
//	1101000-1101999  Aegis 专属检测规则（rules/aegis-protection.conf）
//	1190000-1199999  管理员在控制台维护的 WAF 例外（按顺序编号）
const (
	exclusionRuleIDBase = 1190000
	exclusionRuleIDMax  = 1199999
)

// Exclusion 一条 WAF 例外（控制台「WAF 例外」页维护，表 firewall_waf_exclusions）。
//
// 语义与 CRS 官方调优建议一致：能按路径、按字段排除的，绝不全局移除规则。
//   - Targets 为空：在匹配的请求上整条移除 RuleIDs / Tags 对应的规则；
//   - Targets 非空：只把这些变量（如 ARGS:json.content）从规则的检查目标里拿掉。
type Exclusion struct {
	ID        int64
	Name      string
	PathMatch string // prefix | exact | regex
	Path      string
	Methods   []string
	RuleIDs   []int
	Tags      []string
	Targets   []string
}

var (
	exclusionTagPattern    = regexp.MustCompile(`^[A-Za-z0-9/_.:-]{1,64}$`)
	exclusionTargetPattern = regexp.MustCompile(`^(?:ARGS|ARGS_GET|ARGS_POST|ARGS_NAMES|ARGS_GET_NAMES|ARGS_POST_NAMES|REQUEST_HEADERS|REQUEST_HEADERS_NAMES|REQUEST_COOKIES|REQUEST_COOKIES_NAMES|REQUEST_FILENAME|REQUEST_BASENAME|REQUEST_URI|REQUEST_BODY|FILES|FILES_NAMES|XML)(?::(?:[A-Za-z0-9_.\-\[\]]{1,128}|/[^/\s"';]{1,200}/))?$`)
	methodPattern          = regexp.MustCompile(`^[A-Z]{3,10}$`)
)

// ValidateExclusion 在入库前检查一条例外能否安全地拼进指令（引号、换行、分号都会改写指令结构）。
func ValidateExclusion(e Exclusion) error {
	switch e.PathMatch {
	case "prefix", "exact":
		if !strings.HasPrefix(e.Path, "/") {
			return fmt.Errorf("路径必须以 / 开头")
		}
		if strings.ContainsAny(e.Path, "\"'\\\r\n ") {
			return fmt.Errorf("路径不能包含引号、反斜杠、空白或换行")
		}
	case "regex":
		if strings.ContainsAny(e.Path, "\"\r\n") {
			return fmt.Errorf("正则不能包含双引号或换行")
		}
		if _, err := regexp.Compile(e.Path); err != nil {
			return fmt.Errorf("正则无效：%v", err)
		}
	default:
		return fmt.Errorf("路径匹配方式只能是 prefix、exact 或 regex")
	}
	if len(e.Path) == 0 || len(e.Path) > 512 {
		return fmt.Errorf("路径长度需在 1-512 之间")
	}
	if len(e.RuleIDs) == 0 && len(e.Tags) == 0 {
		return fmt.Errorf("至少指定一条规则 ID 或一个标签")
	}
	for _, id := range e.RuleIDs {
		if id <= 0 || id >= exclusionRuleIDBase {
			return fmt.Errorf("规则 ID %d 无效", id)
		}
	}
	for _, tag := range e.Tags {
		if !exclusionTagPattern.MatchString(tag) {
			return fmt.Errorf("标签 %q 无效", tag)
		}
	}
	for _, target := range e.Targets {
		if !exclusionTargetPattern.MatchString(target) {
			return fmt.Errorf("检查目标 %q 无效（例：ARGS:json.content、REQUEST_HEADERS:User-Agent、ARGS:/^json\\.items\\./）", target)
		}
	}
	for _, method := range e.Methods {
		if !methodPattern.MatchString(method) {
			return fmt.Errorf("请求方法 %q 无效", method)
		}
	}
	return nil
}

// directiveOptions 生成指令所需的全部输入（由 Options 规范化得到）。
type directiveOptions struct {
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
	DisabledGroups     map[string]bool
	RemovedRules       []string
	InspectAdminBody   bool
	BodyExemptPrefixes []string
	SensitiveArgs      []string
	RichTextArgs       []string
	ExtraContentTypes  []string
	Exclusions         []Exclusion
	TempDir            string
}

// crsRequestFiles / crsResponseFiles CRS 规则文件的包含顺序（与上游 crs-setup 的 Include 顺序一致）。
var crsRequestFiles = []string{
	"REQUEST-905-COMMON-EXCEPTIONS.conf",
	"REQUEST-911-METHOD-ENFORCEMENT.conf",
	"REQUEST-913-SCANNER-DETECTION.conf",
	"REQUEST-920-PROTOCOL-ENFORCEMENT.conf",
	"REQUEST-921-PROTOCOL-ATTACK.conf",
	"REQUEST-922-MULTIPART-ATTACK.conf",
	"REQUEST-930-APPLICATION-ATTACK-LFI.conf",
	"REQUEST-931-APPLICATION-ATTACK-RFI.conf",
	"REQUEST-932-APPLICATION-ATTACK-RCE.conf",
	"REQUEST-933-APPLICATION-ATTACK-PHP.conf",
	"REQUEST-934-APPLICATION-ATTACK-GENERIC.conf",
	"REQUEST-941-APPLICATION-ATTACK-XSS.conf",
	"REQUEST-942-APPLICATION-ATTACK-SQLI.conf",
	"REQUEST-943-APPLICATION-ATTACK-SESSION-FIXATION.conf",
	"REQUEST-944-APPLICATION-ATTACK-JAVA.conf",
	"REQUEST-949-BLOCKING-EVALUATION.conf",
}

var crsResponseFiles = []string{
	"RESPONSE-950-DATA-LEAKAGES.conf",
	"RESPONSE-951-DATA-LEAKAGES-SQL.conf",
	"RESPONSE-952-DATA-LEAKAGES-JAVA.conf",
	"RESPONSE-953-DATA-LEAKAGES-PHP.conf",
	"RESPONSE-954-DATA-LEAKAGES-IIS.conf",
	"RESPONSE-955-WEB-SHELLS.conf",
	"RESPONSE-956-DATA-LEAKAGES-RUBY.conf",
	"RESPONSE-959-BLOCKING-EVALUATION.conf",
	"RESPONSE-980-CORRELATION.conf",
}

// mandatoryGroups 不能整组停用的文件：初始化与阻断评估一旦缺失，整套计分就失效。
var mandatoryGroups = map[string]bool{"901": true, "905": true, "949": true, "959": true, "980": true, "999": true}

// groupOf 从 CRS 文件名取三位编号：REQUEST-942-APPLICATION-ATTACK-SQLI.conf → 942。
func groupOf(file string) string {
	base := path.Base(strings.ReplaceAll(file, "\\", "/"))
	parts := strings.SplitN(base, "-", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// attackTagsForSensitive 敏感字段要免掉的攻击类标签（口令、密钥：从不进 SQL / shell / 页面）。
var attackTagsForSensitive = []string{
	"attack-sqli", "attack-xss", "attack-rce", "attack-lfi", "attack-rfi",
	"attack-injection-php", "attack-injection-generic", "attack-injection-java", "attack-generic", "attack-ssti",
}

// attackTagsForRichText 富文本字段要免掉的标签：保留 LFI / RFI / SSRF / Java（Log4Shell）这类与「写了什么文字」无关的检测。
var attackTagsForRichText = []string{"attack-sqli", "attack-xss", "attack-rce", "attack-injection-php", "attack-injection-generic"}

// argNameRegex 把字段名清单变成匹配 ARGS 键的正则：既认表单字段 password，也认 JSON 的 json.password、json.user.password。
func argNameRegex(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		quoted = append(quoted, regexp.QuoteMeta(name))
	}
	if len(quoted) == 0 {
		return ""
	}
	sort.Strings(quoted)
	return `/(?i)^(?:json\.(?:[^/]*\.)?)?(?:` + strings.Join(quoted, "|") + `)$/`
}

func buildDirectives(opts directiveOptions) string {
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}

	engine := "On"
	if opts.DetectionOnly {
		engine = "DetectionOnly"
	}
	responseAccess := "Off"
	if opts.ResponseInspection {
		responseAccess = "On"
	}

	// ── 引擎基线：对齐 coraza 3.8.1 的 coraza.conf-recommended（CRS 包里那份是旧版） ──
	line("# Aegis firewall ruleset v2 — generated by internal/firewall, see docs/firewall.md")
	line("SecRuleEngine %s", engine)
	line("SecRequestBodyAccess On")
	line(`SecRule REQUEST_HEADERS:Content-Type "@rx ^(?:application(?:/soap\+|/)|text/)xml" "id:200000,phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=XML"`)
	line(`SecRule REQUEST_HEADERS:Content-Type "@rx ^application/json" "id:200001,phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON"`)
	line(`SecRule REQUEST_HEADERS:Content-Type "@rx ^application/[a-z0-9.-]+[+]json" "id:200006,phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON"`)
	// 二进制上传（安装包分片、图片、压缩包）不按表单解析：默认的 urlencoded 处理器会把字节流拆成一堆带 NUL 的「参数名」，
	// 920270 当场误报。RAW 处理器只留 REQUEST_BODY。声明成二进制、内容却是 JSON 的请求由中间件改判为 JSON（EffectiveContentType）。
	line(`SecRule REQUEST_HEADERS:Content-Type "@rx ^(?:application/(?:octet-stream|gzip|zip|x-gzip|x-protobuf|pdf|vnd\.android\.package-archive)|image/|video/|audio/|font/)" "id:200007,phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=RAW"`)
	line(`SecRule URI_PARSE_ERROR "@eq 1" "id:200009,phase:1,t:none,log,deny,status:400,msg:'URI failed to parse.',tag:'aegis/baseline'"`)
	line("SecRequestBodyJsonDepthLimit 512")
	line("SecArgumentsLimit %d", opts.ArgumentsLimit)
	line("SecRequestBodyLimit %d", opts.RequestBodyLimit)
	line("SecRequestBodyInMemoryLimit %d", opts.RequestBodyMemory)
	line("SecRequestBodyLimitAction Reject")
	line(`SecRule REQBODY_ERROR "!@eq 0" "id:200002,phase:2,t:none,log,deny,status:400,msg:'Failed to parse request body.',logdata:'%%{reqbody_error_msg}',severity:2,tag:'aegis/baseline'"`)
	line(`SecRule MULTIPART_STRICT_ERROR "!@eq 0" "id:200003,phase:2,t:none,log,deny,status:400,msg:'Multipart request body failed strict validation.',tag:'aegis/baseline'"`)
	line(`SecRule ARGUMENTS_LIMIT_REACHED "@eq 1" "id:200004,phase:1,t:none,log,deny,status:400,msg:'Argument limit reached (GET/PATH args).',tag:'aegis/baseline'"`)
	line(`SecRule ARGUMENTS_LIMIT_REACHED "@eq 1" "id:200005,phase:2,t:none,log,deny,status:400,msg:'Argument limit reached (POST args).',tag:'aegis/baseline'"`)
	line("SecResponseBodyAccess %s", responseAccess)
	line("SecResponseBodyMimeType application/json text/plain text/html text/xml application/xml")
	line("SecResponseBodyLimit %d", opts.ResponseBodyLimit)
	line("SecResponseBodyLimitAction ProcessPartial")
	line(`SecDataDir "%s"`, opts.TempDir)
	// 审计由 Aegis 自己落库（firewall_logs），不让 Coraza 再写一份串行审计文件。
	line("SecAuditEngine Off")

	// ── CRS 设置 ──
	line("Include @crs-setup.conf.example")
	contentTypes := "|application/x-www-form-urlencoded| |multipart/form-data| |multipart/related| |text/xml| |application/xml| |application/soap+xml| |application/json| |application/cloudevents+json| |application/cloudevents-batch+json|"
	for _, ct := range opts.ExtraContentTypes {
		ct = strings.ToLower(strings.TrimSpace(ct))
		if ct == "" || strings.ContainsAny(ct, " |'\"\\") {
			continue
		}
		contentTypes += " |" + ct + "|"
	}
	skipResponse := 1
	if opts.ResponseInspection {
		skipResponse = 0
	}
	line(`SecAction "id:1100001,phase:1,pass,nolog,t:none,`+
		`setvar:tx.blocking_paranoia_level=%d,`+
		`setvar:tx.detection_paranoia_level=%d,`+
		`setvar:tx.inbound_anomaly_score_threshold=%d,`+
		`setvar:tx.outbound_anomaly_score_threshold=%d,`+
		`setvar:'tx.allowed_methods=%s',`+
		`setvar:'tx.allowed_request_content_type=%s',`+
		`setvar:tx.crs_validate_utf8_encoding=1,`+
		`setvar:tx.crs_skip_response_analysis=%d"`,
		opts.Paranoia, opts.DetectionParanoia, opts.InboundThreshold, opts.OutboundThreshold,
		AllowedMethods, contentTypes, skipResponse)

	// ── Aegis 调优档（phase 1 的 ctl，必须排在被调的规则之前） ──
	// 防火墙自己的管理接口：测试台的请求体就是攻击样本，规则与例外里满是 SQL、正则与脚本片段。
	// 无论是否开启管理端请求体检查都不检查请求体。
	line(`SecRule REQUEST_FILENAME "@beginsWith /api/admin/system/firewall/" "id:1100099,phase:1,pass,nolog,t:none,ctl:requestBodyAccess=Off,tag:'aegis/profile-firewall-admin'"`)
	if !opts.InspectAdminBody {
		// 管理端已鉴权，请求体里满是规则表达式、远程函数代码、邮件模板与出站 YAML：
		// 不解析请求体（ctl:requestBodyAccess=Off，中间件据此不读请求体），URI 与请求头照常检查。
		line(`SecRule REQUEST_FILENAME "@beginsWith /api/admin/" "id:1100100,phase:1,pass,nolog,t:none,ctl:requestBodyAccess=Off,tag:'aegis/profile-admin'"`)
	}
	for i, prefix := range opts.BodyExemptPrefixes {
		if i >= 80 {
			break
		}
		prefix = strings.TrimSpace(prefix)
		if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "\"'\\ \r\n") {
			continue
		}
		line(`SecRule REQUEST_FILENAME "@beginsWith %s" "id:%d,phase:1,pass,nolog,t:none,ctl:requestBodyAccess=Off,tag:'aegis/profile-body-exempt'"`, prefix, 1100110+i)
	}
	// 存储对象路径就是用户的文件名（backup.db、crash.log），920440 的「受限扩展名」在这里没有意义。
	line(`SecRule REQUEST_FILENAME "@rx ^/api/(?:v1/apps/[^/]+/)?storage/" "id:1100200,phase:1,pass,nolog,t:none,ctl:ruleRemoveById=920440,tag:'aegis/profile-storage'"`)
	if rx := argNameRegex(opts.SensitiveArgs); rx != "" {
		var actions []string
		for _, tag := range attackTagsForSensitive {
			actions = append(actions, "ctl:ruleRemoveTargetByTag="+tag+";ARGS:"+rx)
		}
		line(`SecAction "id:1100300,phase:1,pass,nolog,t:none,%s,tag:'aegis/profile-sensitive-args'"`, strings.Join(actions, ","))
	}
	if rx := argNameRegex(opts.RichTextArgs); rx != "" {
		var actions []string
		for _, tag := range attackTagsForRichText {
			actions = append(actions, "ctl:ruleRemoveTargetByTag="+tag+";ARGS:"+rx)
		}
		line(`SecAction "id:1100310,phase:1,pass,nolog,t:none,%s,tag:'aegis/profile-rich-text'"`, strings.Join(actions, ","))
	}

	// ── 管理员维护的 WAF 例外 ──
	for i, e := range opts.Exclusions {
		id := exclusionRuleIDBase + i
		if id > exclusionRuleIDMax {
			break
		}
		if directive := exclusionDirective(id, e); directive != "" {
			line("%s", directive)
		}
	}

	// ── CRS 规则 ──
	line("Include @owasp_crs/REQUEST-901-INITIALIZATION.conf")
	line("Include @aegis/aegis-protection.conf")
	for _, file := range crsRequestFiles {
		if opts.DisabledGroups[groupOf(file)] && !mandatoryGroups[groupOf(file)] {
			continue
		}
		line("Include @owasp_crs/%s", file)
	}
	if opts.ResponseInspection {
		for _, file := range crsResponseFiles {
			if opts.DisabledGroups[groupOf(file)] && !mandatoryGroups[groupOf(file)] {
				continue
			}
			line("Include @owasp_crs/%s", file)
		}
	}
	line("Include @owasp_crs/REQUEST-999-COMMON-EXCEPTIONS-AFTER.conf")

	// ── 全局移除（配置期指令，必须在规则加载之后） ──
	for _, id := range opts.RemovedRules {
		if _, err := strconv.Atoi(id); err == nil {
			line("SecRuleRemoveById %s", id)
		}
	}
	return b.String()
}

// exclusionDirective 把一条例外拼成 phase 1 的 ctl 规则。路径匹配写在规则本身，方法用 chain。
func exclusionDirective(id int, e Exclusion) string {
	if ValidateExclusion(e) != nil {
		return ""
	}
	operator := "@beginsWith"
	switch e.PathMatch {
	case "exact":
		operator = "@streq"
	case "regex":
		operator = "@rx"
	}
	var ctls []string
	if len(e.Targets) == 0 {
		for _, ruleID := range e.RuleIDs {
			ctls = append(ctls, "ctl:ruleRemoveById="+strconv.Itoa(ruleID))
		}
		for _, tag := range e.Tags {
			ctls = append(ctls, "ctl:ruleRemoveByTag="+tag)
		}
	} else {
		for _, target := range e.Targets {
			for _, ruleID := range e.RuleIDs {
				ctls = append(ctls, "ctl:ruleRemoveTargetById="+strconv.Itoa(ruleID)+";"+target)
			}
			for _, tag := range e.Tags {
				ctls = append(ctls, "ctl:ruleRemoveTargetByTag="+tag+";"+target)
			}
		}
	}
	if len(ctls) == 0 {
		return ""
	}
	path := strings.ReplaceAll(e.Path, `\`, `\\`)
	if len(e.Methods) == 0 {
		return fmt.Sprintf(`SecRule REQUEST_FILENAME "%s %s" "id:%d,phase:1,pass,nolog,t:none,%s,tag:'aegis/exclusion'"`, operator, path, id, strings.Join(ctls, ","))
	}
	return fmt.Sprintf(`SecRule REQUEST_FILENAME "%s %s" "id:%d,phase:1,pass,nolog,t:none,chain"`+"\n"+
		`    SecRule REQUEST_METHOD "@within %s" "t:none,%s"`,
		operator, path, id, strings.Join(e.Methods, " "), strings.Join(ctls, ","))
}

// mergedFS 把 CRS 文件系统与 Aegis 规则合在一起：@aegis/<file> 指向内嵌规则，其余交给 CRS。
type mergedFS struct {
	crs fs.FS
}

func (m mergedFS) Open(name string) (fs.File, error) {
	name = normalizeFSPath(name)
	if rest, ok := strings.CutPrefix(name, "@aegis/"); ok {
		return rulesFS.Open("rules/" + rest)
	}
	return m.crs.Open(name)
}

func (m mergedFS) ReadFile(name string) ([]byte, error) {
	name = normalizeFSPath(name)
	if idx := strings.Index(name, "@aegis/"); idx >= 0 {
		return rulesFS.ReadFile("rules/" + name[idx+len("@aegis/"):])
	}
	return fs.ReadFile(m.crs, name)
}

func (m mergedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(m.crs, normalizeFSPath(name))
}

func (m mergedFS) Glob(pattern string) ([]string, error) {
	return fs.Glob(m.crs, normalizeFSPath(pattern))
}

func normalizeFSPath(name string) string {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if name == "" {
		return "."
	}
	cleaned := strings.TrimPrefix(strings.TrimPrefix(path.Clean(name), "./"), "/")
	if cleaned == "" {
		return "."
	}
	return cleaned
}
