package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	admindomain "aegis/internal/domain/admin"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func AdminAuth(adminService *service.AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := adminBearerToken(c)
		if token == "" {
			response.Error(c, http.StatusUnauthorized, 40110, "管理员令牌无效")
			c.Abort()
			return
		}
		access, err := adminService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		c.Set("admin.session", access)
		c.Set("admin.token", token)
		c.Next()
	}
}

func AdminAccess(adminService *service.AdminService, appService *service.AppService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := adminBearerToken(c)
		if token == "" {
			response.Error(c, http.StatusUnauthorized, 40110, "管理员令牌无效")
			c.Abort()
			return
		}
		access, err := adminService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		permission, appScoped, err := resolveAdminPermission(c)
		if err != nil {
			response.Error(c, http.StatusForbidden, 40312, "当前管理员无权执行此操作")
			c.Abort()
			return
		}
		var appID *int64
		if appScoped {
			appID, err = extractAdminAppID(c, appService)
			if err != nil {
				response.Error(c, http.StatusBadRequest, 40058, "缺少有效的应用标识")
				c.Abort()
				return
			}
		}
		if err := adminService.Authorize(c.Request.Context(), access, permission, appID); err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		c.Set("admin.session", access)
		c.Set("admin.token", token)
		c.Next()
	}
}

func AdminToken(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.GetHeader("X-Admin-Token"))
		if token == "" {
			token = bearerToken(c.GetHeader("Authorization"))
		}
		if strings.TrimSpace(expected) == "" || subtleCompare(token, expected) == false {
			response.Error(c, http.StatusUnauthorized, 40110, "管理员令牌无效")
			c.Abort()
			return
		}
		c.Next()
	}
}

func adminSession(c *gin.Context) (*admindomain.AccessContext, bool) {
	value, ok := c.Get("admin.session")
	if !ok {
		return nil, false
	}
	session, _ := value.(*admindomain.AccessContext)
	return session, session != nil
}

func adminBearerToken(c *gin.Context) string {
	token := strings.TrimSpace(c.GetHeader("X-Admin-Token"))
	if token != "" {
		return token
	}
	return bearerToken(c.GetHeader("Authorization"))
}

