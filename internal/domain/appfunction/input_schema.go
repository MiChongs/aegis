package appfunction

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 入参 JSON Schema → TypeScript 声明。
//
// 这段代码存在的唯一理由：让编辑器里的 `ctx.input.` 能补全出真实字段。
//
// 没有它时 `ctx.input` 的类型只能是 any，于是脚本第一行的 `ctx.input.orderNo`
// 与接入方实际传的 `ctx.input.order_no` 之间的差异，要等到线上第一次调用
// 才以「undefined」的形式暴露 —— 而那时版本已经不可变了。
//
// 转换是**有意保守**的：认不出来的构造一律降级成 `any`，绝不猜。
// 编辑器里多一个 any 只是少一点帮助，而猜错一个类型会让作者对着一条
// 根本不存在的编译错误改代码，那比没有类型糟得多。

const (
	// maxSchemaDepth 递归深度上限。JSON Schema 可以自引用（$ref 指回自己），
	// 而我们不解析 $ref —— 但嵌套对象同样能写到很深，没有上限就是一个栈溢出。
	maxSchemaDepth = 12
	// maxSchemaProperties 单层属性数上限，防止一份几千字段的 schema
	// 把生成出来的 .d.ts 撑到编辑器卡死。
	maxSchemaProperties = 200
	// InputTypeName 生成出来的接口名，AegisContext.input 指向它
	InputTypeName = "AegisInput"
)

// InputSchemaTemplate 控制台「入参契约」编辑器的起步骨架。
//
// 给一份能直接改的样例，而不是让作者对着空编辑器回忆 JSON Schema 的关键字。
// 里面每一项都对应生成侧支持的一种构造：必填、枚举、范围、嵌套对象、数组。
const InputSchemaTemplate = `{
  "type": "object",
  "required": ["orderNo"],
  "additionalProperties": false,
  "properties": {
    "orderNo": {
      "type": "string",
      "minLength": 1,
      "description": "业务订单号"
    },
    "channel": {
      "type": "string",
      "enum": ["web", "ios", "android"],
      "description": "下单渠道"
    },
    "quantity": {
      "type": "integer",
      "minimum": 1,
      "maximum": 99,
      "default": 1
    },
    "coupon": {
      "type": "object",
      "properties": {
        "code": { "type": "string" },
        "amount": { "type": "string", "description": "定点小数字符串" }
      }
    },
    "tags": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}`

// InputSchemaDeclaration 把入参 schema 渲染成一段 TypeScript 接口声明。
//
// schema 为空、不是对象、或者没有 properties 时返回空串 —— 调用方据此
// 回落到 `input: any`，与没有 schema 时的行为完全一致。
func InputSchemaDeclaration(raw json.RawMessage) string {
	schema := decodeSchema(raw)
	if schema == nil {
		return ""
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return ""
	}
	body := renderObjectBody(schema, 1, 0)
	if strings.TrimSpace(body) == "" {
		return ""
	}
	var builder strings.Builder
	if description, ok := schema["description"].(string); ok && description != "" {
		builder.WriteString("/** " + sanitizeComment(description) + " */\n")
	}
	builder.WriteString("declare interface " + InputTypeName + " {\n")
	builder.WriteString(body)
	builder.WriteString("}")
	return builder.String()
}

// InputSchemaSample 按契约造一份能通过校验的示例 input。
//
// 试跑输入框需要一个起点。给空对象是最糟的选择：配了契约的函数会当场
// 被「缺少必填字段」挡回来，而作者刚建完函数、还不知道契约长什么样 ——
// 他看到的第一件事就是一条报错。
//
// 只填**必填**字段：可选字段全填上去会让人以为它们是必须的，
// 而这份样例正是很多人对这个接口的第一印象。
func InputSchemaSample(raw json.RawMessage) string {
	schema := decodeSchema(raw)
	if schema == nil {
		return ""
	}
	sample := sampleForObject(schema, 0)
	if len(sample) == 0 {
		return ""
	}
	encoded, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return ""
	}
	return string(encoded)
}

