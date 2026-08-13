package service

import (
	"encoding/json"
	"strings"
	"testing"

	functiondomain "aegis/internal/domain/appfunction"
	apperrors "aegis/pkg/errors"
)

// 入参契约存在的理由：没有它时，接入方少传一个字段的表现是脚本第三行
// 抛 TypeError，而调用方拿到的是 50290「应用函数执行失败」——
// 一个既不说少了什么、也不说是自己传错了的错误。
func TestValidateFunctionInputReportsEveryProblem(t *testing.T) {
	t.Parallel()

	schema := json.RawMessage(`{
      "type": "object",
      "required": ["orderNo", "quantity"],
      "additionalProperties": false,
      "properties": {
        "orderNo":  { "type": "string" },
        "quantity": { "type": "integer", "minimum": 1 }
      }
    }`)

	err := validateFunctionInput(schema, json.RawMessage(`{"quantity": 0, "extra": 1}`))
	if err == nil {
		t.Fatal("既缺字段又越界，不该放行")
	}
	// 错误码要能与「函数执行失败」区分开：一个是调用方传错了，
	// 一个是函数自己挂了，两者该去找的人完全不同
	var typed *apperrors.AppError
	if !asAppError(err, &typed) || typed.Code != 40109 {
		t.Fatalf("应是 40109 入参不符合契约，实际 %v", err)
	}
	for _, want := range []string{"orderNo", "quantity", "extra"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误里应逐条点名 %q，实际：%s", want, err.Error())
		}
	}

	if err := validateFunctionInput(schema, json.RawMessage(`{"orderNo":"A1","quantity":2}`)); err != nil {
		t.Errorf("合法入参不该被拦：%v", err)
	}
}

// 没配契约时必须一律放行 —— 加这项功能本身不该改变任何存量函数的行为。
func TestValidateFunctionInputSkipsWhenUnconstrained(t *testing.T) {
	t.Parallel()

	for _, schema := range []string{"", "{}", "null", `{"type":"object"}`} {
		if err := validateFunctionInput(json.RawMessage(schema), json.RawMessage(`{"any":1}`)); err != nil {
			t.Errorf("schema=%q 应放行任何输入，实际 %v", schema, err)
		}
	}
}

// 保存时就要编译一遍。
//
// 一份编译不过的 schema 在调用时的表现是「校验永远抛错」或者更糟
// 「校验被跳过」，两种都不会在保存那一刻暴露 —— 而它一旦存下去
// 就会作用于每一次真实调用。
func TestNormalizeFunctionInputSchemaRejectsBrokenSchema(t *testing.T) {
	t.Parallel()

	if _, err := normalizeFunctionInputSchema(json.RawMessage(`{"type":"object","properties":{"a":{"type":"nope"}}}`)); err == nil {
		t.Error("非法的 type 应在保存时被拒绝")
	}
	if _, err := normalizeFunctionInputSchema(json.RawMessage(`[1,2]`)); err == nil {
		t.Error("数组不是合法的顶层 schema")
	}
	if _, err := normalizeFunctionInputSchema(json.RawMessage(`{"type":"object","required":"orderNo"}`)); err == nil {
		t.Error("required 必须是数组，写成字符串应被拒绝")
	}

	// 空值一律归一化成 {}，与没配一致
	for _, raw := range []string{"", "null", "{}"} {
		got, err := normalizeFunctionInputSchema(json.RawMessage(raw))
		if err != nil || string(got) != "{}" {
			t.Errorf("%q 应归一化为 {}，实际 %s / %v", raw, got, err)
		}
	}

	// 合法 schema 原样通过并保持可用
	valid := json.RawMessage(`{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}`)
	normalized, err := normalizeFunctionInputSchema(valid)
	if err != nil {
		t.Fatalf("合法 schema 应通过：%v", err)
	}
	if !functiondomain.HasInputSchema(normalized) {
		t.Error("归一化之后应仍然算「已配置契约」")
	}
}

// 保存时的编译与调用时的校验必须走同一条路径，否则会出现
// 「保存时说能用、调用时说 schema 不可用」这种谁也解释不了的组合。
func TestSchemaCompileAndValidateShareOnePath(t *testing.T) {
	t.Parallel()

	definition := map[string]any{
		"type":     "object",
		"required": []any{"a"},
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
		},
	}
	if err := compileInputSchema(definition); err != nil {
		t.Fatalf("这份 schema 应能编译：%v", err)
	}
	if problems := validateAgainstSchema(definition, map[string]any{}); len(problems) == 0 {
		t.Error("缺必填字段应被校验出来")
	}

	broken := map[string]any{"type": "object", "properties": map[string]any{
		"a": map[string]any{"type": []any{123}},
	}}
	if err := compileInputSchema(broken); err == nil {
		t.Error("坏 schema 应编译失败")
	}
	if problems := validateAgainstSchema(broken, map[string]any{}); len(problems) == 0 {
		t.Error("坏 schema 在校验路径上也应报错而不是静默放行")
	}
}
