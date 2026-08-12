package httptransport

// 传输层共用的小工具：分页归一化、会话取值、CSV 写出。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"
	"strings"

	authdomain "aegis/internal/domain/auth"
	pointdomain "aegis/internal/domain/points"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) writeTransactions(c *gin.Context, loader func(session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error)) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query PaginationQuery
	_ = bind(c, &query)
	page := normalizePage(query.Page)
	limit := normalizeLimit(query.Limit)
	items, total, err := loader(session, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"items":      items,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": calcTotalPages(total, limit),
	})
}

func authSession(c *gin.Context) (*authdomain.Session, bool) {
	sessionValue, ok := c.Get("auth.session")
	if !ok {
		return nil, false
	}
	session, _ := sessionValue.(*authdomain.Session)
	if session == nil {
		return nil, false
	}
	return session, true
}

func normalizePage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func maybeString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func maybeInt64(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func pickPositive(primary int, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}

func calcPages(total int64, limit int) int {
	if limit <= 0 || total <= 0 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}

func calcTotalPages(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}