func sampleForObject(schema map[string]any, depth int) map[string]any {
	properties, ok := schema["properties"].(map[string]any)
	if !ok || depth > maxSchemaDepth {
		return nil
	}
	required := requiredSet(schema)
	// 排序只为稳定：不排的话同一份契约每次生成出的样例字段顺序都不同，
	// 而这份文本会被作者直接改，顺序跳来跳去很难用。
	names := make([]string, 0, len(required))
	for name := range properties {
		if _, mandatory := required[name]; mandatory {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	sample := make(map[string]any, len(names))
	for _, name := range names {
		field, _ := properties[name].(map[string]any)
		sample[name] = sampleForField(name, field, depth+1)
	}
	return sample
}

// sampleForField 取一个「显然是占位符、但确实合法」的值。
//
// 两条要求都得满足。只求"显然是占位符"（一律给空串零值）的话，
// 一个 `minLength: 1` 的字段会让「按契约填」填出来的东西当场被校验挡回来 ——
// 那比没有这个按钮更让人困惑。只求"合法"（编一段假数据）的话，
// 它会被原样发出去。
//
// 优先用 default 与 enum 的第一项：那是契约作者自己写下的合法值，
// 比我们编的强。
func sampleForField(name string, field map[string]any, depth int) any {
	if field == nil || depth > maxSchemaDepth {
		return nil
	}
	if value, present := field["default"]; present {
		return value
	}
	if values, ok := field["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	switch schemaType(field) {
	case "string":
		return sampleString(name, field)
	case "number", "integer":
		return sampleNumber(field)
	case "boolean":
		return false
	case "array":
		return sampleArray(name, field, depth)
	case "object":
		nested := sampleForObject(field, depth)
		if nested == nil {
			return map[string]any{}
		}
		return nested
	}
	return nil
}

// sampleString 没有长度下限时给空串（一眼看得出要填），
// 有下限时拿字段名当占位符并补足长度 —— 字段名同样一眼看得出是占位符。
//
// `pattern` / `format` 满足不了：那需要反向生成，而生成不出来的下场是
// 一个看起来合法、实际过不了的样例。这两种约束下仍旧交出占位符，
// 由作者自己改 —— 校验会明确告诉他哪里不匹配。
func sampleString(name string, field map[string]any) string {
	minimum, ok := field["minLength"].(float64)
	if !ok || minimum <= 0 {
		return ""
	}
	placeholder := name
	if placeholder == "" {
		placeholder = "x"
	}
	for len([]rune(placeholder)) < int(minimum) {
		placeholder += "x"
	}
	return placeholder
}

func sampleNumber(field map[string]any) float64 {
	// 有下界时取下界：minimum 为 1 的字段填 0 会立刻被校验挡回来
	if low, ok := field["minimum"].(float64); ok {
		return low
	}
	if low, ok := field["exclusiveMinimum"].(float64); ok {
		return low + 1
	}
	return 0
}

func sampleArray(name string, field map[string]any, depth int) []any {
	minimum, _ := field["minItems"].(float64)
	if minimum <= 0 {
		return []any{}
	}
	items, _ := field["items"].(map[string]any)
	out := make([]any, 0, int(minimum))
	for index := 0; index < int(minimum); index++ {
		out = append(out, sampleForField(name, items, depth+1))
	}
	return out
}

// InputSchemaFieldCount 顶层字段数，控制台用它显示「入参 N 个字段」。
func InputSchemaFieldCount(raw json.RawMessage) int {
	schema := decodeSchema(raw)
	if schema == nil {
		return 0
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return 0
	}
	return len(properties)
}

// HasInputSchema 这份 schema 是不是真的约束了什么。
//
// `{}` 与「一个没有 properties 的对象」都算「没约束」：前者是默认值，
// 后者在 JSON Schema 语义下同样放行任何输入，把它当成「已配置」
// 会让控制台显示一个其实不生效的徽标。
func HasInputSchema(raw json.RawMessage) bool {
	return InputSchemaFieldCount(raw) > 0
}

func decodeSchema(raw json.RawMessage) map[string]any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil
	}
	return decoded
}

// renderObjectBody 渲染一个对象的成员列表（不含外层大括号）。
func renderObjectBody(schema map[string]any, indent, depth int) string {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return ""
	}
	required := requiredSet(schema)

	// 按名字排序：Go 的 map 遍历顺序是随机的，不排的话同一份 schema
	// 每次生成出来的 .d.ts 都不一样，编辑器会反复重建类型。
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxSchemaProperties {
		names = names[:maxSchemaProperties]
	}

	pad := strings.Repeat("  ", indent)
	var builder strings.Builder
	for _, name := range names {
		field, _ := properties[name].(map[string]any)
		if comment := fieldComment(field); comment != "" {
			builder.WriteString(pad + "/** " + comment + " */\n")
		}
		optional := "?"
		if _, mandatory := required[name]; mandatory {
			optional = ""
		}
		builder.WriteString(pad + propertyKey(name) + optional + ": " +
			renderType(field, indent, depth+1) + ";\n")
	}
	// additionalProperties 不为 false 时补一条索引签名：schema 没有禁止
	// 额外字段，那么读一个未声明的字段在运行时是完全合法的，
	// 而 TypeScript 会把它报成错误 —— 那是一条假警报。
	if allowsAdditional(schema) {
		builder.WriteString(pad + "[key: string]: any;\n")
	}
	return builder.String()
}

func requiredSet(schema map[string]any) map[string]struct{} {
	set := map[string]struct{}{}
	list, ok := schema["required"].([]any)
	if !ok {
		return set
	}
	for _, item := range list {
		if name, ok := item.(string); ok {
			set[name] = struct{}{}
		}
	}
	return set
}

func allowsAdditional(schema map[string]any) bool {
	value, present := schema["additionalProperties"]
	if !present {
		return false
	}
	allowed, isBool := value.(bool)
	return !isBool || allowed
}

