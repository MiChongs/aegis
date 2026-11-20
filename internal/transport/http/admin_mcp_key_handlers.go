package httptransport

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/internal/authz"
	"aegis/internal/middleware"
	admindomain "aegis/internal/domain/admin"
	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// MCP 密钥（docs/mcp.md）。
//
// 两组接口：
//   - /api/admin/profile/mcp-keys/*：签发与管理，只接受控制台会话（中间件与 service 两道都拦 MCP 密钥）；
//   - /api/admin/mcp/*：给 MCP 服务端用的自述接口（我是谁、我能调哪些接口），接受密钥也接受会话。

type mcpKeyRequest struct {
	Name               string     `json:"name" form:"name"`
	Description        string     `json:"description" form:"description"`
	Permissions        []string   `json:"permissions" form:"permissions"`
	AppIDs             []int64    `json:"appIds" form:"appIds"`
	ReadOnly           *bool      `json:"readOnly" form:"readOnly"`
	AllowIPs           []string   `json:"allowIps" form:"allowIps"`
	RateLimitPerMinute *int       `json:"rateLimitPerMinute" form:"rateLimitPerMinute"`
	ExpiresAt          *time.Time `json:"expiresAt" form:"expiresAt"`
	// ExpiresInDays 与 ExpiresAt 二选一。签发时两者都不给为永不过期；
	// 修改时两者都不给保持原过期时间，要改成永不过期须显式带 neverExpires。
	ExpiresInDays *int `json:"expiresInDays" form:"expiresInDays"`
	NeverExpires  bool `json:"neverExpires" form:"neverExpires"`
}

type mcpKeyListQuery struct {
	// Scope 为 all 时（仅超管）列出全部管理员的密钥。
	Scope string `form:"scope"`
}

type mcpEndpointQuery struct {
	Q       string `form:"q"`
	Group   string `form:"group"`
	Method  string `form:"method"`
	Allowed string `form:"allowed"`
	Limit   int    `form:"limit"`
}

func (req mcpKeyRequest) toInput(current *admindomain.MCPKey) admindomain.MCPKeyInput {
	input := admindomain.MCPKeyInput{
		Name:               req.Name,
		Description:        req.Description,
		Permissions:        req.Permissions,
		AppIDs:             req.AppIDs,
		ReadOnly:           true,
		AllowIPs:           req.AllowIPs,
		RateLimitPerMinute: 120,
		ExpiresAt:          req.ExpiresAt,
	}
	if current != nil {
		input.ReadOnly = current.ReadOnly
		input.RateLimitPerMinute = current.RateLimitPerMinute
		if req.ExpiresAt == nil {
			input.ExpiresAt = current.ExpiresAt
		}
	}
	if req.ReadOnly != nil {
		input.ReadOnly = *req.ReadOnly
	}
	if req.RateLimitPerMinute != nil {
		input.RateLimitPerMinute = *req.RateLimitPerMinute
	}
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		at := time.Now().Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)
		input.ExpiresAt = &at
	}
	if req.NeverExpires {
		input.ExpiresAt = nil
	}
	return input
}

func mcpKeyIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("keyId")), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40169, "keyId 无效")
		return 0, false
	}
	return id, true
}

func (h *Handler) mustAdminAccess(c *gin.Context) (*admindomain.AccessContext, bool) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return nil, false
	}
	return access, true
}

// AdminListMCPKeys 我的 MCP 密钥；超管带 scope=all 看全部。
func (h *Handler) AdminListMCPKeys(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	var query mcpKeyListQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.admin.ListMCPKeys(c.Request.Context(), access, query.Scope == "all")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminMCPKeyCatalog 签发表单用的权限目录与当前管理员的生效权限。
func (h *Handler) AdminMCPKeyCatalog(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	response.Success(c, http.StatusOK, "ok", h.admin.MCPPermissionCatalog(c.Request.Context(), access))
}

func (h *Handler) AdminGetMCPKey(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	id, ok := mcpKeyIDParam(c)
	if !ok {
		return
	}
	item, err := h.admin.GetMCPKey(c.Request.Context(), access, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", item)
}

func (h *Handler) AdminCreateMCPKey(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	var req mcpKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.admin.CreateMCPKey(c.Request.Context(), access, req.toInput(nil))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "MCP 密钥已签发，明文只显示这一次，请立即保存", item)
}

func (h *Handler) AdminUpdateMCPKey(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	id, ok := mcpKeyIDParam(c)
	if !ok {
		return
	}
	var req mcpKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	current, err := h.admin.GetMCPKey(c.Request.Context(), access, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	item, err := h.admin.UpdateMCPKey(c.Request.Context(), access, id, req.toInput(current))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "MCP 密钥已更新", item)
}

func (h *Handler) AdminRotateMCPKey(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	id, ok := mcpKeyIDParam(c)
	if !ok {
		return
	}
	item, err := h.admin.RotateMCPKey(c.Request.Context(), access, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已轮换，旧密钥立即失效；新明文只显示这一次", item)
}

func (h *Handler) AdminEnableMCPKey(c *gin.Context)  { h.setMCPKeyEnabled(c, true) }
func (h *Handler) AdminDisableMCPKey(c *gin.Context) { h.setMCPKeyEnabled(c, false) }

func (h *Handler) setMCPKeyEnabled(c *gin.Context, enabled bool) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	id, ok := mcpKeyIDParam(c)
	if !ok {
		return
	}
	item, err := h.admin.SetMCPKeyEnabled(c.Request.Context(), access, id, enabled)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "MCP 密钥已停用"
	if enabled {
		message = "MCP 密钥已启用"
	}
	response.Success(c, http.StatusOK, message, item)
}

func (h *Handler) AdminRevokeMCPKey(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	id, ok := mcpKeyIDParam(c)
	if !ok {
		return
	}
	if err := h.admin.RevokeMCPKey(c.Request.Context(), access, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "MCP 密钥已撤销", gin.H{"revoked": true})
}

// ── MCP 服务端自述 ──

type mcpAppSummary struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	AppKey string `json:"appKey,omitempty"`
	Status bool   `json:"status"`
}

