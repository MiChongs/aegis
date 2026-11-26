package firewall

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
)

// RuleGroup CRS / Aegis 的一个规则文件（控制台按组启停、按组统计命中）。
type RuleGroup struct {
	Code        string `json:"code"`
	File        string `json:"file"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Kind: request / response / aegis / core
	Kind      string `json:"kind"`
	RuleCount int    `json:"ruleCount"`
	Enabled   bool   `json:"enabled"`
	// Locked 不能停用（初始化、阻断评估、关联统计）。
	Locked bool `json:"locked"`
}

// RuleInfo 规则目录里的一条规则。
type RuleInfo struct {
	ID       int      `json:"id"`
	Group    string   `json:"group"`
	Message  string   `json:"message"`
	Severity string   `json:"severity"`
	Paranoia int      `json:"paranoia"`
	Phase    int      `json:"phase"`
	Tags     []string `json:"tags"`
	// Removed 被全局移除（SecRuleRemoveById）。
	Removed bool `json:"removed"`
	// Active 当前配置下实际参与检测：所在组启用、未被移除、偏执等级不高于检测等级。
	Active bool `json:"active"`
}

// Catalog 当前配置下的规则目录。
type Catalog struct {
	Version           string      `json:"version"`
	Groups            []RuleGroup `json:"groups"`
	Rules             []RuleInfo  `json:"rules"`
	TotalRules        int         `json:"totalRules"`
	ActiveRules       int         `json:"activeRules"`
	Paranoia          int         `json:"paranoia"`
	DetectionParanoia int         `json:"detectionParanoia"`
}

// groupMeta 规则组的中文名与说明（控制台显示）。
var groupMeta = map[string][2]string{
	"aegis": {"Aegis 专属", "内部服务头、alg=none 令牌、网关 appKey 格式、国内云元数据 SSRF、二次封装载荷"},
	"901":   {"初始化", "CRS 计分变量与默认值，不能停用"},
	"905":   {"常见例外", "CRS 自带的少量已知误报例外"},
	"911":   {"方法限制", "只放行路由表里实际用到的 HTTP 方法"},
	"913":   {"扫描器识别", "常见漏洞扫描器的 User-Agent 特征"},
	"920":   {"协议规范", "请求行、头部、编码、长度与内容类型的合规检查"},
	"921":   {"协议攻击", "请求走私、响应拆分、头注入"},
	"922":   {"多段表单攻击", "multipart 头部与边界的畸形构造"},
	"930":   {"本地文件包含", "路径穿越与敏感文件读取"},
	"931":   {"远程文件包含", "参数里塞入外部 URL 让服务端加载"},
	"932":   {"远程命令执行", "Unix / Windows 命令注入与 Shell 元字符"},
	"933":   {"PHP 注入", "PHP 函数、变量与包装器"},
	"934":   {"通用攻击", "Node.js / 原型链污染、SSRF、模板注入等"},
	"941":   {"跨站脚本", "XSS：标签、事件属性、JavaScript 协议与 libinjection"},
	"942":   {"SQL 注入", "SQLi：关键字组合、注释、libinjection"},
	"943":   {"会话固定", "通过参数植入会话标识"},
	"944":   {"Java 攻击", "反序列化、表达式注入、Log4Shell"},
	"949":   {"入站阻断评估", "累计入站异常分，越线即拦，不能停用"},
	"950":   {"数据泄露", "响应里的目录列表、源码等"},
	"951":   {"SQL 错误泄露", "响应里的数据库报错"},
	"952":   {"Java 错误泄露", "响应里的 Java 异常栈"},
	"953":   {"PHP 错误泄露", "响应里的 PHP 报错与源码"},
	"954":   {"IIS 错误泄露", "响应里的 IIS / ASP.NET 报错"},
	"955":   {"WebShell 特征", "响应里出现常见 WebShell 页面"},
	"956":   {"Ruby 错误泄露", "响应里的 Ruby 异常"},
	"959":   {"出站阻断评估", "累计出站异常分，不能停用"},
	"980":   {"关联统计", "入站与出站命中的汇总，不能停用"},
	"999":   {"后置例外", "CRS 插件挂点"},
}

type parsedRule struct {
	RuleInfo
	file string
}

var (
	staticRulesOnce sync.Once
	staticRules     []parsedRule
	staticCounts    map[string]int
	staticFiles     map[string]string

	idPattern       = regexp.MustCompile(`\bid:'?(\d+)'?`)
	msgPattern      = regexp.MustCompile(`\bmsg:'((?:[^'\\]|\\.)*)'`)
	severityPattern = regexp.MustCompile(`\bseverity:'?([A-Za-z0-9]+)'?`)
	tagPattern      = regexp.MustCompile(`\btag:'([^']*)'`)
	phasePattern    = regexp.MustCompile(`\bphase:'?(\d|request|response|logging)'?`)
)

