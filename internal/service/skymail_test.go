package service

import (
	emaildomain "aegis/internal/domain/email"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

// fakeSkymail 一个最小的 Cloud Mail：业务错误照真实实现返回 HTTP 200 + 信封里的 code。
type fakeSkymail struct {
	t        *testing.T
	logins   atomic.Int32
	mu       sync.Mutex
	tokens   map[string]bool
	sent     []skymailSendRequest
	accounts []map[string]any
	// expireNext 置 true 时，下一次带令牌的请求回 401 并作废全部令牌
	expireNext atomic.Bool
	sendReply  func() map[string]any
}

func newFakeSkymail(t *testing.T) (*fakeSkymail, *httptest.Server) {
	fake := &fakeSkymail{t: t, tokens: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeSkymail) reply(w http.ResponseWriter, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": data})
}

func (f *fakeSkymail) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/login" {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "sender@mail.test" || body["password"] != "secret-1" {
			f.reply(w, 500, "账号或密码错误", nil)
			return
		}
		token := "tok-" + string(rune('a'+f.logins.Add(1)))
		f.mu.Lock()
		f.tokens[token] = true
		f.mu.Unlock()
		f.reply(w, 200, "success", map[string]string{"token": token})
		return
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		f.t.Errorf("Cloud Mail 的令牌不带 Bearer 前缀，实际：%q", auth)
	}
	if f.expireNext.CompareAndSwap(true, false) {
		f.mu.Lock()
		f.tokens = map[string]bool{}
		f.mu.Unlock()
	}
	f.mu.Lock()
	valid := f.tokens[auth]
	f.mu.Unlock()
	if !valid {
		f.reply(w, 401, "身份认证失效,请重新登录", nil)
		return
	}
	switch r.URL.Path {
	case "/api/my/loginUserInfo":
		f.reply(w, 200, "success", map[string]any{
			"userId": 1, "email": "sender@mail.test", "permKeys": []string{"*"},
			"account": map[string]any{"accountId": 7, "email": "sender@mail.test"},
		})
	case "/api/account/list":
		f.reply(w, 200, "success", f.accounts)
	case "/api/email/send":
		var body skymailSendRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.sent = append(f.sent, body)
		f.mu.Unlock()
		if f.sendReply != nil {
			reply := f.sendReply()
			f.reply(w, reply["code"].(int), reply["message"].(string), reply["data"])
			return
		}
		f.reply(w, 200, "success", []map[string]any{{
			"emailId": 42, "type": 1, "status": 1, "accountId": body.AccountID, "createTime": "2099-12-30 23:59:59",
		}})
	default:
		http.NotFound(w, r)
	}
}

func newTestSkymailSender(server *httptest.Server) *skymailEmailSender {
	sender := newSkymailEmailSender(zap.NewNop())
	sender.client.client = server.Client()
	return sender
}

func skymailTestConfig(baseURL string, fromAddress string) *emaildomain.Config {
	return &emaildomain.Config{
		Name:     "cloud-mail",
		Provider: emaildomain.ProviderSkymail,
		Settings: map[string]string{
			skymailKeyBaseURL:          baseURL + "/api",
			skymailKeyLoginEmail:       "sender@mail.test",
			emaildomain.KeyFromAddress: fromAddress,
			emaildomain.KeyFromName:    "Aegis",
		},
		Secrets: map[string]string{skymailKeyPassword: "secret-1"},
	}
}

