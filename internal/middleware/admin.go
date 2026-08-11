package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	admindomain "aegis/internal/domain/admin"
	platformdomain "aegis/internal/domain/platform"
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

func AdminAccess(adminService *service.AdminService, appService *service.AppService, governance *service.PlatformGovernanceService) gin.HandlerFunc {
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
		// 平台治理的 blockAdminWrite 执行点：被停运 / 封禁 / 归档的应用对**应用管理员**只读。
		// 平台级管理员（超管或持 platform:app:govern）必须放行 —— 否则谁都改不动，
		// 连解除治理本身都做不到。
		if err := enforceGovernanceAdminWrite(c, adminService, governance, access, appID); err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		c.Set("admin.session", access)
		c.Set("admin.token", token)
		c.Next()
	}
}

// enforceGovernanceAdminWrite 应用作用域的写操作在治理只读期的闸门。
func enforceGovernanceAdminWrite(
	c *gin.Context,
	adminService *service.AdminService,
	governance *service.PlatformGovernanceService,
	access *admindomain.AccessContext,
	appID *int64,
) error {
	if governance == nil || appID == nil || *appID <= 0 {
		return nil
	}
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	// 申诉是被治理方唯一的出口，绝不能被只读闸门挡住 ——
	// 挡住了就成了「停运的应用连喊冤都喊不了」。
	if strings.Contains(routePath(c), "/governance/appeals") {
		return nil
	}
	decision := governance.Decide(*appID, platformdomain.CapabilityAdminWrite)
	if decision.Allowed {
		return nil
	}
	if isPlatformGovernor(c, adminService, access) {
		return nil
	}
	return apperrors.New(40336, http.StatusForbidden, decision.Message)
}

// isPlatformGovernor 判定当前管理员是否属于平台级治理方。
//
// 只认全局作用域的 platform:app:govern —— 应用级角色即便被授了这个权限点，
// 也只是"能治理自己那个应用"，那等于自己解自己的封。
func isPlatformGovernor(c *gin.Context, adminService *service.AdminService, access *admindomain.AccessContext) bool {
	if access == nil {
		return false
	}
	if access.IsSuperAdmin {
		return true
	}
	if adminService == nil {
		return false
	}
	return adminService.Authorize(c.Request.Context(), access, service.PermissionPlatformAppGovern, nil) == nil
}

// RequireSuperAdmin 必须在 AdminAuth / AdminAccess 之后挂载使用。
// 校验当前会话属于超级管理员；不通过返回 403。
// 专用于平台级敏感资源（如 /api/admin/system/banners 的写操作），
// 不走 Casbin 权限点，防止因授权表漂移被误赋权。
func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session, ok := adminSession(c)
		if !ok || session == nil || !session.IsSuperAdmin {
			response.Error(c, http.StatusForbidden, 40301, "需要超级管理员权限")
			c.Abort()
			return
		}
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

// routePath 取注册路由模板，未匹配到路由时回落到请求路径。
func routePath(c *gin.Context) string {
	if path := c.FullPath(); path != "" {
		return path
	}
	if c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.Path
	}
	return ""
}