// loadStaticRules 解析内嵌的 CRS 与 Aegis 规则文件（只做一次）。目录与是否编译无关，
// 停用的组也要列出来，控制台才能再把它打开。
func loadStaticRules() {
	staticRulesOnce.Do(func() {
		staticCounts = map[string]int{}
		staticFiles = map[string]string{}
		files, _ := fs.Glob(coreruleset.FS, "@owasp_crs/*.conf")
		for _, file := range files {
			data, err := fs.ReadFile(coreruleset.FS, file)
			if err != nil {
				continue
			}
			parseRuleFile(file, string(data))
		}
		if data, err := rulesFS.ReadFile("rules/aegis-protection.conf"); err == nil {
			parseRuleFile("@aegis/aegis-protection.conf", string(data))
		}
		sort.Slice(staticRules, func(i, j int) bool { return staticRules[i].ID < staticRules[j].ID })
	})
}

func parseRuleFile(file, text string) {
	group := groupOf(file)
	if strings.Contains(file, "@aegis/") {
		group = "aegis"
	}
	if group == "" {
		return
	}
	staticFiles[group] = file
	// 续行（行尾反斜杠）拼回一条完整指令；chain 子规则没有 id，自然被跳过。
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var statements []string
	var current strings.Builder
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if current.Len() == 0 && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		if strings.HasSuffix(trimmed, "\\") {
			current.WriteString(strings.TrimSuffix(trimmed, "\\"))
			current.WriteByte(' ')
			continue
		}
		current.WriteString(trimmed)
		statements = append(statements, current.String())
		current.Reset()
	}
	for _, stmt := range statements {
		if !strings.HasPrefix(stmt, "SecRule") && !strings.HasPrefix(stmt, "SecAction") {
			continue
		}
		idMatch := idPattern.FindStringSubmatch(stmt)
		if idMatch == nil {
			continue
		}
		id, _ := strconv.Atoi(idMatch[1])
		msg := ""
		if m := msgPattern.FindStringSubmatch(stmt); m != nil {
			msg = m[1]
		}
		// 只收真正做检测的规则：没有 msg 的是 SecAction 初始化、SecMarker 跳转与计数辅助。
		if msg == "" {
			continue
		}
		info := RuleInfo{ID: id, Group: group, Message: msg, Paranoia: 1}
		if m := severityPattern.FindStringSubmatch(stmt); m != nil {
			info.Severity = normalizeSeverityText(m[1])
		}
		if m := phasePattern.FindStringSubmatch(stmt); m != nil {
			switch m[1] {
			case "request":
				info.Phase = 2
			case "response":
				info.Phase = 4
			case "logging":
				info.Phase = 5
			default:
				info.Phase, _ = strconv.Atoi(m[1])
			}
		}
		for _, tag := range tagPattern.FindAllStringSubmatch(stmt, -1) {
			value := tag[1]
			if level, ok := strings.CutPrefix(value, "paranoia-level/"); ok {
				info.Paranoia, _ = strconv.Atoi(level)
			}
			if strings.HasPrefix(value, "attack-") || strings.HasPrefix(value, "aegis/") || strings.HasPrefix(value, "language-") || strings.HasPrefix(value, "platform-") {
				info.Tags = append(info.Tags, value)
			}
		}
		staticRules = append(staticRules, parsedRule{RuleInfo: info, file: file})
		staticCounts[group]++
	}
}