func TestSkymailSenderSendsAndReusesToken(t *testing.T) {
	fake, server := newFakeSkymail(t)
	sender := newTestSkymailSender(server)
	config := skymailTestConfig(server.URL, "")

	out := emailOutbound{
		To: "user@example.com", Subject: "验证码", HTML: "<p>123456</p>", Text: "123456",
		Attachments: []emailAttachment{{Filename: "receipt.pdf", ContentType: "application/pdf", Content: []byte("%PDF")}},
	}
	for i := 0; i < 2; i++ {
		result, err := sender.Send(context.Background(), config, out)
		if err != nil {
			t.Fatalf("第 %d 次发信失败：%v", i+1, err)
		}
		id, host, ok := parseSkymailProviderMessageID(result.ProviderMessageID)
		if !ok || id != 42 || !strings.HasPrefix(host, "127.0.0.1") {
			t.Fatalf("服务商邮件 ID 应为 42@<主机>，实际 %q", result.ProviderMessageID)
		}
		if result.Status != emaildomain.DeliveryStatusSent {
			t.Fatalf("状态 1 应记为 sent，实际 %q", result.Status)
		}
	}
	if got := fake.logins.Load(); got != 1 {
		t.Fatalf("令牌应被缓存，两封信只登录一次，实际登录 %d 次", got)
	}
	if len(fake.sent) != 2 {
		t.Fatalf("应发出两封，实际 %d", len(fake.sent))
	}
	first := fake.sent[0]
	if first.AccountID != 7 || len(first.ReceiveEmail) != 1 || first.ReceiveEmail[0] != "user@example.com" || first.Name != "Aegis" {
		t.Fatalf("发信请求不对：%+v", first)
	}
	if len(first.Attachments) != 1 || first.Attachments[0].Content != base64.StdEncoding.EncodeToString([]byte("%PDF")) {
		t.Fatalf("附件应以 Base64 上送：%+v", first.Attachments)
	}
}

func TestSkymailRelogsInOnceWhenTokenExpires(t *testing.T) {
	fake, server := newFakeSkymail(t)
	sender := newTestSkymailSender(server)
	config := skymailTestConfig(server.URL, "")
	out := emailOutbound{To: "user@example.com", Subject: "s", HTML: "<p>x</p>"}

	if _, err := sender.Send(context.Background(), config, out); err != nil {
		t.Fatalf("首封失败：%v", err)
	}
	fake.expireNext.Store(true)
	if _, err := sender.Send(context.Background(), config, out); err != nil {
		t.Fatalf("令牌失效后应自动重登并发出，实际：%v", err)
	}
	if got := fake.logins.Load(); got != 2 {
		t.Fatalf("应重新登录一次（共两次），实际 %d 次", got)
	}
}

func TestSkymailBusinessRejectionIsConfigError(t *testing.T) {
	fake, server := newFakeSkymail(t)
	fake.sendReply = func() map[string]any {
		return map[string]any{"code": 500, "message": "发件账号不存在", "data": nil}
	}
	sender := newTestSkymailSender(server)
	_, err := sender.Send(context.Background(), skymailTestConfig(server.URL, ""),
		emailOutbound{To: "user@example.com", Subject: "s", HTML: "<p>x</p>"})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != 40070 || appErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("HTTP 200 + code 500 是业务拒绝，应为 40070，实际 %v", err)
	}
	if !strings.Contains(appErr.Message, "发件账号不存在") {
		t.Fatalf("应带上 Cloud Mail 的原话，实际 %q", appErr.Message)
	}
}

func TestSkymailWrongPasswordIsAuthError(t *testing.T) {
	_, server := newFakeSkymail(t)
	sender := newTestSkymailSender(server)
	config := skymailTestConfig(server.URL, "")
	config.Secrets[skymailKeyPassword] = "wrong"
	_, err := sender.Send(context.Background(), config, emailOutbound{To: "user@example.com", Subject: "s", HTML: "x"})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != 40071 {
		t.Fatalf("密码错误应为 40071，实际 %v", err)
	}
}

func TestSkymailResolvesSenderAccountByAddress(t *testing.T) {
	fake, server := newFakeSkymail(t)
	fake.accounts = []map[string]any{
		{"accountId": 7, "email": "sender@mail.test", "sort": 0, "isDel": 0},
		{"accountId": 9, "email": "Notice@Mail.test", "sort": 1, "isDel": 0},
	}
	sender := newTestSkymailSender(server)
	if _, err := sender.Send(context.Background(), skymailTestConfig(server.URL, "notice@mail.test"),
		emailOutbound{To: "user@example.com", Subject: "s", HTML: "x"}); err != nil {
		t.Fatalf("发信失败：%v", err)
	}
	if fake.sent[0].AccountID != 9 {
		t.Fatalf("发件地址应换算成邮箱账号 9（大小写不敏感），实际 %d", fake.sent[0].AccountID)
	}

	_, err := sender.Send(context.Background(), skymailTestConfig(server.URL, "nobody@mail.test"),
		emailOutbound{To: "user@example.com", Subject: "s", HTML: "x"})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != 40070 {
		t.Fatalf("不在名下的发件地址应报 40070，实际 %v", err)
	}
}

