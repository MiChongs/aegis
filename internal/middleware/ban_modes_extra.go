package middleware

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────
// 增强型"恶心"封禁模式的响应实现
//   - zipBombResponse      : gzip 压缩炸弹
//   - chunkedInfiniteRespond: 无限流式响应
//   - infiniteRedirectRespond: 无限 302 跳转链路
//   - mirrorRequestRespond : 完整回显攻击者请求
//   - fakeLoginRespond     : 伪装登录页
//   - randomGarbageRespond : 随机二进制垃圾
//   - cursedHeadersRespond : 50+ 伪造响应头
//   - jsonBombRespond      : 深度嵌套 JSON
//   - cookieBombRespond    : 80+ 巨型 Cookie
//   - reverseSlowlorisRespond: 服务端反向 slowloris
// ─────────────────────────────────────────────────────────────────────

// zipBombBody 启动时预计算一次；10MiB 的 0x00 压缩后约 10KiB
var (
	zipBombBody     []byte
	zipBombBodyOnce sync.Once
)

func getZipBombBody() []byte {
	zipBombBodyOnce.Do(func() {
		const decompressedSize = 10 * 1024 * 1024 // 10 MiB 的零
		buf := &bytes.Buffer{}
		gz, _ := gzip.NewWriterLevel(buf, gzip.BestCompression)
		// 分块写入 10MiB 零，压缩比极高
		zeros := make([]byte, 4096)
		remaining := decompressedSize
		for remaining > 0 {
			n := len(zeros)
			if n > remaining {
				n = remaining
			}
			_, _ = gz.Write(zeros[:n])
			remaining -= n
		}
		_ = gz.Close()
		zipBombBody = buf.Bytes()
	})
	return zipBombBody
}

// zipBombResponse 返回 gzip 压缩炸弹（10KiB → 10MiB）。
// 对支持 gzip 解码的扫描器 / 抓取脚本，解压会快速耗尽内存。
func zipBombResponse(c *gin.Context) {
	body := getZipBombBody()
	c.Header("Content-Encoding", "gzip")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Length", fmt.Sprintf("%d", len(body)))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(body)
}

// chunkedInfiniteRespond 无限 chunked 流式响应。
// 每秒输出 1KiB 垃圾，直到客户端断开或 5 分钟兜底。
func chunkedInfiniteRespond(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	hardStop := time.After(5 * time.Minute)
	buf := bytes.Repeat([]byte("."), 1024)

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-hardStop:
			return
		case <-ticker.C:
			if _, err := c.Writer.Write(buf); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// infiniteRedirectRespond 让攻击者陷入无限 302 跳转。
// 每次跳转到 /_trap/<16 字节随机 hex>?q=<noise>，被跳回来还是一样。
func infiniteRedirectRespond(c *gin.Context) {
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	nonce := hex.EncodeToString(random)
	target := "/_trap/" + nonce + "?q=" + nonce
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, target)
}

// mirrorRequestRespond 把攻击者请求 1:1 回显。
// 让扫描器看到自己的 payload 回显，误判为 XSS/SSRF 触点，陷入调试死循环。
func mirrorRequestRespond(c *gin.Context) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s %s\n", c.Request.Method, c.Request.URL.RequestURI(), c.Request.Proto)
	for k, v := range c.Request.Header {
		// 跳过敏感头回显
		if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Cookie") {
			continue
		}
		for _, vv := range v {
			fmt.Fprintf(&sb, "%s: %s\n", k, vv)
		}
	}
	sb.WriteString("\n")
	if c.Request.Body != nil {
		body, _ := io.ReadAll(io.LimitReader(c.Request.Body, 4*1024))
		sb.Write(body)
	}
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString(sb.String())
}

