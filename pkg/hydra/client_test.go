package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNextPageToken(t *testing.T) {
	h := http.Header{}
	h.Add("Link", `</admin/clients?page_size=1&page_token=abc>; rel="first"`)
	h.Add("Link", `</admin/clients?page_size=1&page_token=xyz%3D>; rel="next"`)
	if got := nextPageToken(h); got != "xyz=" {
		t.Fatalf("got %q", got)
	}
	if got := nextPageToken(http.Header{}); got != "" {
		t.Fatalf("没有 Link 时应为空，got %q", got)
	}
}

func TestErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/oauth2/auth/requests/login":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Not Found", "error_description": "Unable to locate the requested resource"})
		case "/admin/oauth2/auth/requests/login/accept":
			if r.Method != http.MethodPut || r.URL.Query().Get("login_challenge") != "c1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var body AcceptLogin
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(Redirect{RedirectTo: "https://hydra/next?sub=" + body.Subject})
		}
	}))
	defer srv.Close()
	c := New(srv.URL, nil)
	if _, err := c.GetLoginRequest(context.Background(), "missing"); !IsNotFound(err) {
		t.Fatalf("应识别为 404：%v", err)
	}
	redirect, err := c.AcceptLoginRequest(context.Background(), "c1", AcceptLogin{Subject: "42"})
	if err != nil || redirect != "https://hydra/next?sub=42" {
		t.Fatalf("redirect=%q err=%v", redirect, err)
	}
	if _, err := c.GetLoginRequest(context.Background(), " "); err == nil {
		t.Fatal("空 challenge 不应发请求")
	}
}