func TestSkymailSenderRejectsTooManyAttachments(t *testing.T) {
	_, server := newFakeSkymail(t)
	sender := newTestSkymailSender(server)
	out := emailOutbound{To: "user@example.com", Subject: "s", HTML: "x"}
	for i := 0; i < skymailMaxAttachments+1; i++ {
		out.Attachments = append(out.Attachments, emailAttachment{Filename: "a.txt", Content: []byte("a")})
	}
	if _, err := sender.Send(context.Background(), skymailTestConfig(server.URL, ""), out); err == nil {
		t.Fatal("超过 10 个附件应在本地拒绝，而不是丢掉多出来的")
	}
}

func TestSkymailSenderValidate(t *testing.T) {
	sender := newSkymailEmailSender(zap.NewNop())
	config := emaildomain.Config{Provider: emaildomain.ProviderSkymail, Settings: map[string]string{
		skymailKeyBaseURL: "https://mail.example.com", skymailKeyLoginEmail: "sender@example.com",
	}}
	if err := sender.Validate(config); err == nil {
		t.Fatal("缺密码应校验失败")
	}
	config.SecretsCipher = map[string]string{skymailKeyPassword: "cipher"}
	if err := sender.Validate(config); err != nil {
		t.Fatalf("仅有密文也应视为已配置：%v", err)
	}
	config.Settings[skymailKeyBaseURL] = "mail.example.com"
	if err := sender.Validate(config); err == nil {
		t.Fatal("不带协议的邮局地址应校验失败")
	}
}

func TestNormalizeSkymailBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://skymail.ink":           "https://skymail.ink",
		"https://skymail.ink/":          "https://skymail.ink",
		"https://skymail.ink/api":       "https://skymail.ink",
		"https://skymail.ink/api/":      "https://skymail.ink",
		"https://skymail.ink/API/login": "https://skymail.ink",
		" https://mail.example.com/x ":  "https://mail.example.com/x",
	}
	for input, want := range cases {
		if got := normalizeSkymailBaseURL(input); got != want {
			t.Errorf("%q → %q，期望 %q", input, got, want)
		}
	}
}

func TestSkymailTimeParsesUTC(t *testing.T) {
	var value struct {
		At skymailTime `json:"at"`
		Cc skymailText `json:"cc"`
	}
	if err := json.Unmarshal([]byte(`{"at":"2099-12-30 23:59:59","cc":["a@b.c"]}`), &value); err != nil {
		t.Fatal(err)
	}
	if value.At != "2099-12-30T23:59:59Z" {
		t.Fatalf("UTC 时间应转成 RFC 3339，实际 %q", value.At)
	}
	if value.Cc != `["a@b.c"]` {
		t.Fatalf("数组形态的抄送应保留原文，实际 %q", value.Cc)
	}
}

