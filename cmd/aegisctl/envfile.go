package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// envfile.go：cluster.env 的解析、合并、打码与比较。
//
// 格式与 docker 的 env_file 一致（docker stack deploy 读的正是这个文件）：
// 行首空白去掉；# 开头是注释；KEY=VALUE 按第一个等号切开，值原样保留，不处理引号。
// 解析时保留全部行，写回时注释与顺序不变。

// EnvLine 文件里的一行
type EnvLine struct {
	Raw   string
	Key   string
	Value string
	IsKV  bool
	// Directives 紧挨在键上方的 #@ 标注（secret 32 / hex 32 / copy KEY / required）
	Directives []string
}

// EnvFile 保留行序与注释的 env 文件
type EnvFile struct {
	Lines []EnvLine
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// ParseEnv 解析 env 文件；不合法的行（没有等号、键名非法）保留为原文，Problems 里列出
func ParseEnv(data string) (*EnvFile, []string) {
	f := &EnvFile{}
	var problems []string
	var pending []string
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.TrimSuffix(data, "\n")
	if data == "" {
		return f, nil
	}
	for i, raw := range strings.Split(data, "\n") {
		line := strings.TrimLeftFunc(raw, unicode.IsSpace)
		switch {
		case line == "":
			f.Lines = append(f.Lines, EnvLine{Raw: raw})
			pending = nil
		case strings.HasPrefix(line, "#@"):
			pending = append(pending, strings.TrimSpace(strings.TrimPrefix(line, "#@")))
			f.Lines = append(f.Lines, EnvLine{Raw: raw})
		case strings.HasPrefix(line, "#"):
			f.Lines = append(f.Lines, EnvLine{Raw: raw})
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok || !envKeyPattern.MatchString(key) {
				problems = append(problems, fmt.Sprintf("第 %d 行不是合法的 KEY=VALUE", i+1))
				f.Lines = append(f.Lines, EnvLine{Raw: raw})
				pending = nil
				continue
			}
			f.Lines = append(f.Lines, EnvLine{Raw: raw, Key: key, Value: value, IsKV: true, Directives: pending})
			pending = nil
		}
	}
	return f, problems
}

// Get 取值；重复的键以最后一个为准（与 docker 一致）
func (f *EnvFile) Get(key string) (string, bool) {
	for i := len(f.Lines) - 1; i >= 0; i-- {
		if f.Lines[i].IsKV && f.Lines[i].Key == key {
			return f.Lines[i].Value, true
		}
	}
	return "", false
}

// Value 取值，不存在时为空串
func (f *EnvFile) Value(key string) string {
	v, _ := f.Get(key)
	return v
}

// Set 改写已有键（所有同名行），不存在则追加到末尾
func (f *EnvFile) Set(key, value string) {
	found := false
	for i := range f.Lines {
		if f.Lines[i].IsKV && f.Lines[i].Key == key {
			f.Lines[i].Value = value
			f.Lines[i].Raw = key + "=" + value
			found = true
		}
	}
	if !found {
		f.Lines = append(f.Lines, EnvLine{Raw: key + "=" + value, Key: key, Value: value, IsKV: true})
	}
}

// Unset 删除键（连同它上方的 #@ 标注行之外的注释保持不动），返回是否删过
func (f *EnvFile) Unset(key string) bool {
	out := f.Lines[:0]
	removed := false
	for _, l := range f.Lines {
		if l.IsKV && l.Key == key {
			removed = true
			continue
		}
		out = append(out, l)
	}
	f.Lines = out
	return removed
}

// Keys 按出现顺序列出键（去重）
func (f *EnvFile) Keys() []string {
	seen := map[string]bool{}
	var keys []string
	for _, l := range f.Lines {
		if l.IsKV && !seen[l.Key] {
			seen[l.Key] = true
			keys = append(keys, l.Key)
		}
	}
	return keys
}

// Map 键值表
func (f *EnvFile) Map() map[string]string {
	m := map[string]string{}
	for _, l := range f.Lines {
		if l.IsKV {
			m[l.Key] = l.Value
		}
	}
	return m
}

// DirectivesOf 某个键的标注
func (f *EnvFile) DirectivesOf(key string) []string {
	for _, l := range f.Lines {
		if l.IsKV && l.Key == key {
			return l.Directives
		}
	}
	return nil
}

// RequiredKeys 标了 #@ required 或会自动生成（secret / hex / copy）的键：这些键不能为空
func (f *EnvFile) RequiredKeys() []string {
	var keys []string
	for _, l := range f.Lines {
		if !l.IsKV {
			continue
		}
		for _, d := range l.Directives {
			name := strings.Fields(d + " ")[0]
			if name == "required" || name == "secret" || name == "hex" || name == "copy" {
				keys = append(keys, l.Key)
				break
			}
		}
	}
	return keys
}

// String 写回文本（末尾带换行）
func (f *EnvFile) String() string {
	var b strings.Builder
	for _, l := range f.Lines {
		b.WriteString(l.Raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// Clone 深拷贝
func (f *EnvFile) Clone() *EnvFile {
	c := &EnvFile{Lines: make([]EnvLine, len(f.Lines))}
	for i, l := range f.Lines {
		l.Directives = append([]string(nil), l.Directives...)
		c.Lines[i] = l
	}
	return c
}

// ─────────────────────────── 生成随机值 ───────────────────────────

const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// RandomAlnum 只含字母数字的随机串（要拼进 DSN、URL 与 NATS 配置，不能有特殊字符）
func RandomAlnum(r io.Reader, n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	max := big.NewInt(int64(len(alnum)))
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(r, max)
		if err != nil {
			return "", err
		}
		b[i] = alnum[v.Int64()]
	}
	return string(b), nil
}

// RandomHex n 字节的十六进制串
func RandomHex(r io.Reader, n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ─────────────────────────── 合并：init 生成 / 补齐 cluster.env ───────────────────────────

// MergeReport 合并结果（只记键名，不记值）
type MergeReport struct {
	Added     []string // 线上文件原来没有、按模板补上的键
	Generated []string // 自动生成随机值的键
	Imported  []string // 取自现网的键
	Copied    []string // 按 #@ copy 取自其他键
	Extra     []string // 模板里没有、从现网导入后追加在末尾的键
}

// MergeClusterEnv 用模板生成或补齐线上的 cluster.env。
//
// 取值优先级（只针对每个键各自判断）：
//
//	线上文件里已有且非空 → 不动；
//	线上文件里有但为空   → 现网导入值；仍为空且标了 secret / hex / copy 就生成；
//	线上文件里没有       → 现网导入值 → 模板默认值 → 生成。
//
// existing 为 nil 表示线上还没有这个文件：以模板为底稿（注释与分节都保留）。
// imported 里模板没有的键，按 extraOrder 的顺序追加在文件末尾。
func MergeClusterEnv(tmpl, existing *EnvFile, imported map[string]string, extraOrder []string, rnd io.Reader, note string) (*EnvFile, MergeReport, error) {
	var rep MergeReport
	var out *EnvFile
	existingKeys := map[string]bool{}
	if existing != nil {
		out = existing.Clone()
		for _, k := range existing.Keys() {
			existingKeys[k] = true
		}
	} else {
		out = tmpl.Clone()
	}

	// 线上缺的模板键：带着标注追加
	if existing != nil {
		var missing []EnvLine
		for _, l := range tmpl.Lines {
			if l.IsKV && !existingKeys[l.Key] {
				missing = append(missing, l)
			}
		}
		if len(missing) > 0 {
			out.Lines = append(out.Lines, EnvLine{Raw: ""}, EnvLine{Raw: "# ── aegisctl init 按模板补上的键" + note + " ──"})
			for _, l := range missing {
				for _, d := range l.Directives {
					out.Lines = append(out.Lines, EnvLine{Raw: "#@ " + d})
				}
				nl := l
				nl.Directives = append([]string(nil), l.Directives...)
				out.Lines = append(out.Lines, nl)
				rep.Added = append(rep.Added, l.Key)
			}
		}
	}

	directives := map[string][]string{}
	for _, l := range tmpl.Lines {
		if l.IsKV {
			directives[l.Key] = l.Directives
		}
	}
	for _, l := range out.Lines {
		if l.IsKV && len(l.Directives) > 0 {
			directives[l.Key] = l.Directives
		}
	}

	var copies []string
	for _, key := range out.Keys() {
		cur := out.Value(key)
		wasExisting := existingKeys[key]
		if wasExisting && cur != "" {
			continue
		}
		if v := imported[key]; v != "" {
			if v != cur {
				out.Set(key, v)
				rep.Imported = append(rep.Imported, key)
			}
			continue
		}
		if cur != "" {
			continue // 模板默认值
		}
		for _, d := range directives[key] {
			fields := strings.Fields(d)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "secret", "hex":
				n := 0
				if len(fields) > 1 {
					n, _ = strconv.Atoi(fields[1])
				}
				var v string
				var err error
				if fields[0] == "secret" {
					v, err = RandomAlnum(rnd, n)
				} else {
					v, err = RandomHex(rnd, n)
				}
				if err != nil {
					return nil, rep, err
				}
				out.Set(key, v)
				rep.Generated = append(rep.Generated, key)
			case "copy":
				if len(fields) > 1 {
					copies = append(copies, key+"\x00"+fields[1])
				}
			}
		}
	}
	for _, c := range copies {
		key, from, _ := strings.Cut(c, "\x00")
		if v := out.Value(from); v != "" && out.Value(key) == "" {
			out.Set(key, v)
			rep.Copied = append(rep.Copied, key)
		}
	}

	// 现网多出来的键
	have := map[string]bool{}
	for _, k := range out.Keys() {
		have[k] = true
	}
	var extra []string
	for _, k := range extraOrder {
		if !have[k] && imported[k] != "" {
			extra = append(extra, k)
			have[k] = true
		}
	}
	if len(extra) > 0 {
		out.Lines = append(out.Lines, EnvLine{Raw: ""}, EnvLine{Raw: "# ── 从现网 Dokploy 导入的其他变量" + note + " ──"})
		for _, k := range extra {
			out.Set(k, imported[k])
			rep.Extra = append(rep.Extra, k)
		}
	}
	return out, rep, nil
}

// ─────────────────────────── 打码与比较 ───────────────────────────

var sensitiveName = regexp.MustCompile(`(?i)(PASS|PWD|SECRET|TOKEN|_KEY|KEYS$|^KEY|SALT|DSN|CREDENTIAL|PRIVATE|AUTH|COOKIE)`)
var urlCredential = regexp.MustCompile(`://[^/@\s]*:[^/@\s]*@`)

// IsSensitive 键名或值看起来像密钥
func IsSensitive(key, value string) bool {
	return sensitiveName.MatchString(key) || urlCredential.MatchString(value)
}

// Fingerprint 值的 sha256 前 8 位
func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

// MaskValue 打码后的展示：空值、长度与指纹，不含任何明文字符
func MaskValue(value string) string {
	if value == "" {
		return "（空）"
	}
	return fmt.Sprintf("****（%d 字符，sha256:%s）", len([]rune(value)), Fingerprint(value))
}

// DisplayValue get 的展示规则：敏感值打码，reveal 时原样
func DisplayValue(key, value string, reveal bool) string {
	if reveal || !IsSensitive(key, value) {
		return value
	}
	return MaskValue(value)
}

// EnvChange 一个键的差异
type EnvChange struct {
	Key  string
	Kind string // added / removed / changed
	From string // 指纹或空
	To   string
}

// DiffEnv 比较两份键值表（from → to），只给指纹
func DiffEnv(from, to map[string]string) []EnvChange {
	keys := map[string]bool{}
	for k := range from {
		keys[k] = true
	}
	for k := range to {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out []EnvChange
	for _, k := range sorted {
		a, inA := from[k]
		b, inB := to[k]
		switch {
		case inA && !inB:
			out = append(out, EnvChange{Key: k, Kind: "removed", From: Fingerprint(a)})
		case !inA && inB:
			out = append(out, EnvChange{Key: k, Kind: "added", To: Fingerprint(b)})
		case a != b:
			out = append(out, EnvChange{Key: k, Kind: "changed", From: Fingerprint(a), To: Fingerprint(b)})
		}
	}
	return out
}

// QuotedValueKeys 值两端带引号的键：docker 的 env_file 不去引号，多半是写错了
func QuotedValueKeys(f *EnvFile) []string {
	var keys []string
	for _, l := range f.Lines {
		v := l.Value
		if l.IsKV && len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			keys = append(keys, l.Key)
		}
	}
	return keys
}
