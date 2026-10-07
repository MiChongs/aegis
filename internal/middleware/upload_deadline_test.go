package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsUploadRequest(t *testing.T) {
	cases := []struct {
		method, contentType string
		want                bool
	}{
		{http.MethodPost, "multipart/form-data; boundary=x", true},
		{http.MethodPut, "application/octet-stream", true},
		{http.MethodPost, "application/json", false},
		{http.MethodGet, "multipart/form-data; boundary=x", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, "/", nil)
		req.Header.Set("Content-Type", tc.contentType)
		if got := isUploadRequest(req); got != tc.want {
			t.Errorf("%s %s: got %v, want %v", tc.method, tc.contentType, got, tc.want)
		}
	}
}
