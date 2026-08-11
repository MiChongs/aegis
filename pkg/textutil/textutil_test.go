package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateBytesNeverSplitsRune(t *testing.T) {
	// "风险评估记录" 每字 3 字节，共 18 字节；从 0 到 18 每个字节预算都不能切出半个字
	const s = "风险评估记录"
	for max := 0; max <= len(s)+4; max++ {
		got := TruncateBytes(s, max)
		if !utf8.ValidString(got) {
			t.Fatalf("max=%d 切出了非法 UTF-8: %q", max, got)
		}
		if len(got) > max && max > 0 {
			t.Fatalf("max=%d 超出预算: %d 字节", max, len(got))
		}
		if !strings.HasPrefix(s, got) {
			t.Fatalf("max=%d 结果不是原串前缀: %q", max, got)
		}
	}
}

func TestTruncateBytesBudgetIsBytesNotRunes(t *testing.T) {
	// 6 字节预算下只能装 2 个汉字，而不是 6 个
	if got := TruncateBytes("风险评估记录", 6); got != "风险" {
		t.Fatalf("want 风险, got %q", got)
	}
	if got := TruncateBytes("abcdef", 6); got != "abcdef" {
		t.Fatalf("ASCII 未超预算不应被动: %q", got)
	}
}

func TestTruncateBytesDropsPartialTailFromUpstream(t *testing.T) {
	// 模拟上游已经按字节切过一刀（响应缓冲区写满时的分片截断）
	broken := "风险"[:4] // "风" + "险" 的首字节 0xe9
	if utf8.ValidString(broken) {
		t.Fatal("用例前提失效：构造的串应当是非法 UTF-8")
	}
	got := TruncateBytes(broken, 1024)
	if got != "风" {
		t.Fatalf("want 风, got %q", got)
	}
}

func TestSanitizeUTF8(t *testing.T) {
	if got := SanitizeUTF8("正常文本"); got != "正常文本" {
		t.Fatalf("合法输入不应被改动: %q", got)
	}
	got := SanitizeUTF8("前\xe7后")
	if !utf8.ValidString(got) {
		t.Fatalf("清洗后仍非法: %q", got)
	}
	if !strings.Contains(got, "前") || !strings.Contains(got, "后") {
		t.Fatalf("清洗吃掉了合法字符: %q", got)
	}
	// 用户原本就写了 U+FFFD 时不应被当成非法字节处理
	if got := SanitizeUTF8("已知�未知"); got != "已知�未知" {
		t.Fatalf("合法 U+FFFD 被改动: %q", got)
	}
}
