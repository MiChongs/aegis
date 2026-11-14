package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	platformdomain "aegis/internal/domain/platform"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func AdminAuth(adminService *service.AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := adminBearerToken(c)
		if token == "" {
			response.Error(c, http.StatusUnauthorized, 40110, "管理员令牌无效")
			c.Abort()
			return
		}
		// 只挂 AdminAuth 的路由是「管理员自己」的那一组（资料、密码、两步验证、MCP 密钥管理……）。
		// MCP 密钥只能进白名单里的几条只读路由，其余一律要控制台会话。
		if isMCPKeyToken(token) && !mcpKeySessionRouteAllowed(c) {
			response.Error(c, http.StatusForbidden, 40167, "该接口仅限控制台登录会话调用，不接受 MCP 密钥")
			c.Abort()
			return
		}
		access, err := authenticateAdmin(c, adminService, token)
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
		access, err := authenticateAdmin(c, adminService, token)
		if err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		permission, appScoped, err := resolveAdminPermission(c)
		if err != nil {
			// 路由没登记在权限表里 —— 默认拒绝，但要说清是"没登记"而不是"没权限"。
			// 混用同一句文案时，一条漏登记的新路由会伪装成一次权限配置问题，
			// 于是排查方向从"补一行权限表"歪到"给这个人加授权"。
			response.Error(c, http.StatusForbidden, 40315,
				"该管理端接口尚未登记权限规则，已按默认拒绝处理（请在 resolveAdminPermission 中补登记）")
			c.Abort()
			return
		}
		var appID *int64
		if appScoped {
			appID, err = extractAdminAppID(c, appService)
			if errors.Is(err, errAppIDConflict) {
				response.Error(c, http.StatusBadRequest, 40058,
					"请求中的 appid 不一致：查询串、表单与请求体里的应用标识必须相同")
				c.Abort()
				return
			}
			if err != nil {
				response.Error(c, http.StatusBadRequest, 40058,
					"缺少有效的应用标识：该接口按应用作用域鉴权，请在路径、查询串或请求体中携带 appid")
				c.Abort()
				return
			}
			// 处理函数用这个值，保证改的就是鉴权通过的那个应用
			c.Set(AdminScopeAppIDKey, *appID)
		}
		if err := enforceMCPKeyRoute(c, access, permission, appScoped); err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		if err := adminService.Authorize(c.Request.Context(), access, permission, appID); err != nil {
			writeAdminError(c, err)
			c.Abort()
			return
		}
		// 记下这次请求已经通过了哪个具体权限点：处理函数里那些「仅超管」的二次把关
		// （RequireSuperAdmin / requireSuperAdminSession）据此认可细分授权，见 AuthorizedPermission。
		if permission != "" {
			c.Set(AdminAuthorizedPermissionKey, permission)
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
	return adminService.Authorize(c.Request.Context(), access, authz.PermPlatformAppGovern, nil) == nil
}

// AdminAuthorizedPermissionKey AdminAccess 判定通过的权限点（gin 上下文键）。
const AdminAuthorizedPermissionKey = "admin.authorizedPermission"

// AuthorizedPermission 本次请求经 AdminAccess 判定通过的具体权限点；没有（只挂了 AdminAuth，
// 或路由不要求权限点）时返回空串。
//
// 权限细分之前，平台运维类接口只挂一个粗粒度的 system:admin:manage，处理函数里再叠一道
// 「必须是超管」。细分之后这道二次把关改为：**已经按某个具体权限点放行的请求认可该授权**，
// 否则（未经权限点判定的接口）仍然要求超管。于是被授予「出海网关：查看」的人能打开网关页，
// 而一条只挂了 AdminAuth 的接口不会因此对所有管理员敞开。
func AuthorizedPermission(c *gin.Context) string {
	value, _ := c.Get(AdminAuthorizedPermissionKey)
	permission, _ := value.(string)
	return permission
}

// RequireSuperAdmin 必须在 AdminAuth / AdminAccess 之后挂载使用。
// 校验当前会话属于超级管理员，或本次请求已经按某个具体权限点通过了 AdminAccess；不通过返回 403。
func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session, ok := adminSession(c)
		if ok && session != nil && !session.IsSuperAdmin && AuthorizedPermission(c) != "" {
			c.Next()
			return
		}
		if !ok || session == nil || !session.IsSuperAdmin {
			response.Error(c, http.StatusForbidden, 40301, "需要超级管理员权限")
			c.Abort()
			return
		}
		// 超管签发的密钥只有授予「全部权限」才能进超管专属接口：
		// 这些接口不走权限点，密钥的权限范围在这里没有别的落点。
		if session.MCPKey != nil && !session.MCPKey.AllowsAll() {
			response.Error(c, http.StatusForbidden, 40164,
				"该接口仅限超级管理员，MCP 密钥需授予「全部权限」才能调用")
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
	if token := bearerToken(c.GetHeader("Authorization")); token != "" {
		return token
	}
	// WebSocket 升级请求：浏览器无法给握手加自定义头，令牌按平台既有约定
	// 借 Sec-WebSocket-Protocol 携带（"aegis.jwt.<token>"，与 /api/ws 同一套，
	// 见 realtime_service.go）。仅升级请求解析，普通请求不看这个头。
	if websocket.IsWebSocketUpgrade(c.Request) {
		const prefix = "aegis.jwt."
		for _, protocol := range websocket.Subprotocols(c.Request) {
			if strings.HasPrefix(protocol, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(protocol, prefix))
			}
		}
	}
	return ""
}