func TestSkymailDeliveryUpdateMapping(t *testing.T) {
	delivery := pgrepo.PendingProviderDelivery{ProviderMessageID: "42@mail.example.com"}
	cases := []struct {
		status    int
		want      string
		event     string
		delivered bool
		ok        bool
	}{
		{skymailStatusSent, "", "", false, false},
		{skymailStatusSaving, "", "", false, false},
		{skymailStatusDelivered, emaildomain.DeliveryStatusDelivered, "delivered", true, true},
		{skymailStatusBounced, emaildomain.DeliveryStatusBounced, "bounced", false, true},
		{skymailStatusComplaint, emaildomain.DeliveryStatusComplained, "complained", false, true},
		{skymailStatusNoRecip, emaildomain.DeliveryStatusBounced, "bounced", false, true},
		{skymailStatusFailed, emaildomain.DeliveryStatusFailed, "failed", false, true},
		{skymailStatusDelayed, "", "delayed", false, true},
	}
	for _, tc := range cases {
		update, ok := skymailDeliveryUpdate(5, delivery, SkymailEmail{EmailID: 42, Status: tc.status})
		if ok != tc.ok {
			t.Fatalf("状态 %d：ok=%v，期望 %v", tc.status, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if update.Status != tc.want || update.Event != tc.event || update.MarkDelivered != tc.delivered || update.AppID != 5 {
			t.Fatalf("状态 %d 映射不对：%+v", tc.status, update)
		}
	}
	// 延迟已经记过一次，就不再重复记
	delivery.LastEvent = "delayed"
	if _, ok := skymailDeliveryUpdate(5, delivery, SkymailEmail{EmailID: 42, Status: skymailStatusDelayed}); ok {
		t.Fatal("同一封信的延迟只记一条时间线")
	}
}

func TestBuildSkymailSendRequest(t *testing.T) {
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.7"))
	request, err := buildSkymailSendRequest(skymailSendParams{
		ReceiveEmail: []string{"a@example.com", "A@example.com", " b@example.com "},
		Subject:      "  你好 ", Content: "<p>hi</p>",
		Attachments: []skymailSendAttachmentParams{{Filename: "x.pdf", Content: "data:application/pdf;base64," + pdf}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(request.ReceiveEmail) != 2 || request.Subject != "你好" {
		t.Fatalf("收件人应去重、主题应去空白：%+v", request)
	}
	if request.Attachments[0].Content != pdf || request.Attachments[0].Size != 8 {
		t.Fatalf("data URL 前缀应裁掉：%+v", request.Attachments[0])
	}

	bad := []skymailSendParams{
		{Subject: "s", Content: "c"},
		{ReceiveEmail: []string{"not-an-email"}, Subject: "s", Content: "c"},
		{ReceiveEmail: []string{"a@example.com"}, Content: "c"},
		{ReceiveEmail: []string{"a@example.com"}, Subject: "s"},
		{ReceiveEmail: []string{"a@example.com"}, Subject: "s", Content: "c", SendType: "reply"},
		{ReceiveEmail: []string{"a@example.com"}, Subject: "s", Content: "c", SendType: "spam", EmailID: 1},
		{ReceiveEmail: []string{"a@example.com"}, Subject: "s", Content: "c",
			Attachments: []skymailSendAttachmentParams{{Filename: "x", Content: "%%%"}}},
	}
	for i, params := range bad {
		if _, err := buildSkymailSendRequest(params); err == nil {
			t.Errorf("第 %d 组非法参数应被拒绝：%+v", i, params)
		}
	}
	many := skymailSendParams{Subject: "s", Content: "c"}
	for i := 0; i <= skymailMaxRecipients; i++ {
		many.ReceiveEmail = append(many.ReceiveEmail, strings.Repeat("a", i+1)+"@example.com")
	}
	if _, err := buildSkymailSendRequest(many); err == nil {
		t.Fatal("超过 50 个收件人应被拒绝")
	}
}

func TestDecodeSkymailParamsRejectsUnknownFields(t *testing.T) {
	var p skymailUsersParams
	if err := decodeSkymailParams(json.RawMessage(`{"num":2,"emial":"x"}`), &p); err == nil {
		t.Fatal("拼错的字段名应被拒绝，而不是静默忽略")
	}
	if err := decodeSkymailParams(nil, &p); err != nil {
		t.Fatalf("空参数应当作 {}：%v", err)
	}
	if _, err := validSkymailIDs([]int64{1, 1, 2}, "邮件"); err != nil {
		t.Fatal(err)
	}
	if _, err := validSkymailIDs([]int64{0}, "邮件"); err == nil {
		t.Fatal("非正数 id 应被拒绝")
	}
}