// AdminMCPWhoAmI 当前凭证代表谁、能做什么、能碰哪些应用（已按密钥范围收窄）。
func (h *Handler) AdminMCPWhoAmI(c *gin.Context) {
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	apps, err := h.visibleAdminApps(c, access)
	if err != nil {
		h.writeError(c, err)
		return
	}
	summaries := make([]mcpAppSummary, 0, len(apps))
	for _, app := range apps {
		summaries = append(summaries, mcpAppSummary{ID: app.ID, Name: app.Name, AppKey: app.AppKey, Status: app.Status})
	}
	payload := gin.H{
		"admin": gin.H{
			"id":           access.AdminID,
			"account":      access.Account,
			"displayName":  access.DisplayName,
			"isSuperAdmin": access.IsSuperAdmin,
		},
		"authType":    "session",
		"key":         nil,
		"permissions": h.admin.EffectivePermissions(c.Request.Context(), access),
		"apps":        summaries,
	}
	if access.MCPKey != nil {
		payload["authType"] = "mcp_key"
		payload["key"] = gin.H{
			"id":          access.MCPKey.KeyID,
			"name":        access.MCPKey.Name,
			"keyPrefix":   access.MCPKey.KeyPrefix,
			"permissions": access.MCPKey.Permissions,
			"appIds":      access.MCPKey.AppIDs,
			"readOnly":    access.MCPKey.ReadOnly,
			"allowsAll":   access.MCPKey.AllowsAll(),
			"expiresAt":   nullableTime(access.ExpiresAt),
		}
	}
	response.Success(c, http.StatusOK, "ok", payload)
}

// visibleAdminApps 这个凭证看得见的应用：角色分配过滤后再按密钥的应用范围过滤。
func (h *Handler) visibleAdminApps(c *gin.Context, access *admindomain.AccessContext) ([]appdomain.App, error) {
	items, err := h.app.ListApps(c.Request.Context())
	if err != nil {
		return nil, err
	}
	if !access.IsSuperAdmin {
		items = filterAppsByAssignments(items, access.Assignments)
	}
	return filterAppsByMCPKey(items, access.MCPKey), nil
}

