package appfunction

import (
	"encoding/json"
	"strings"
	"testing"
)

// 生成出来的类型是作者写脚本时唯一的参照。它错一处的代价不对称：
// 少一个字段只是少一点帮助，而**多**一个不存在的字段、或者把可选说成必填，
// 会让作者对着一条假警报改代码，那比没有类型糟得多。
func TestInputSchemaDeclarationRendersFields(t *testing.T) {
	t.Parallel()

	declaration := InputSchemaDeclaration(json.RawMessage(`{
      "type": "object",
      "required": ["orderNo"],
      "properties": {
        "orderNo":  { "type": "string", "description": "业务订单号", "minLength": 1 },
        "quantity": { "type": "integer", "minimum": 1, "maximum": 99, "default": 1 },
        "channel":  { "type": "string", "enum": ["web", "ios"] },
        "paid":     { "type": "boolean" },
        "tags":     { "type": "array", "items": { "type": "string" } },
        "coupon":   { "type": "object", "properties": { "code": { "type": "string" } } }
      }
    }`))

	for _, want := range []string{
		"declare interface AegisInput {",
		"orderNo: string;",     // required → 无问号
		"quantity?: number;",   // integer → number，可选
		`channel?: "web" | "ios";`,
		"paid?: boolean;",
		"tags?: string[];",
		"code?: string;", // 嵌套对象展开
	} {
		if !strings.Contains(declaration, want) {
			t.Errorf("生成的类型里缺少 %q：\n%s", want, declaration)
		}
	}

	// 约束写进注释：TypeScript 表达不了「最小值 1」，
	// 而那恰恰是作者需要知道、否则会自己再写一遍判断的东西
	if !strings.Contains(declaration, "业务订单号") || !strings.Contains(declaration, "最小 1") {
		t.Errorf("描述与约束应进 JSDoc：\n%s", declaration)
	}
	if !strings.Contains(declaration, "范围 1–99") || !strings.Contains(declaration, "默认 1") {
		t.Errorf("范围与默认值应进 JSDoc：\n%s", declaration)
	}
}

// 没有 schema 时必须什么都不生成，调用方据此回落到 any ——
// 与加这项之前的行为完全一致。
func TestInputSchemaDeclarationEmptyWhenUnconstrained(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "null", "{}", `{"type":"object"}`, `{"type":"string"}`, "不是 JSON"} {
		if got := InputSchemaDeclaration(json.RawMessage(raw)); got != "" {
			t.Errorf("%q 不该生成类型，实际：\n%s", raw, got)
		}
		if HasInputSchema(json.RawMessage(raw)) {
			t.Errorf("%q 不该算「已配置契约」", raw)
		}
	}
}

// additionalProperties 决定要不要索引签名。
//
// schema 没有禁止额外字段时，读一个未声明的字段在运行时完全合法；
// 不补索引签名会让 TypeScript 把它报成错误 —— 一条假警报。
func TestInputSchemaDeclarationRespectsAdditionalProperties(t *testing.T) {
	t.Parallel()

	open := InputSchemaDeclaration(json.RawMessage(
		`{"type":"object","additionalProperties":true,"properties":{"a":{"type":"string"}}}`))
	if !strings.Contains(open, "[key: string]: any;") {
		t.Errorf("允许额外字段时应有索引签名：\n%s", open)
	}

	closed := InputSchemaDeclaration(json.RawMessage(
		`{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string"}}}`))
	if strings.Contains(closed, "[key: string]: any;") {
		t.Errorf("禁止额外字段时不该有索引签名：\n%s", closed)
	}
	// 未写 additionalProperties 时按「封闭」渲染：多给一条索引签名会让
	// 所有拼错的字段名都变成合法的 any，类型检查等于白做
	silent := InputSchemaDeclaration(json.RawMessage(
		`{"type":"object","properties":{"a":{"type":"string"}}}`))
	if strings.Contains(silent, "[key: string]: any;") {
		t.Errorf("未声明 additionalProperties 时不该有索引签名：\n%s", silent)
	}
}

// 认不出来的构造一律降级成 any，绝不猜。
func TestInputSchemaDeclarationFallsBackToAny(t *testing.T) {
	t.Parallel()

	declaration := InputSchemaDeclaration(json.RawMessage(`{
      "type": "object",
      "properties": {
        "ref":    { "$ref": "#/definitions/Thing" },
        "mixed":  { "oneOf": [{ "type": "string" }, { "type": "number" }] },
        "weird":  { "type": "wat" }
      }
    }`))
	if !strings.Contains(declaration, "ref?: any;") {
		t.Errorf("$ref 应降级成 any：\n%s", declaration)
	}
	if !strings.Contains(declaration, "mixed?: string | number;") {
		t.Errorf("oneOf 应渲染成联合类型：\n%s", declaration)
	}
	if !strings.Contains(declaration, "weird?: any;") {
		t.Errorf("未知 type 应降级成 any：\n%s", declaration)
	}
}

