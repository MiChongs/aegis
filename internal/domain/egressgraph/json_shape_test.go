package egressgraph

import (
	"encoding/json"
	"regexp"
	"testing"

	"aegis/pkg/egress"
)

// TestViewHasNoNullLists 控制台按数组读这些字段（.length / .map），Go 的空切片会编码成 null，
// 曾让画布节点库直接崩溃。目录与空画布的编译结果里不允许出现 null 列表。
func TestViewHasNoNullLists(t *testing.T) {
	nullList := regexp.MustCompile(`"(outputs|fields|accepts|nodes|edges|issues|proxyTypes|groupTypes|ruleTypes|builtins|profiles|protocols|strategies|actions)":null`)
	check := func(name string, v any) {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if match := nullList.Find(data); match != nil {
			t.Fatalf("%s 里有 null 列表: %s", name, match)
		}
	}
	check("目录", BuildCatalog("https://mirror/"))
	empty := NewGraph()
	check("空画布", Mask(empty))
	check("空画布编译", ViewOf(Compile(empty)))
	check("旧配置画布", Mask(FromConfig(egress.Config{})))
}