func writeAdminError(c *gin.Context, err error) {
	if appErr, ok := err.(*apperrors.AppError); ok {
		response.Error(c, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	response.Error(c, http.StatusForbidden, 40312, "当前管理员无权执行此操作")
}

func resolveAdminPermission(c *gin.Context) (string, bool, error) {
	fullPath := c.FullPath()
	if fullPath == "" {
		fullPath = c.Request.URL.Path
	}
	method := c.Request.Method

	switch {
	case fullPath == "/api/admin/dashboard":
		return "", false, nil
	// 组织 — GET 所有管理员可读，写操作需权限
	case strings.HasPrefix(fullPath, "/api/admin/system/organizations"):
		if method == http.MethodGet {
			return "", false, nil
		}
		if method == http.MethodPost {
			return "org:create", false, nil
		}
		return "org:write", false, nil
	// 部门 — GET 所有管理员可读，写操作需权限
	case strings.HasPrefix(fullPath, "/api/admin/system/departments"):
		if method == http.MethodGet {
			return "", false, nil
		}
		if strings.Contains(fullPath, "/invite") || strings.Contains(fullPath, "/batch-invite") {
			return "org:member:invite", false, nil
		}
		if strings.Contains(fullPath, "/members") {
			if method == http.MethodGet {
				return "", false, nil
			}
			return "org:member:write", false, nil
		}
		return "org:dept:write", false, nil
	// 邀请 — 查看/接受/拒绝自己的邀请对所有管理员开放
	case strings.HasPrefix(fullPath, "/api/admin/system/invitations"):
		return "", false, nil
	// 岗位
	case strings.HasPrefix(fullPath, "/api/admin/system/positions"):
		if method == http.MethodGet {
			return "org:dept:read", false, nil
		}
		return "org:write", false, nil
	// 管理员部门查询
	case strings.Contains(fullPath, "/departments") && strings.HasPrefix(fullPath, "/api/admin/system/admins/"):
		return "org:dept:read", false, nil
	case strings.HasPrefix(fullPath, "/api/admin/system/"):
		return "system:admin:manage", false, nil
	case fullPath == "/api/admin/user-settings/stats" || fullPath == "/api/admin/user-settings/user" || fullPath == "/api/admin/user-settings/check-integrity":
		return "system:user_setting:read", false, nil
	case strings.HasPrefix(fullPath, "/api/admin/user-settings/"):
		return "system:user_setting:write", false, nil
	case fullPath == "/api/app/password-policy/templates" || fullPath == "/api/admin/apps/password-policy/templates":
		return "platform:app:read", false, nil
	case strings.HasPrefix(fullPath, "/api/app/password-policy"):
		if method == http.MethodGet || strings.Contains(fullPath, "/get") || strings.Contains(fullPath, "/templates") {
			return "app:read", true, nil
		}
		return "app:write", true, nil
	case strings.HasPrefix(fullPath, "/api/app/points"):
		if strings.Contains(fullPath, "/stats") {
			return "points:read", true, nil
		}
		return "points:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/version"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail", "/stats", "/channel/list", "/channel/detail", "/channel/users", "/channel/preview-match"}) {
			return "version:read", true, nil
		}
		return "version:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/site"):
		if isCompatReadPath(fullPath, []string{"/audit-list", "/list", "/detail", "/user-sites", "/audit-stats"}) {
			return "site:read", true, nil
		}
		if strings.Contains(fullPath, "/audit") {
			return "site:audit", true, nil
		}
		return "site:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/role-application"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail", "/statistics"}) {
			return "role_application:read", true, nil
		}
		return "role_application:review", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/email-config"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail"}) {
			return "email:read", true, nil
		}
		return "email:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/platform/storage-config"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail"}) {
			return "platform:storage:read", false, nil
		}
		return "platform:storage:write", false, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/storage-config"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail"}) {
			return "storage:read", true, nil
		}
		return "storage:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/payment-config"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail"}) {
			return "payment:read", true, nil
		}
		return "payment:write", true, nil
	case strings.HasPrefix(fullPath, "/api/app/workflow"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail", "/info", "/instances", "/instances/list", "/instance/detail", "/instances/info", "/tasks/todo", "/task/detail", "/task/history", "/templates", "/templates/list", "/validate", "/node-types", "/statistics", "/logs", "/engine/status"}) {
			return "workflow:read", true, nil
		}
		return "workflow:write", true, nil
	case fullPath == "/api/admin/apps":
		if method == http.MethodGet {
			return "app:read", false, nil
		}
		return "app:write", false, nil
	case strings.HasPrefix(fullPath, "/api/admin/apps/:appkey"):
		switch {
		case strings.Contains(fullPath, "/stats"):
			return "app:read", true, nil
		case strings.Contains(fullPath, "/audits/"):
			if strings.Contains(fullPath, "/login") {
				return "audit:login:read", true, nil
			}
			return "audit:session:read", true, nil
		case strings.Contains(fullPath, "/users"):
			if method == http.MethodGet {
				return "app:user:read", true, nil
			}
			return "app:user:write", true, nil
		case strings.Contains(fullPath, "/notifications"):
			if method == http.MethodGet {
				return "app:notification:read", true, nil
			}
			return "app:notification:write", true, nil
		case strings.Contains(fullPath, "/banners"):
			if method == http.MethodGet {
				return "content:banner:read", true, nil
			}
			return "content:banner:write", true, nil
		case strings.Contains(fullPath, "/notices"):
			if method == http.MethodGet {
				return "content:notice:read", true, nil
			}
			return "content:notice:write", true, nil
		case strings.Contains(fullPath, "/policy"), strings.Contains(fullPath, "/password-policy"):
			if method == http.MethodGet {
				return "app:read", true, nil
			}
			return "app:write", true, nil
		default:
			if method == http.MethodGet {
				return "app:read", true, nil
			}
			return "app:write", true, nil
		}
	default:
		return "", false, io.EOF
	}
}

func extractAdminAppID(c *gin.Context, appService *service.AppService) (*int64, error) {
	// 1) 优先从路径参数 :appkey 解析
	if appKey := strings.TrimSpace(c.Param("appkey")); appKey != "" {
		if appService != nil {
			app, err := appService.GetAppByKey(c.Request.Context(), appKey)
			if err != nil || app == nil {
				return nil, io.EOF
			}
			return &app.ID, nil
		}
	}

	// 2) 兼容 query/form/body 中的数字 appid（遗留 API）
	for _, value := range []string{c.Query("appid"), c.PostForm("appid"), c.PostForm("appId")} {
		if appID, ok := parseOptionalInt64(value); ok {
			return &appID, nil
		}
	}
	if c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, io.EOF
	}
	contentType := strings.ToLower(c.ContentType())
	if !strings.Contains(contentType, "json") {
		return nil, io.EOF
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		return nil, io.EOF
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	for _, key := range []string{"appid", "appId"} {
		if value, ok := payload[key]; ok {
			switch typed := value.(type) {
			case float64:
				id := int64(typed)
				if id > 0 {
					return &id, nil
				}
			case string:
				if id, ok := parseOptionalInt64(typed); ok {
					return &id, nil
				}
			}
		}
	}
	return nil, io.EOF
}

func parseOptionalInt64(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func isCompatReadPath(path string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func subtleCompare(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	result := 1
	for i := range left {
		if left[i] != right[i] {
			result = 0
		}
	}
	return result == 1
}