func resolveAdminPermission(c *gin.Context) (string, bool, error) {
	fullPath := routePath(c)
	method := c.Request.Method

	switch {
	case fullPath == "/api/admin/dashboard":
		return "", false, nil
	// ── 平台治理（全站作用域，appScoped 恒为 false）──
	//
	// 这一组接口的语义就是"跨应用"：带上 appid 反而会让 scopeMatches 认应用级授权，
	// 于是被治理应用自己的管理员就能解自己的封。因此**永远不按 appScoped 判定**。
	case fullPath == "/api/admin/platform/catalog":
		// 状态 / 能力目录是编译进二进制的静态表，且被治理方也要靠它读懂自己的处境
		return "", false, nil
	case strings.HasPrefix(fullPath, "/api/admin/platform/"):
		switch {
		case strings.HasSuffix(fullPath, "/revoke-sessions"):
			return service.PermissionPlatformAppDanger, false, nil
		case strings.Contains(fullPath, "/appeals/") && strings.HasSuffix(fullPath, "/review"):
			return service.PermissionPlatformAppealReview, false, nil
		case strings.HasPrefix(fullPath, "/api/admin/platform/storage-config"):
			if isCompatReadPath(fullPath, []string{"/list", "/detail"}) {
				return service.PermissionPlatformStorageRead, false, nil
			}
			return service.PermissionPlatformStorageWrite, false, nil
		case method == http.MethodGet:
			return service.PermissionPlatformAppRead, false, nil
		default:
			return service.PermissionPlatformAppGovern, false, nil
		}
	// 工单系统 —— 这里只做"能不能进模块"的粗粒度闸门，
	// 「能不能看/改这一条工单」由 TicketService 的 Scope + ActionSet 判定，
	// 因此不带任何权限点、但属于某处理组的管理员也必须放行进来。
	case strings.HasPrefix(fullPath, "/api/admin/tickets"):
		switch {
		// 元数据与我的待办：任意已登录管理员可读，否则工单页会整页空白
		case strings.HasSuffix(fullPath, "/metadata"), strings.HasSuffix(fullPath, "/workbench"):
			return "", false, nil
		// 配置类资源的写操作需要 ticket:manage
		case strings.Contains(fullPath, "/categories"),
			strings.Contains(fullPath, "/groups"),
			strings.Contains(fullPath, "/sla-policies"),
			strings.Contains(fullPath, "/quick-replies"):
			if method == http.MethodGet {
				return "", false, nil
			}
			return "ticket:manage", false, nil
		case strings.HasSuffix(fullPath, "/export"):
			return "ticket:export", false, nil
		default:
			// 列表/详情/回复/指派等：进模块即可，细粒度在 service 层
			return "", false, nil
		}
	// 统一通知出口 —— 渠道内含 IM 凭据，读要权限点，写由 RequireSuperAdmin 二次把关
	case strings.HasPrefix(fullPath, "/api/admin/notify"):
		if fullPath == "/api/admin/notify/catalog" {
			return "", false, nil
		}
		if strings.Contains(fullPath, "/deliveries") {
			if method == http.MethodGet {
				return "notify:delivery:read", false, nil
			}
			return "notify:channel:write", false, nil
		}
		if method == http.MethodGet {
			return "notify:channel:read", false, nil
		}
		return "notify:channel:write", false, nil
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
	case fullPath == "/api/app/password-policy/templates" ||
		fullPath == "/api/admin/apps/password-policy/templates" ||
		fullPath == "/api/admin/apps/signin-reward/templates":
		return "platform:app:read", false, nil
	// 第三方登录渠道模板：编译进二进制的静态目录，不含任何租户数据与凭据，
	// 任意已登录管理员均可读取（否则配置向导会因权限点缺失而空白）
	case fullPath == "/api/admin/oauth-providers/templates":
		return "", false, nil
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
		if isCompatReadPath(fullPath, []string{"/list", "/detail", "/deliveries"}) {
			return "email:read", true, nil
		}
		return "email:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/storage-config"):
		if isCompatReadPath(fullPath, []string{"/list", "/detail"}) {
			return "storage:read", true, nil
		}
		return "storage:write", true, nil
	case strings.HasPrefix(fullPath, "/api/admin/app/payment-config"):
		// /methods 返回的是**平台支持哪些支付渠道**（Provider.Describe() 的静态目录：
		// 渠道名、能力矩阵、配置字段 schema），编译进二进制，不含任何租户数据与凭据。
		// 与第三方登录渠道模板同一性质，因此任意已登录管理员均可读取。
		//
		// 按 appScoped 处理会被中间件以 40058「缺少有效的应用标识」拦掉（控制台不带 appid）；
		// 只改成 appScoped=false 也不够 —— scopeMatches 在 requestAppID 为 nil 时只认
		// 全局作用域的授权，应用级管理员会转而拿到 403。两种写法都会让渠道市场
		// 永远显示「平台支持 0 种支付方式」。
		if fullPath == "/api/admin/app/payment-config/methods" {
			return "", false, nil
		}
		if isCompatReadPath(fullPath, []string{
			"/list", "/detail", "/orders/list", "/orders/detail",
			"/refunds/list", "/refunds/order", "/refunds/refundable",
		}) {
			return "payment:read", true, nil
		}
		return "payment:write", true, nil
	case strings.HasPrefix(fullPath, "/api/app/workflow"):
		// 节点类型目录（静态目录）与引擎状态（Temporal 连通性）都与具体应用无关，
		// 也不含租户数据。控制台不带 appid 调用，按 appScoped 处理会被 40058 拦掉 ——
		// 工作流画布因此拿不到节点类型，节点面板整个是空的。
		// 与 /methods 同理不能只改 appScoped=false（应用级管理员会转而拿到 403）。
		if fullPath == "/api/app/workflow/node-types" || fullPath == "/api/app/workflow/engine/status" {
			return "", false, nil
		}
		if isCompatReadPath(fullPath, []string{"/list", "/detail", "/info", "/instances", "/instances/list", "/instance/detail", "/instances/info", "/tasks/todo", "/task/detail", "/task/history", "/templates", "/templates/list", "/validate", "/statistics", "/logs"}) {
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
		// 绑定记录是用户数据，按用户权限点判定；渠道配置本身按应用配置权限点判定
		case strings.Contains(fullPath, "/oauth-bindings"):
			if method == http.MethodGet {
				return "app:user:read", true, nil
			}
			return "app:user:write", true, nil
		case strings.Contains(fullPath, "/oauth-providers"):
			if method == http.MethodGet {
				return "app:read", true, nil
			}
			return "app:write", true, nil
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
		// 应用自己的治理视图：看自己被怎么了、以及提交申诉。
		// 只读用 app:read、申诉用 app:write，都留在应用作用域 ——
		// 应用管理员在这里改不了治理结论，改结论要走 /api/admin/platform/*。
		case strings.Contains(fullPath, "/governance"):
			if method == http.MethodGet {
				return "app:read", true, nil
			}
			return "app:write", true, nil
		case strings.Contains(fullPath, "/policy"),
			strings.Contains(fullPath, "/password-policy"),
			strings.Contains(fullPath, "/signin-reward"):
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
	// 1) 优先从路径参数 :appkey 解析（兼容 appKey 字符串与纯数字应用 ID，
	//    与 handler 层 resolveAppID 的解析语义保持一致）
	if appKey := strings.TrimSpace(c.Param("appkey")); appKey != "" {
		if appService != nil {
			app, err := appService.GetAppByKey(c.Request.Context(), appKey)
			if err == nil && app != nil {
				return &app.ID, nil
			}
			if appID, ok := parseOptionalInt64(appKey); ok {
				app, getErr := appService.GetApp(c.Request.Context(), appID)
				if getErr == nil && app != nil {
					return &app.ID, nil
				}
			}
			return nil, io.EOF
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