func normalizeSeverityText(s string) string {
	switch strings.ToUpper(s) {
	case "0", "1", "2", "EMERGENCY", "ALERT", "CRITICAL":
		return "critical"
	case "3", "ERROR":
		return "error"
	case "4", "WARNING":
		return "warning"
	case "5", "NOTICE":
		return "notice"
	default:
		return strings.ToLower(s)
	}
}

// ruleSetVersion CRS 版本号（取自 crs-setup 里的 ver:'OWASP_CRS/x.y.z'）。
var ruleSetVersion = sync.OnceValue(func() string {
	data, err := fs.ReadFile(coreruleset.FS, "@crs-setup.conf.example")
	if err != nil {
		return ""
	}
	if m := regexp.MustCompile(`OWASP_CRS/([0-9.]+)`).FindStringSubmatch(string(data)); m != nil {
		return m[1]
	}
	return ""
})

// buildCatalog 按当前配置给静态目录打上启用 / 移除 / 生效标记。
func buildCatalog(opts directiveOptions) *Catalog {
	loadStaticRules()
	removed := make(map[int]bool, len(opts.RemovedRules))
	for _, id := range opts.RemovedRules {
		if n, err := strconv.Atoi(strings.TrimSpace(id)); err == nil {
			removed[n] = true
		}
	}
	responseGroups := map[string]bool{"950": true, "951": true, "952": true, "953": true, "954": true, "955": true, "956": true, "959": true, "980": true}
	groupEnabled := func(code string) bool {
		if responseGroups[code] && !opts.ResponseInspection {
			return false
		}
		if mandatoryGroups[code] {
			return true
		}
		return !opts.DisabledGroups[code]
	}
	cat := &Catalog{
		Version:           ruleSetVersion(),
		Paranoia:          opts.Paranoia,
		DetectionParanoia: opts.DetectionParanoia,
	}
	codes := make([]string, 0, len(staticFiles))
	for code := range staticFiles {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool {
		if codes[i] == "aegis" {
			return true
		}
		if codes[j] == "aegis" {
			return false
		}
		return codes[i] < codes[j]
	})
	for _, code := range codes {
		meta := groupMeta[code]
		kind := "request"
		switch {
		case code == "aegis":
			kind = "aegis"
		case responseGroups[code]:
			kind = "response"
		case mandatoryGroups[code]:
			kind = "core"
		}
		cat.Groups = append(cat.Groups, RuleGroup{
			Code:        code,
			File:        strings.TrimPrefix(strings.TrimPrefix(staticFiles[code], "@owasp_crs/"), "@aegis/"),
			Name:        meta[0],
			Description: meta[1],
			Kind:        kind,
			RuleCount:   staticCounts[code],
			Enabled:     groupEnabled(code),
			Locked:      mandatoryGroups[code] || code == "aegis",
		})
	}
	cat.Rules = make([]RuleInfo, 0, len(staticRules))
	for _, rule := range staticRules {
		info := rule.RuleInfo
		info.Tags = append([]string(nil), info.Tags...)
		info.Removed = removed[info.ID]
		info.Active = groupEnabled(info.Group) && !info.Removed && info.Paranoia <= opts.DetectionParanoia
		if info.Active {
			cat.ActiveRules++
		}
		cat.Rules = append(cat.Rules, info)
	}
	cat.TotalRules = len(cat.Rules)
	return cat
}

// RuleMessage 按规则 ID 查描述（事件详情页把旧日志里只有 ID 的记录补上说明）。
func RuleMessage(id int) (string, string, bool) {
	loadStaticRules()
	idx := sort.Search(len(staticRules), func(i int) bool { return staticRules[i].ID >= id })
	if idx < len(staticRules) && staticRules[idx].ID == id {
		return staticRules[idx].Message, staticRules[idx].Group, true
	}
	return "", "", false
}

// GroupName 规则组的中文名。
func GroupName(code string) string {
	if meta, ok := groupMeta[code]; ok {
		return meta[0]
	}
	return code
}