// fakeLoginRespond 伪装一个看似可真实登录的页面；任何登录尝试都会走蜜罐链路。
func fakeLoginRespond(c *gin.Context) {
	const html = `<!doctype html><html lang="zh-CN"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Login · Admin Console</title>
<style>
body{margin:0;font-family:-apple-system,"Segoe UI",Roboto,sans-serif;background:#f4f6fa;color:#1f2937;display:grid;place-items:center;min-height:100vh}
.card{width:360px;background:#fff;border-radius:14px;box-shadow:0 8px 32px rgba(0,0,0,0.08);padding:36px 32px}
h1{margin:0 0 6px;font-size:20px}p.sub{margin:0 0 22px;color:#6b7280;font-size:13px}
label{display:block;font-size:12px;color:#374151;margin:14px 0 6px}
input{width:100%;height:40px;padding:0 12px;border:1px solid #d1d5db;border-radius:8px;font-size:14px;box-sizing:border-box}
input:focus{outline:2px solid #3b82f6;outline-offset:-1px;border-color:#3b82f6}
button{margin-top:20px;width:100%;height:40px;border:0;border-radius:8px;background:#2563eb;color:#fff;font-weight:600;cursor:pointer}
button:hover{background:#1d4ed8}
.hint{margin-top:18px;font-size:11px;color:#9ca3af;text-align:center}
</style></head><body><form class="card" method="post" autocomplete="off">
<h1>Admin Console</h1><p class="sub">Internal access · v3.8.2</p>
<label>Username</label><input name="u" autofocus required>
<label>Password</label><input name="p" type="password" required>
<label>2FA Code</label><input name="otp" inputmode="numeric" maxlength="6">
<button type="submit">Sign in</button>
<div class="hint">Having trouble? Contact ops@internal.local</div></form></body></html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Server", "nginx/1.18.0")       // 假身份
	c.Header("X-Powered-By", "Django/4.2.7") // 误导指纹
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString(html)
}

// randomGarbageRespond 返回 512KiB 随机二进制 + 乱配 Content-Type。
func randomGarbageRespond(c *gin.Context) {
	buf := make([]byte, 512*1024)
	_, _ = rand.Read(buf)
	cts := []string{
		"application/octet-stream",
		"image/jpeg",
		"application/pdf",
		"application/x-msdownload",
		"audio/mpeg",
	}
	c.Header("Content-Type", cts[mrand.Intn(len(cts))])
	c.Header("Content-Disposition", `attachment; filename="data.bin"`)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(buf)
}

// cursedHeadersRespond 返回 50+ 矛盾/伪造响应头，破坏指纹检测。
func cursedHeadersRespond(c *gin.Context) {
	fakes := []struct{ k, v string }{
		{"Server", "Apache/2.4.29 (Ubuntu)"},
		{"X-Powered-By", "PHP/7.4.3"},
		{"X-AspNet-Version", "4.0.30319"},
		{"X-Generator", "Drupal 8 (https://www.drupal.org)"},
		{"X-Frame-Options", "ALLOWALL"},
		{"X-Backend-Server", "web-42.prod.internal.local"},
		{"X-Served-By", "cache-lhr7344-LHR"},
		{"X-Cache", "HIT"},
		{"X-Cache-Hits", "256"},
		{"X-Varnish", "1020983727 1020856115"},
		{"X-Content-Digest", "sha256:7f9b2c3a..."},
		{"X-Request-Id", "req_" + randomHex(16)},
		{"X-Trace-Id", randomHex(32)},
		{"X-Processed-By", "spring-cloud-gateway"},
		{"X-Fastly-Request-ID", randomHex(40)},
		{"X-AMZ-Request-Id", randomHex(16)},
		{"X-AMZ-CF-Id", randomHex(56)},
		{"X-Azure-Ref", "Ref A: " + randomHex(32)},
		{"X-Runtime", "0.034721"},
		{"X-Application-Version", "6.66.6-beta-honeypot"},
		{"X-Database", "postgresql 15.4"},
		{"X-Admin-User", "root"},
		{"X-Session-Id", randomHex(24)},
		{"X-Feature-Flag", "chaos-monkey,debug-mode,admin-bypass"},
		{"Strict-Transport-Security", "max-age=0"},
		{"Public-Key-Pins", "pin-sha256=\"invalid\""},
		{"X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet"},
		{"ETag", `"`+randomHex(16)+`"`},
	}
	// 再造 20+ 随机 X- header 增加噪声
	for i := 0; i < 25; i++ {
		c.Writer.Header().Add(fmt.Sprintf("X-Random-%d", i), randomHex(8+mrand.Intn(24)))
	}
	for _, h := range fakes {
		c.Header(h.k, h.v)
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString(`{"status":"ok","debug":true,"secrets_loaded":true}`)
}

// jsonBombRespond 工业级 JSON 炸弹。
//
// 设计原理：**多攻击矢量叠加 + gzip 压缩 = 武器级威慑**
//   ① 深度嵌套数组 500,000 层
//        – 直接干爆所有默认栈配置（Python json.loads / V8 / Ruby json / PHP json_decode
//          默认递归限制 1000-50000；500k 是教科书级 blowup）
//   ② 对象 200,000 个 unicode-escape 键值对
//        – 每键都是 `中文_N`；parser 必须对每个 `\uXXXX` 做 UTF-16 → UTF-8 转换，
//          密集的 hash table 冲突 + string alloc 会把 GC 打爆
//   ③ 10 万位数字字面量（BigInt 陷阱）
//        – Python decimal.Decimal / Ruby Integer / PHP gmp 会尝试任意精度解析，O(N²)
//   ④ 100 万个 unicode escape 组成的超长字符串
//        – 解码阶段必须线性扫描并生成 UTF-8 缓冲
//   ⑤ 全部输出走 gzip（Content-Encoding: gzip）
//        – 有效载荷压缩后约 2–4 MB 上线；解压后 **200–500 MB**
//        – 解析后内存驻留 **2-5 GB**
//
// 服务端开销极低：gzip 压缩比 >100×，流式写入无内存占用。
// 攻击者侧：Python / Node / Ruby / PHP / Go encoding/json 几乎必定 OOM 或数分钟僵死。
func jsonBombRespond(c *gin.Context) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Encoding", "gzip")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no") // 阻止 nginx 缓冲响应
	c.Status(http.StatusOK)

	gw := gzip.NewWriter(c.Writer)
	defer gw.Close()

	const (
		nestedDepth  = 500000 // 500k 层嵌套数组
		keyCount     = 200000 // 200k 个 unicode-escape 键
		unicodeLen   = 500000 // 500k 次重复 unicode escape
		numericDigit = 100000 // 10 万位数字
	)

	// ① 深度嵌套数组开头
	openBrk := bytes.Repeat([]byte("["), 64*1024)
	remaining := nestedDepth
	for remaining > 0 {
		n := len(openBrk)
		if n > remaining {
			n = remaining
		}
		if _, err := gw.Write(openBrk[:n]); err != nil {
			return
		}
		remaining -= n
	}

	// ② Inner object with unicode-escape key/value pairs.
	// Emit literal ASCII \uXXXX sequences (6 bytes each). JSON parsers must
	// perform UTF-16 -> UTF-8 decoding + surrogate pair handling + string alloc
	// per escape. Millions of escapes will crush the string allocator and GC.
	_, _ = gw.Write([]byte(`{`))
	keyTpl := []byte(`"\u4e2d\u6587\u7f51\u7edc_`)
	valTpl := []byte(`"\u7f51\u7edc\u653b\u51fb\u6d6a\u8d39_`)
	sepColon := []byte(`":`)
	closeQuote := []byte(`"`)
	for i := 0; i < keyCount; i++ {
		if i > 0 {
			_, _ = gw.Write([]byte(`,`))
		}
		_, _ = gw.Write(keyTpl)
		_, _ = fmt.Fprintf(gw, "%d", i)
		_, _ = gw.Write(sepColon)
		_, _ = gw.Write(valTpl)
		_, _ = fmt.Fprintf(gw, "%d", i)
		_, _ = gw.Write(closeQuote)
		if i%10000 == 0 {
			select {
			case <-c.Request.Context().Done():
				return
			default:
			}
		}
	}

	// ③ BigInt 陷阱（十万位纯 1）
	_, _ = gw.Write([]byte(`,"n":`))
	ones := bytes.Repeat([]byte("1"), 4096)
	done := 0
	for done < numericDigit {
		n := len(ones)
		if n > numericDigit-done {
			n = numericDigit - done
		}
		if _, err := gw.Write(ones[:n]); err != nil {
			return
		}
		done += n
	}

	// ④ 百万 unicode-escape 字符串
	_, _ = gw.Write([]byte(`,"s":"`))
	unicodeChunk := []byte(`\u4e2d\u6587\u7f51\u7edc\u653b\u51fb\u6d6a\u8d39`)
	for i := 0; i < unicodeLen; i++ {
		if _, err := gw.Write(unicodeChunk); err != nil {
			return
		}
	}
	_, _ = gw.Write([]byte(`"`))

	_, _ = gw.Write([]byte(`}`))

	// ⑤ 对应数量的闭合括号
	closeBrk := bytes.Repeat([]byte("]"), 64*1024)
	remaining = nestedDepth
	for remaining > 0 {
		n := len(closeBrk)
		if n > remaining {
			n = remaining
		}
		if _, err := gw.Write(closeBrk[:n]); err != nil {
			return
		}
		remaining -= n
	}
}

// cookieBombRespond 下发 80+ 大 Cookie；攻击者后续每次请求都会携带 ~100KiB 头。
func cookieBombRespond(c *gin.Context) {
	for i := 0; i < 80; i++ {
		name := fmt.Sprintf("__trap_%02d", i)
		value := randomHex(512)
		c.SetCookie(name, value, 3600*24*30, "/", "", false, true)
	}
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("ok\n")
}

// reverseSlowlorisRespond 服务端反向 slowloris：
// 劫持连接后手写 HTTP/1.1 响应，每 1 秒 1 字节地慢吐头部，持续数分钟。
func reverseSlowlorisRespond(c *gin.Context) {
	hj, ok := c.Writer.(http.Hijacker)
	if !ok {
		// 劫持失败则降级到逐字节 slow 响应
		slowResponseFallback(c)
		return
	}
	conn, bw, err := hj.Hijack()
	if err != nil {
		slowResponseFallback(c)
		return
	}
	defer conn.Close()

	lines := []string{
		"HTTP/1.1 200 OK\r\n",
		"Content-Type: text/plain\r\n",
		"Cache-Control: no-store\r\n",
		"X-Server: Apache/2.4\r\n",
		"X-Slow-Drip: active\r\n",
		"Connection: close\r\n",
		"\r\n",
		"loading...",
	}
	flat := strings.Join(lines, "")
	deadline := time.Now().Add(5 * time.Minute)
	_ = conn.SetWriteDeadline(deadline)

	for i := 0; i < len(flat); i++ {
		if time.Now().After(deadline) {
			return
		}
		if _, err := bw.Write([]byte{flat[i]}); err != nil {
			return
		}
		if err := bw.Flush(); err != nil {
			return
		}
		time.Sleep(1 * time.Second)
	}
}

// slowResponseFallback 简单版本：500ms 一字节滴给客户端，不劫持 conn。
func slowResponseFallback(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(http.StatusForbidden)
	msg := []byte("forbidden")
	for _, b := range msg {
		select {
		case <-c.Request.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
			_, _ = c.Writer.Write([]byte{b})
			c.Writer.Flush()
		}
	}
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)[:n]
}
