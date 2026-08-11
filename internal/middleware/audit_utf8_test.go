package middleware

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// 审计里每一处「按字节截断」都可能把汉字劈成两半，而后果要到写库时才以
// `invalid byte sequence for encoding "UTF8"` (SQLSTATE 22021) 暴露出来 ——
// 报错点与出错点隔着好几层，且写入是 fire-and-forget 的，只留一条 warn。
// 因此这里对四条截断路径逐一钉死：它们的产物必须永远是合法 UTF-8。

func TestTrimAuditStringNeverSplitsRune(t *testing.T) {
	// 每字 3 字节，512 字节预算除不尽 —— 天然会落在字符中间
	value := strings.Repeat("风险评估记录", 200)
	got := trimAuditString(value)

	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got[len(got)-8:])
	}
	if len(got) > maxAuditStringBytes {
		t.Fatalf("超出字节预算: %d > %d", len(got), maxAuditStringBytes)
	}
	if !strings.HasPrefix(value, got) {
		t.Fatal("截断结果应当是原串前缀")
	}
}

func TestSetAuditFailureNeverSplitsRune(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	SetAuditFailure(c, &longChineseError{})

	stored, ok := c.Get(auditContextErrorKey)
	if !ok {
		t.Fatal("错误摘要未写入上下文")
	}
	if summary, _ := stored.(string); !utf8.ValidString(summary) {
		t.Fatalf("错误摘要不是合法 UTF-8: %q", summary)
	}
}

// TestAuditResponseWriterCapNeverSplitsRune 响应摘要写满时的截断。
// 这条是线上故障（monitor.risk.assessments.read）的真实触发路径：
// 风控记录列表带中文规则名，响应体超过 2KiB 上限，恰好切在汉字中间。
//
// 分片大小刻意与 3 字节字符长度互质：分片边界与字符边界无关，
// 缓冲区在达到上限之前就可能停在字符中间，因此边界不能只看单个分片。
func TestAuditResponseWriterCapNeverSplitsRune(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(strings.Repeat("异地登录风险", 2000))
	for _, chunk := range []int{7, 64, 999, len(body)} {
		buf := &bytes.Buffer{}
		w := &auditResponseWriter{ResponseWriter: discardWriter{}, buf: buf}
		for offset := 0; offset < len(body); offset += chunk {
			end := min(offset+chunk, len(body))
			if _, err := w.Write(body[offset:end]); err != nil {
				t.Fatalf("chunk=%d 写入失败: %v", chunk, err)
			}
		}

		if buf.Len() > maxAuditResponseSnippet {
			t.Fatalf("chunk=%d 缓冲区超出上限: %d", chunk, buf.Len())
		}
		if !utf8.Valid(buf.Bytes()) {
			t.Fatalf("chunk=%d 缓冲区切出了半个字符", chunk)
		}
		if w.size != len(body) {
			t.Fatalf("chunk=%d 响应体总字节数不应受摘要上限影响: %d != %d", chunk, w.size, len(body))
		}

		if snippet, _, _ := extractResponseSnippet(buf.Bytes(), 200); !utf8.ValidString(snippet) {
			t.Fatalf("chunk=%d 摘要非法", chunk)
		}
	}
}

func TestExtractResponseSnippetNeverSplitsRune(t *testing.T) {
	raw := []byte(strings.Repeat("超出上限的中文响应体", 1000))
	snippet, _, _ := extractResponseSnippet(raw, 200)

	if !utf8.ValidString(snippet) {
		t.Fatal("摘要不是合法 UTF-8")
	}
	if !strings.HasSuffix(snippet, "...<truncated>") {
		t.Fatal("超限时应带截断标记")
	}
}

// ── 测试替身

type longChineseError struct{}

func (e *longChineseError) Error() string {
	// 远超 300 字节上限，且 300 除以 3 有余数
	return strings.Repeat("写入风险评估记录失败", 100)
}

type discardWriter struct{ gin.ResponseWriter }

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