// writeAdminError 把服务层的业务错误原样交给客户端。
//
// 只有非 AppError（也就是没人给出业务判定的意外错误）才落到最后那句兜底 ——
// AdminService.Authorize 的拒绝文案里带着缺失的权限点与作用域，
// 在这里改写成一句通用文案等于把刚算出来的信息丢掉。
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

// resolveAdminPermission 解析当前路由需要的权限点与作用域。
//
// 规则表在 internal/authz —— 与权限词汇、角色定义、判定引擎同一个包。
// 这里曾经是 250 行嵌套 switch，用 strings.HasPrefix / strings.Contains /
// 后缀匹配拼判定，三类问题都不会在编译期暴露：
// 不锚定的 Contains 会误伤（任何含 "/users" 的路径都算用户接口）、
// 后缀匹配会把以 /list 结尾的写接口判成读、分支顺序即优先级但顺序藏在嵌套里。
//
// 现在只剩一次查表；941 条真实路由的判定结果由
// internal/authz/testdata/route_permissions.json 逐条钉住。
func resolveAdminPermission(c *gin.Context) (string, bool, error) {
	decision, ok := authz.ResolveRoute(c.Request.Method, routePath(c))
	if !ok {
		return "", false, errRouteNotRegistered
	}
	return decision.Permission, decision.AppScoped(), nil
}

// AdminScopeAppIDKey 应用作用域鉴权通过的应用 ID（int64）在 gin.Context 里的键。
const AdminScopeAppIDKey = "admin.scopeAppID"

// errRouteNotRegistered 路由没登记在权限规则表里 —— 默认拒绝。
var errRouteNotRegistered = errors.New("该管理端路由未登记权限规则")

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

	// 2) 兼容 query/form/body 中的数字 appid（遗留 API）。
	//
	// 必须把所有来源都看一遍：鉴权按这里取到的应用判定，而处理函数通常只从请求体绑定。
	// 旧实现取到第一个就返回，于是「查询串写有权限的应用 A、请求体写应用 B」能用 A 的
	// 权限改 B 的数据。现在各来源出现不同的值一律拒绝。
	var candidates []int64
	for _, value := range []string{c.Query("appid"), c.Query("appId"), c.PostForm("appid"), c.PostForm("appId")} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		appID, ok := parseOptionalInt64(value)
		if !ok {
			return nil, errAppIDInvalid
		}
		candidates = append(candidates, appID)
	}
	bodyIDs, err := jsonBodyAppIDs(c)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, bodyIDs...)
	if len(candidates) == 0 {
		return nil, io.EOF
	}
	for _, id := range candidates[1:] {
		if id != candidates[0] {
			return nil, errAppIDConflict
		}
	}
	return &candidates[0], nil
}

var (
	// errAppIDConflict 同一请求的不同位置给出了不同的 appid。
	errAppIDConflict = errors.New("appid conflict")
	// errAppIDInvalid appid 不是正整数。
	errAppIDInvalid = errors.New("appid invalid")
)

// jsonBodyAppIDs 读 JSON 请求体顶层的 appid / appId（不消耗请求体）。
// 用 UseNumber 解码：float64 在 2^53 以上会丢精度，可能让两个不同的 ID 看起来相等。
func jsonBodyAppIDs(c *gin.Context) ([]int64, error) {
	if c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, nil
	}
	if !strings.Contains(strings.ToLower(c.ContentType()), "json") {
		return nil, nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		// 不是 JSON 对象（数组、格式错误）：这里不判，交给处理函数报格式错误
		return nil, nil
	}
	var out []int64
	for _, key := range []string{"appid", "appId"} {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		var raw string
		switch typed := value.(type) {
		case json.Number:
			raw = typed.String()
		case string:
			raw = typed
		default:
			return nil, errAppIDInvalid
		}
		id, ok := parseOptionalInt64(raw)
		if !ok {
			return nil, errAppIDInvalid
		}
		out = append(out, id)
	}
	return out, nil
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
