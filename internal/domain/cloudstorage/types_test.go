package cloudstorage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNamespaceAndKeyPatterns(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"favorites", "app.settings", "save-1", "a", "0slot"} {
		if !ValidNamespace(ok) {
			t.Errorf("命名空间 %q 应当合法", ok)
		}
	}
	for _, bad := range []string{"", "Favorites", "-x", ".x", "a/b", "a b", strings.Repeat("a", 65)} {
		if ValidNamespace(bad) {
			t.Errorf("命名空间 %q 应当被拒绝", bad)
		}
	}
	for _, ok := range []string{"default", "Slot_01", "v1.2", "A-b"} {
		if !ValidKey(ok) {
			t.Errorf("键 %q 应当合法", ok)
		}
	}
	// 斜杠、点开头（.. 目录跳转）与超长都不行
	for _, bad := range []string{"", "a/b", "..", ".hidden", "_x", strings.Repeat("k", 129)} {
		if ValidKey(bad) {
			t.Errorf("键 %q 应当被拒绝", bad)
		}
	}
}

func TestConfigNormalize(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig(1)
	cfg.Namespaces = []Namespace{{Key: " favorites ", Name: ""}}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("默认配置应当合法：%v", err)
	}
	if cfg.Namespaces[0].Key != "favorites" || cfg.Namespaces[0].Name != "favorites" {
		t.Fatalf("命名空间应被修整并以键补名称，得到 %+v", cfg.Namespaces[0])
	}

	bad := DefaultConfig(1)
	bad.MaxItemBytes = bad.QuotaBytes + 1
	if bad.Normalize() == nil {
		t.Error("单条目上限超过配额应当被拒绝")
	}

	dup := DefaultConfig(1)
	dup.Namespaces = []Namespace{{Key: "a"}, {Key: "a"}}
	if dup.Normalize() == nil {
		t.Error("重复命名空间应当被拒绝")
	}

	restricted := DefaultConfig(1)
	restricted.RestrictNamespaces = true
	if restricted.Normalize() == nil {
		t.Error("限定命名空间却没有目录应当被拒绝")
	}
	restricted.Namespaces = []Namespace{{Key: "favorites"}}
	if err := restricted.Normalize(); err != nil {
		t.Fatal(err)
	}
	if !restricted.AllowsNamespace("favorites") || restricted.AllowsNamespace("other") {
		t.Error("限定模式下只允许目录内的命名空间")
	}
}

func TestEffectiveQuota(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig(1)
	state := UserState{}
	if state.EffectiveQuota(cfg) != cfg.QuotaBytes {
		t.Error("没有覆盖值时应沿用应用默认")
	}
	override := int64(123)
	state.QuotaOverride = &override
	if state.EffectiveQuota(cfg) != 123 {
		t.Error("覆盖值应当生效")
	}
}

func TestContentRoundTrip(t *testing.T) {
	t.Parallel()

	// json 原样落盘：字段顺序与数字写法都不改
	raw := json.RawMessage(`{"b":1,"a":[1.50,2]}`)
	data, err := DecodeContent(EncodingJSON, raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(raw) {
		t.Fatalf("json 内容应原样落盘，得到 %s", data)
	}
	back, err := EncodeContent(EncodingJSON, data)
	if err != nil || string(back) != string(raw) {
		t.Fatalf("json 读回应与写入一致，得到 %s / %v", back, err)
	}

	text, err := DecodeContent(EncodingText, json.RawMessage(`"你好\n世界"`))
	if err != nil || string(text) != "你好\n世界" {
		t.Fatalf("text 解码失败：%q / %v", text, err)
	}

	// base64 标准 / URL 安全 / 无 padding 都认
	for _, encoded := range []string{`"AQID/w=="`, `"AQID_w"`, `"AQID/w"`} {
		bin, err := DecodeContent(EncodingBase64, json.RawMessage(encoded))
		if err != nil || len(bin) != 4 || bin[3] != 0xff {
			t.Fatalf("base64 %s 解码失败：%v / %v", encoded, bin, err)
		}
	}

	if _, err := DecodeContent(EncodingJSON, json.RawMessage(`{`)); err == nil {
		t.Error("非法 JSON 应当被拒绝")
	}
	if _, err := DecodeContent(EncodingText, json.RawMessage(`123`)); err == nil {
		t.Error("text 编码的内容必须是字符串")
	}
	if _, err := DecodeContent(EncodingBase64, json.RawMessage(`"***"`)); err == nil {
		t.Error("非法 base64 应当被拒绝")
	}
}