// renderType 把一个字段 schema 渲染成 TypeScript 类型表达式。
func renderType(field map[string]any, indent, depth int) string {
	if field == nil || depth > maxSchemaDepth {
		return "any"
	}
	// enum 优先于 type：它比 type 精确得多，而两者同时出现时
	// 取 type 会把 "pending" | "paid" 退化成 string。
	if union := renderEnum(field); union != "" {
		return union
	}
	if union := renderUnionOf(field, "oneOf", indent, depth); union != "" {
		return union
	}
	if union := renderUnionOf(field, "anyOf", indent, depth); union != "" {
		return union
	}

	switch schemaType(field) {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		items, _ := field["items"].(map[string]any)
		element := renderType(items, indent, depth+1)
		if strings.ContainsAny(element, " |") {
			return "Array<" + element + ">"
		}
		return element + "[]"
	case "object":
		body := renderObjectBody(field, indent+1, depth)
		if strings.TrimSpace(body) == "" {
			return "Record<string, any>"
		}
		return "{\n" + body + strings.Repeat("  ", indent) + "}"
	}
	return "any"
}

// schemaType 取 type 字段。数组形式（`"type": ["string", "null"]`）取第一个
// 非 null 项 —— 生成 `string | null` 也行，但那会让每一处取值都要先判空，
// 而 schema 作者写这种形式时表达的通常是「可以不传」，那已经由 optional 覆盖了。
func schemaType(field map[string]any) string {
	switch value := field["type"].(type) {
	case string:
		return value
	case []any:
		for _, item := range value {
			if name, ok := item.(string); ok && name != "null" {
				return name
			}
		}
	}
	return ""
}

func renderEnum(field map[string]any) string {
	values, ok := field["enum"].([]any)
	if !ok || len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			parts = append(parts, strconv.Quote(typed))
		case bool:
			parts = append(parts, strconv.FormatBool(typed))
		case float64:
			parts = append(parts, strconv.FormatFloat(typed, 'f', -1, 64))
		case nil:
			parts = append(parts, "null")
		default:
			// 认不出的字面量整条降级：混着 any 的联合类型等于 any，
			// 不如老实写出来。
			return "any"
		}
	}
	return strings.Join(parts, " | ")
}

func renderUnionOf(field map[string]any, keyword string, indent, depth int) string {
	branches, ok := field[keyword].([]any)
	if !ok || len(branches) == 0 {
		return ""
	}
	parts := make([]string, 0, len(branches))
	seen := map[string]struct{}{}
	for _, branch := range branches {
		typed, ok := branch.(map[string]any)
		if !ok {
			return "any"
		}
		rendered := renderType(typed, indent, depth+1)
		if _, duplicate := seen[rendered]; duplicate {
			continue
		}
		seen[rendered] = struct{}{}
		parts = append(parts, rendered)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, " | ")
}

// fieldComment 把 description / default / 数值与长度约束凑成一行 JSDoc。
//
// 约束也写进注释是刻意的：TypeScript 表达不了「最小值 1」，
// 而那恰恰是作者写脚本时需要知道的事 —— 否则他会自己再写一遍范围判断。
func fieldComment(field map[string]any) string {
	if field == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if description, ok := field["description"].(string); ok && description != "" {
		parts = append(parts, sanitizeComment(description))
	}
	if constraint := describeRange(field); constraint != "" {
		parts = append(parts, constraint)
	}
	if pattern, ok := field["pattern"].(string); ok && pattern != "" {
		parts = append(parts, "匹配 "+sanitizeComment(pattern))
	}
	if value, present := field["default"]; present {
		if encoded, err := json.Marshal(value); err == nil {
			parts = append(parts, "默认 "+sanitizeComment(string(encoded)))
		}
	}
	return strings.Join(parts, " · ")
}

func describeRange(field map[string]any) string {
	low, hasLow := numericConstraint(field, "minimum", "minLength", "minItems")
	high, hasHigh := numericConstraint(field, "maximum", "maxLength", "maxItems")
	switch {
	case hasLow && hasHigh:
		return fmt.Sprintf("范围 %s–%s", low, high)
	case hasLow:
		return "最小 " + low
	case hasHigh:
		return "最大 " + high
	}
	return ""
}

func numericConstraint(field map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := field[key].(float64)
		if !ok {
			continue
		}
		return strconv.FormatFloat(value, 'f', -1, 64), true
	}
	return "", false
}

// sanitizeComment 注释里出现 `*/` 会提前关闭注释块，
// 后面的类型声明会被当成代码，整份 .d.ts 从那里开始全部失效。
func sanitizeComment(value string) string {
	value = strings.ReplaceAll(value, "*/", "*∕")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	if len([]rune(value)) > 200 {
		value = string([]rune(value)[:200]) + "…"
	}
	return strings.TrimSpace(value)
}

// propertyKey 不是合法 JS 标识符的字段名要加引号（`order-no`、`3d`）。
func propertyKey(name string) string {
	if name == "" {
		return `""`
	}
	for index, char := range name {
		valid := char == '_' || char == '$' ||
			(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(index > 0 && char >= '0' && char <= '9')
		if !valid {
			return strconv.Quote(name)
		}
	}
	return name
}
