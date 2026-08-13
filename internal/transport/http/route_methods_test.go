package httptransport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/internal/config"

	"github.com/gin-gonic/gin"
)

// 方法收口的三条行为。它们都属于「不会有人手工去试、坏了也没有测试会红」的那一类：
// 路径存在但方法不对，是运维与接入方每天都在制造、却从不出现在功能用例里的请求。

func doRequest(t *testing.T, engine *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// 方法不匹配必须回 405，且带上 Allow。
//
// 这里曾经回 501。它是 5xx，所以「调用方把 GET 写成了 POST」会被算进服务端
// 错误率、触发告警、并让重试逻辑（普遍对 5xx 重试）把必然失败的请求打回来。
func TestMethodMismatchAnswers405WithAllow(t *testing.T) {
	engine := newTestRouter(t)

	cases := []struct {
		method, path, wantAllow string
	}{
		{http.MethodDelete, "/healthz", "GET"},
		{http.MethodPut, "/api/admin/auth/login", "POST"},
		{http.MethodPost, "/api/public/branding", "GET"},
	}

	for _, tc := range cases {
		rec := doRequest(t, engine, tc.method, tc.path, nil)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s 回了 %d，应为 405 —— 5xx 会把调用方的用法错误计成服务端故障",
				tc.method, tc.path, rec.Code)
		}
		allow := rec.Header().Get("Allow")
		if allow == "" {
			t.Errorf("%s %s 的 405 没有 Allow 头：RFC 9110 §15.5.6 要求必须有，"+
				"生成式客户端靠它自我纠正", tc.method, tc.path)
		}
		if !strings.Contains(allow, tc.wantAllow) {
			t.Errorf("%s %s 的 Allow=%q，应含 %s", tc.method, tc.path, allow, tc.wantAllow)
		}

		var body struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s 响应不是 JSON：%v", tc.method, tc.path, err)
		}
		if body.Code != 40500 {
			t.Errorf("%s %s 业务码为 %d，应为 40500（错误码约定是 HTTP 状态码乘 100）",
				tc.method, tc.path, body.Code)
		}
	}
}

// OPTIONS 在 CORS 管不到的两种情形下也要给出答案。
//
// gin-contrib/cors 只处理带 Origin 的请求，且 CORS 未启用时中间件整个直通。
// 这两种漏下来的 OPTIONS 若回 405，等于对着「这个资源支持什么方法」这个提问
// 回答「不支持提问」，而答案本身就在 Allow 头里躺着。
func TestOptionsFallsBackTo204WithAllow(t *testing.T) {
	corsOn, err := NewRouter(RouterDeps{CORS: config.CORSConfig{
		Enabled:      true,
		AllowOrigins: []string{"https://console.example.com"},
		AllowMethods: []string{"GET", "POST"},
	}})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	cases := []struct {
		name    string
		engine  *gin.Engine
		headers map[string]string
	}{
		{"CORS 关闭时的预检", newTestRouter(t), map[string]string{
			"Origin":                        "https://console.example.com",
			"Access-Control-Request-Method": "POST",
		}},
		{"不带 Origin 的能力探测", corsOn, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, tc.engine, http.MethodOptions, "/api/admin/auth/login", tc.headers)
			if rec.Code != http.StatusNoContent {
				t.Errorf("回了 %d，应为 204", rec.Code)
			}
			if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "POST") {
				t.Errorf("Allow=%q，应含 POST", allow)
			}
		})
	}

	// 带 Origin 且 CORS 开启时仍应由 CORS 中间件短路，并带上 ACAO ——
	// 兜底不能把真正的预检抢过来，否则浏览器拿不到跨域许可。
	rec := doRequest(t, corsOn, http.MethodOptions, "/api/admin/auth/login", map[string]string{
		"Origin":                        "https://console.example.com",
		"Access-Control-Request-Method": "POST",
	})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Errorf("真正的预检丢了 Access-Control-Allow-Origin（得到 %q）：跨域写请求会全部失败", got)
	}
}

// 探针与头像必须响应 HEAD。
//
// gin 按方法分树，GET 不会顺带响应 HEAD —— 这与 net/http 的 ServeMux 不同，
// 因此很容易想当然。而负载均衡器的存活检查、邮件客户端的图片代理、
// 链接检查器发的都是 HEAD，收到 405 的表现是「服务好着呢，探针全红」。
//
// 这条**必须**起真实服务器，不能用 httptest.NewRecorder：body 的丢弃是
// net/http 在 response.write 里按 `req.Method == "HEAD"` 做的，
// 而 Recorder 只是个 buffer，不实现那一层。拿 Recorder 断言「HEAD 无 body」
// 会得到一个假阳性的失败；反过来，若哪天改成手工丢弃 body，
// Recorder 又会给出假阴性的通过。
func TestHeadIsServedOnProbeAndAvatarRoutes(t *testing.T) {
	server := httptest.NewServer(newTestRouter(t))
	defer server.Close()

	for _, path := range headCapablePaths {
		req, err := http.NewRequest(http.MethodHead, server.URL+path, nil)
		if err != nil {
			t.Fatalf("构造请求失败：%v", err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("HEAD %s：%v", path, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("读 HEAD %s 响应体：%v", path, err)
		}

		if resp.StatusCode == http.StatusMethodNotAllowed ||
			resp.StatusCode == http.StatusNotImplemented {
			t.Errorf("HEAD %s 回了 %d：该路径没有注册 HEAD", path, resp.StatusCode)
		}
		if len(body) != 0 {
			t.Errorf("HEAD %s 带了 %d 字节响应体，HEAD 响应不能有 body",
				path, len(body))
		}
	}
}

// 显式注册了 HEAD 的路径。两条测试共用，避免各写一份而漂移。
var headCapablePaths = []string{"/healthz", "/readyz", "/api/avatars/x", "/api/avatar/x"}

// HEAD 与 GET 的状态码必须一致。
//
// 如果 HEAD 走了另一条捷径（比如不执行业务逻辑直接回 200），它就成了一个
// 绕过鉴权与状态判定的资源探测器：GET 回 401/404 的地址，HEAD 却回 200。
func TestHeadMirrorsGetStatus(t *testing.T) {
	engine := newTestRouter(t)

	for _, path := range headCapablePaths {
		head := doRequest(t, engine, http.MethodHead, path, nil)
		get := doRequest(t, engine, http.MethodGet, path, nil)
		if head.Code != get.Code {
			t.Errorf("%s：HEAD 回 %d 而 GET 回 %d —— 两者不一致意味着 HEAD 可以用来"+
				"探测 GET 拿不到的资源", path, head.Code, get.Code)
		}
	}
}

// HEAD 不进 OpenAPI：它与同路径的 GET 共用契约，单独列出只会让生成式客户端
// 多出一批名字雷同、永远拿不到数据的方法。
func TestOpenAPISpecOmitsHeadOperations(t *testing.T) {
	engine := newTestRouter(t)
	spec, err := BuildOpenAPISpec(engine, DefaultDocsOptions())
	if err != nil {
		t.Fatalf("BuildOpenAPISpec: %v", err)
	}

	for path, item := range spec.Paths.Map() {
		if item.Head != nil {
			t.Errorf("%s 在规范里带了 head 操作", path)
		}
	}

	// 反向确认：HEAD 被跳过不等于把整条路径漏掉，同路径的 GET 还得在。
	if item := spec.Paths.Find("/healthz"); item == nil || item.Get == nil {
		t.Error("/healthz 的 GET 操作从规范里消失了")
	}
}
