package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func bindNoticeForTest(t *testing.T, body string) AdminNoticeUpsertRequest {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/admin/apps/10000/notices/1", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	var req AdminNoticeUpsertRequest
	if !bindContentUpsert(ctx, &req, &req.StartTime, &req.EndTime) {
		t.Fatalf("bind failed: %s", recorder.Body.String())
	}
	return req
}

// 控制台清空时间输入框发的是 null：必须变成「清除」信号（零值时间），而不是「不修改」（nil）。
func TestBindContentUpsertTurnsExplicitNullIntoClear(t *testing.T) {
	req := bindNoticeForTest(t, `{"title":"维护","status":"published","startTime":null,"endTime":null}`)
	if req.StartTime == nil || !req.StartTime.IsZero() {
		t.Fatalf("startTime null should become zero time, got %v", req.StartTime)
	}
	if req.EndTime == nil || !req.EndTime.IsZero() {
		t.Fatalf("endTime null should become zero time, got %v", req.EndTime)
	}
	if req.Title == nil || *req.Title != "维护" {
		t.Fatalf("other fields must still bind, got %+v", req)
	}
}

func TestBindContentUpsertKeepsOmittedAndSetTimes(t *testing.T) {
	req := bindNoticeForTest(t, `{"status":"archived","endTime":"2026-10-08T00:00:00Z"}`)
	if req.StartTime != nil {
		t.Fatalf("omitted startTime must stay nil (no change), got %v", req.StartTime)
	}
	if req.EndTime == nil || req.EndTime.IsZero() || req.EndTime.UTC().Day() != 8 {
		t.Fatalf("explicit endTime must be kept, got %v", req.EndTime)
	}
}

func TestBindContentUpsertRejectsMalformedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/apps/10000/notices", strings.NewReader(`{"title":`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	var req AdminNoticeUpsertRequest
	if bindContentUpsert(ctx, &req, &req.StartTime, &req.EndTime) {
		t.Fatal("malformed body should fail")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}