// 字段名不是合法 JS 标识符时必须加引号，否则整份 .d.ts 语法错误、
// 编辑器里所有类型一起失效。
func TestInputSchemaDeclarationQuotesAwkwardKeys(t *testing.T) {
	t.Parallel()

	declaration := InputSchemaDeclaration(json.RawMessage(
		`{"type":"object","properties":{"order-no":{"type":"string"},"3d":{"type":"boolean"}}}`))
	if !strings.Contains(declaration, `"order-no"?: string;`) {
		t.Errorf("带连字符的键应加引号：\n%s", declaration)
	}
	if !strings.Contains(declaration, `"3d"?: boolean;`) {
		t.Errorf("数字开头的键应加引号：\n%s", declaration)
	}
}

// 注释里出现 `*/` 会提前关闭注释块，之后的声明会被当成代码 ——
// 整份类型从那一行起全部失效，而这只需要一句用户填的描述就能触发。
func TestInputSchemaDeclarationEscapesCommentTerminator(t *testing.T) {
	t.Parallel()

	declaration := InputSchemaDeclaration(json.RawMessage(
		`{"type":"object","properties":{"a":{"type":"string","description":"结束 */ 之后"}}}`))
	if strings.Contains(declaration, "*/ 之后") {
		t.Errorf("描述里的注释结束符必须被转义：\n%s", declaration)
	}
	if !strings.Contains(declaration, "a?: string;") {
		t.Errorf("转义之后字段声明仍应完整：\n%s", declaration)
	}
}

// 同一份 schema 每次都要生成同一段文本：Go 的 map 遍历顺序是随机的，
// 不排序的话编辑器会因为「类型变了」反复重建语言服务。
func TestInputSchemaDeclarationIsDeterministic(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"object","properties":{
      "z":{"type":"string"},"a":{"type":"string"},"m":{"type":"string"}}}`)
	first := InputSchemaDeclaration(raw)
	for i := 0; i < 20; i++ {
		if got := InputSchemaDeclaration(raw); got != first {
			t.Fatalf("同一份 schema 生成出了两种文本：\n%s\n---\n%s", first, got)
		}
	}
	if strings.Index(first, "a?:") > strings.Index(first, "m?:") {
		t.Errorf("字段应按名字排序：\n%s", first)
	}
}

// 深度递归的 schema 不能把生成器打爆 —— schema 是接入方写的。
func TestInputSchemaDeclarationSurvivesDeepNesting(t *testing.T) {
	t.Parallel()

	nested := `{"type":"string"}`
	for i := 0; i < 60; i++ {
		nested = `{"type":"object","properties":{"n":` + nested + `}}`
	}
	declaration := InputSchemaDeclaration(json.RawMessage(nested))
	if declaration == "" {
		t.Fatal("深层嵌套仍应生成出东西")
	}
	if !strings.Contains(declaration, "any") {
		t.Errorf("超过深度上限的部分应降级成 any：\n%s", declaration)
	}
}

// 内置样例必须自己就能生成出合法类型 —— 它是作者的起点，
// 一份生成不出东西的样例会让人以为这个功能坏了。
func TestInputSchemaTemplateGeneratesTypes(t *testing.T) {
	t.Parallel()

	var probe map[string]any
	if err := json.Unmarshal([]byte(InputSchemaTemplate), &probe); err != nil {
		t.Fatalf("内置样例不是合法 JSON: %v", err)
	}
	declaration := InputSchemaDeclaration(json.RawMessage(InputSchemaTemplate))
	if !strings.Contains(declaration, "orderNo: string;") {
		t.Errorf("样例应生成必填的 orderNo：\n%s", declaration)
	}
	if count := InputSchemaFieldCount(json.RawMessage(InputSchemaTemplate)); count != 5 {
		t.Errorf("样例顶层应有 5 个字段，实际 %d", count)
	}
}

// 生成的声明要能和其余类型拼成一份合法的 .d.ts。
func TestSDKDeclarationEmbedsInputType(t *testing.T) {
	t.Parallel()

	withSchema := SDKDeclarationWithInput([]string{CapUserRead}, json.RawMessage(
		`{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}`))
	if !strings.Contains(withSchema, "declare interface AegisInput {") {
		t.Errorf("应嵌入生成的入参接口：\n%s", withSchema)
	}
	if strings.Contains(withSchema, "declare type AegisInput = any;") {
		t.Error("配了 schema 就不该再回落到 any")
	}
	if err := checkBraceBalance(withSchema); err != nil {
		t.Errorf("拼出来的 .d.ts 括号不平衡：%v", err)
	}

	// 没配 schema 时必须仍然有 AegisInput 这个名字，
	// 否则 AegisContext.input 引用了一个不存在的类型，整份声明失效
	without := SDKDeclarationWithInput([]string{CapUserRead}, nil)
	if !strings.Contains(without, "declare type AegisInput = any;") {
		t.Errorf("未配 schema 时应回落到 any 别名：\n%s", without)
	}
}