func filterAppsByMCPKey(apps []appdomain.App, key *admindomain.MCPKeyScope) []appdomain.App {
	if !key.RestrictsApps() {
		return apps
	}
	filtered := make([]appdomain.App, 0, len(key.AppIDs))
	for _, app := range apps {
		if key.AllowsApp(app.ID) {
			filtered = append(filtered, app)
		}
	}
	return filtered
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

// mcpEndpointGuardedPrefixes / mcpEndpointExemptPrefixes 与 TestEveryAdminRouteHasAuthzRule 同一套口径：
// 前者是走权限规则表的管理端命名空间，后者是「管理员自己」那组（不接受 MCP 密钥）。
var (
	mcpEndpointGuardedPrefixes = []string{"/api/admin/", "/api/app/password-policy", "/api/app/points", "/api/app/workflow"}
	mcpEndpointExemptPrefixes  = []string{"/api/admin/auth/", "/api/admin/captcha/", "/api/admin/profile", "/api/admin/avatar", "/api/admin/notifications", "/api/admin/mcp/"}
)

// mcpEndpointsHandler 管理端接口目录（GET /api/admin/mcp/endpoints），逐条标出当前凭证能不能调、为什么不能。
//
// 判定口径与中间件一致（只读、无权限点模块、应用范围、密钥权限范围、管理员本人权限），
// 但只是预判：超管专属接口与 service 层的细粒度判定这里看不见，以实际调用结果为准。
//
// 处理器是具名类型上的方法而不是闭包：闭包的名字（funcN）由编译器按内联结果编号，
// 换一个 Go 版本就会变，路由快照与接口目录里显示的处理器名会跟着漂。
func (h *Handler) mcpEndpointsHandler(router *gin.Engine) gin.HandlerFunc {
	return mcpEndpointCatalog{h: h, router: router}.AdminMCPEndpoints
}

// mcpEndpointCatalog 接口目录要读的路由表。路由表在 NewRouter 返回前才完整，所以只存引用、请求时才读
type mcpEndpointCatalog struct {
	h      *Handler
	router *gin.Engine
}

// AdminMCPEndpoints GET /api/admin/mcp/endpoints
func (m mcpEndpointCatalog) AdminMCPEndpoints(c *gin.Context) {
	h, router := m.h, m.router
	access, ok := h.mustAdminAccess(c)
	if !ok {
		return
	}
	var query mcpEndpointQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	snapshot := h.admin.EffectivePermissions(c.Request.Context(), access)
	keywords := strings.Fields(strings.ToLower(query.Q))
	method := strings.ToUpper(strings.TrimSpace(query.Method))
	group := strings.TrimSpace(query.Group)
	onlyAllowed := query.Allowed == "1" || strings.EqualFold(query.Allowed, "true")

	items := make([]admindomain.MCPEndpoint, 0, 256)
	groups := map[string]int{}
	for _, route := range router.Routes() {
		if route.Method == http.MethodHead || route.Method == http.MethodOptions {
			continue
		}
		if !mcpHasAnyPrefix(route.Path, mcpEndpointGuardedPrefixes) || mcpHasAnyPrefix(route.Path, mcpEndpointExemptPrefixes) {
			continue
		}
		decision, registered := authz.ResolveRoute(route.Method, route.Path)
		if !registered {
			continue
		}
		item := admindomain.MCPEndpoint{
			Method:     route.Method,
			Path:       route.Path,
			Handler:    shortHandlerName(route.Handler),
			Group:      matchRouteGroup(route.Path).Title,
			Permission: decision.Permission,
			AppScoped:  decision.AppScoped(),
		}
		if decision.Permission != "" {
			item.PermissionName = authz.PermissionName(decision.Permission)
		}
		item.Allowed, item.Reason = mcpEndpointVerdict(access, snapshot, route.Method, route.Path, decision)
		if onlyAllowed && !item.Allowed {
			continue
		}
		if method != "" && item.Method != method {
			continue
		}
		if group != "" && item.Group != group {
			continue
		}
		if len(keywords) > 0 {
			haystack := strings.ToLower(item.Method + " " + item.Path + " " + item.Handler + " " + item.Group + " " + item.Permission + " " + item.PermissionName)
			matched := true
			for _, keyword := range keywords {
				if !strings.Contains(haystack, keyword) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}
		groups[item.Group]++
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Method < items[j].Method
	})
	total := len(items)
	if query.Limit > 0 && query.Limit < len(items) {
		items = items[:query.Limit]
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items, "total": total, "groups": groups})
}

// mcpEndpointVerdict 预判一条接口对当前凭证是否可调。
func mcpEndpointVerdict(access *admindomain.AccessContext, snapshot admindomain.PermissionSnapshot, method, path string, decision authz.RouteDecision) (bool, string) {
	write := method != http.MethodGet
	key := access.MCPKey
	if key != nil {
		if write && key.ReadOnly && !middleware.IsReadPermission(decision.Permission) {
			return false, "只读密钥不能调用写接口"
		}
		if decision.Permission == "" {
			for _, item := range []struct{ prefix, permission string }{
				{"/api/admin/tickets", "ticket:read"},
				{"/api/admin/system/organizations", "org:dept:read"},
			} {
				if strings.HasPrefix(path, item.prefix) {
					if !key.AllowsPermission(item.permission) {
						return false, "密钥范围不包含 " + item.permission
					}
					return true, "细粒度权限由服务端逐项判定"
				}
			}
			if write && !key.AllowsAll() {
				return false, "无权限点的写接口需要「全部权限」密钥"
			}
			return true, ""
		}
		if !key.AllowsPermission(decision.Permission) {
			return false, "密钥范围不包含 " + decision.Permission
		}
		if write && !decision.AppScoped() && key.RestrictsApps() {
			return false, "限定应用的密钥不能调用平台级写接口"
		}
	}
	if decision.Permission == "" || snapshot.All {
		return true, ""
	}
	if containsString(snapshot.Global, decision.Permission) {
		return true, ""
	}
	if decision.AppScoped() {
		var apps []string
		for appID, permissions := range snapshot.Apps {
			if containsString(permissions, decision.Permission) {
				apps = append(apps, appID)
			}
		}
		if len(apps) > 0 {
			sort.Strings(apps)
			return true, "仅限应用 " + strings.Join(apps, ", ")
		}
	}
	return false, "管理员本人没有 " + decision.Permission
}

func mcpHasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
