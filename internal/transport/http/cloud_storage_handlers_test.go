package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 用户云存储的请求绑定：DELETE 的可选修订号必须能区分「没传」与「传了 0」，
// PUT 的 content 必须原样保留（字段顺序、数字写法），缺 content 必须被拒。
func TestCloudStorageRequestBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/x?ifRevision=3&permanent=true", nil)
	var q CloudItemDeleteQuery
	if err := c.ShouldBindQuery(&q); err != nil || q.IfRevision == nil || *q.IfRevision != 3 || !q.Permanent {
		t.Fatalf("query 绑定：%+v / %v", q, err)
	}
	c.Request = httptest.NewRequest(http.MethodDelete, "/x", nil)
	var empty CloudItemDeleteQuery
	if err := c.ShouldBindQuery(&empty); err != nil || empty.IfRevision != nil {
		t.Fatalf("空 query：%+v / %v", empty, err)
	}

	c.Request = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"content":{"b":1,"a":[1,2]},"ifRevision":0,"metadata":{"n":2}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var put CloudItemPutRequest
	if err := bind(c, &put); err != nil || string(put.Content) != `{"b":1,"a":[1,2]}` || put.IfRevision == nil || *put.IfRevision != 0 {
		t.Fatalf("PUT 绑定：%+v content=%s / %v", put, put.Content, err)
	}
	c.Request = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"encoding":"json"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var missing CloudItemPutRequest
	if err := bind(c, &missing); err == nil {
		t.Fatal("缺 content 应当被拒")
	}
}
