package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	"github.com/gin-gonic/gin"
)

type DocsOptions struct {
	Title       string
	Description string
	Version     string
	ServerURL   string
	// PortalURL 是开发者门户（aegis-console 的 /developers）的地址。
	// /docs 与 /docs/tags/:slug 一律 302 到这里，后端不再自行渲染文档页面。
	PortalURL string
}

type docOperation struct {
	Summary     string
	Description string
	// OperationID 直接决定生成式客户端里的方法名。
	// 留空时回落到从路径拼出来的 `post__api__v1__apps__by_appkey__auth__login`，
	// 那串东西在 Kotlin / Java 客户端里没法看，因此网关接口一律显式指定。
	OperationID  string
	RequestModel any
	RequestBody  *openapi3.RequestBodyRef
	Responses    *openapi3.Responses
	Security     *openapi3.SecurityRequirements
	Tags         []string
}

type OAuthCallbackQuery struct {
	Provider string `form:"provider"`
	Code     string `form:"code"`
	State    string `form:"state"`
}

type SettingsCategoryQuery struct {
	Category string `form:"category"`
}

type AdminLoginAuditFilterQuery struct {
	Keyword string `form:"keyword"`
	Status  string `form:"status"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

type AdminSessionAuditFilterQuery struct {
	Keyword   string `form:"keyword"`
	EventType string `form:"eventType"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

type AdminNotificationFilterQuery struct {
	Keyword string `form:"keyword"`
	Type    string `form:"type"`
	Level   string `form:"level"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

type AdminAppUserFilterQuery struct {
	Keyword     string `form:"keyword"`
	Account     string `form:"account"`
	Nickname    string `form:"nickname"`
	Email       string `form:"email"`
	Phone       string `form:"phone"`
	RegisterIP  string `form:"registerIp"`
	UserID      *int64 `form:"userId"`
	Enabled     *bool  `form:"enabled"`
	CreatedFrom string `form:"createdFrom"`
	CreatedTo   string `form:"createdTo"`
	Page        int    `form:"page"`
	Limit       int    `form:"limit"`
}

type WebSocketQuery struct {
	Token string `form:"token"`
}

func DefaultDocsOptions() DocsOptions {
	return DocsOptions{
		Title:       "Aegis API Reference",
		Description: "A modern OpenAPI reference generated from the Gin router and designed for high-concurrency service evolution.",
		Version:     "1.0.0",
		ServerURL:   "/",
		PortalURL:   DefaultDocsPortalURL,
	}
}

// DefaultDocsPortalURL 指向与后端同源部署时的门户路径。
// 前后端分域时通过 DOCS_PORTAL_URL 配置绝对地址。
const DefaultDocsPortalURL = "/developers"

func RegisterDocsRoutes(router *gin.Engine, opts DocsOptions) error {
	spec, err := BuildOpenAPISpec(router, opts)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	router.GET("/openapi.json", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	})
	// 文档页面由 aegis-console 的 /developers 门户承载（快速接入 + 接口浏览），
	// 后端只保留机器可读的 /openapi.json，避免维护两套互相漂移的文档。
	portal := strings.TrimRight(strings.TrimSpace(opts.PortalURL), "/")
	if portal == "" {
		portal = DefaultDocsPortalURL
	}
	router.GET("/docs", func(c *gin.Context) {
		c.Redirect(http.StatusFound, portal)
	})
	router.GET("/docs/tags/:slug", func(c *gin.Context) {
		// 旧的分组页链接保持可用：带上 tag 查询参数，门户会直接定位到该分组
		slug := strings.TrimSpace(c.Param("slug"))
		target := portal + "/api"
		if slug != "" {
			target += "?tag=" + url.QueryEscape(slug)
		}
		c.Redirect(http.StatusFound, target)
	})
	return nil
}

func BuildOpenAPISpec(router *gin.Engine, opts DocsOptions) (*openapi3.T, error) {
	if router == nil {
		return nil, fmt.Errorf("router is required")
	}

	opts = normalizeDocsOptions(opts)
	spec := &openapi3.T{
		OpenAPI: "3.1.0",
		Info: &openapi3.Info{
			Title:       opts.Title,
			Description: opts.Description,
			Version:     opts.Version,
		},
		Components: &openapi3.Components{
			Schemas:         openapi3.Schemas{},
			SecuritySchemes: openapi3.SecuritySchemes{},
		},
		Paths: openapi3.NewPaths(),
		Servers: openapi3.Servers{
			&openapi3.Server{URL: opts.ServerURL},
		},
	}
	registerSecuritySchemes(spec)

	generator := openapi3gen.NewGenerator(openapi3gen.UseAllExportedFields())

	// 三层叠加，后者覆盖前者：
	//   1. 从路由表推导出的请求模型（docs_route_models.go，机器生成，覆盖面最大）
	//   2. 手工登记的元数据（摘要、响应示例，只覆盖少数重点接口）
	//   3. 网关命名空间（由接入目录生成，多平台客户端就是从这一段产出的）
	routeDocs := map[string]docOperation{}
	for key, model := range generatedRouteModels() {
		routeDocs[key] = docOperation{RequestModel: model}
	}
	for key, doc := range manualRouteDocs(generator, spec) {
		if doc.RequestModel == nil {
			// 手工条目通常只写摘要与响应，别把推导出来的请求模型顶掉。
			if existing, ok := routeDocs[key]; ok {
				doc.RequestModel = existing.RequestModel
			}
		}
		routeDocs[key] = doc
	}
	// 网关目录本身已被 TestGatewayCatalogMatchesRegisteredRoutes 钉在路由上，
	// 因此这一层不会漂移。
	for key, doc := range gatewayRouteDocs() {
		routeDocs[key] = doc
	}
	tagSet := map[string]struct{}{}

	for _, route := range router.Routes() {
		if route.Path == "/docs" || route.Path == "/openapi.json" {
			continue
		}

		method := strings.ToUpper(strings.TrimSpace(route.Method))
		openAPIPath := normalizeOpenAPIPath(route.Path)
		key := routeKey(method, openAPIPath)
		meta, ok := routeDocs[key]
		if !ok {
			meta = docOperation{}
		}

		op := openapi3.NewOperation()
		op.OperationID = firstNonEmpty(meta.OperationID, buildOperationID(method, openAPIPath))
		op.Summary = firstNonEmpty(meta.Summary, humanizeRoute(method, openAPIPath))
		op.Description = firstNonEmpty(meta.Description, defaultOperationDescription(method, openAPIPath))
		op.Tags = firstStringSlice(meta.Tags, deriveTags(openAPIPath))
		op.Responses = meta.Responses
		if op.Responses == nil {
			op.Responses = defaultJSONResponses(successEnvelopeSchema(genericObjectSchemaRef()))
		}
		if meta.Security != nil {
			op.Security = meta.Security
		} else if security := securityForRoute(openAPIPath); len(security) > 0 {
			op.Security = &security
		}

		for _, parameter := range buildPathParameters(openAPIPath) {
			op.Parameters = append(op.Parameters, parameter)
		}

		if meta.RequestBody != nil {
			op.RequestBody = meta.RequestBody
		} else if meta.RequestModel != nil {
			if allowsQueryModel(method) {
				for _, parameter := range buildQueryParameters(generator, spec, meta.RequestModel) {
					op.Parameters = append(op.Parameters, parameter)
				}
			} else {
				body, err := requestBodyForModel(generator, spec, meta.RequestModel, []string{
					"application/json",
					"application/x-www-form-urlencoded",
				})
				if err != nil {
					return nil, fmt.Errorf("build request body for %s %s: %w", method, openAPIPath, err)
				}
				op.RequestBody = body
			}
		}

		spec.AddOperation(openAPIPath, method, op)
		for _, tag := range op.Tags {
			tagSet[tag] = struct{}{}
		}
	}

	spec.Tags = buildSpecTags(tagSet)
	if err := spec.Validate(context.Background()); err != nil {
		return nil, err
	}
	return spec, nil
}

func normalizeDocsOptions(opts DocsOptions) DocsOptions {
	defaults := DefaultDocsOptions()
	opts.Title = firstNonEmpty(strings.TrimSpace(opts.Title), defaults.Title)
	opts.Description = firstNonEmpty(strings.TrimSpace(opts.Description), defaults.Description)
	opts.Version = firstNonEmpty(strings.TrimSpace(opts.Version), defaults.Version)
	opts.ServerURL = firstNonEmpty(strings.TrimSpace(opts.ServerURL), defaults.ServerURL)
	return opts
}

func manualRouteDocs(generator *openapi3gen.Generator, spec *openapi3.T) map[string]docOperation {
	_ = generator
	_ = spec
	return map[string]docOperation{
		routeKey(http.MethodGet, "/healthz"): {
			Summary:     "Health Check",
			Description: "Returns the liveness state of the API runtime.",
			Tags:        []string{"System"},
			Responses:   defaultJSONResponses(successEnvelopeSchema(schemaFromProperties(map[string]*openapi3.SchemaRef{"status": stringSchemaRef("healthy")}))),
		},
		routeKey(http.MethodGet, "/readyz"): {
			Summary:     "Readiness Check",
			Description: "Returns the readiness state of the API runtime.",
			Tags:        []string{"System"},
			Responses:   defaultJSONResponses(successEnvelopeSchema(schemaFromProperties(map[string]*openapi3.SchemaRef{"status": stringSchemaRef("ready")}))),
		},
		routeKey(http.MethodGet, "/api/app/public"): {
			Summary:      "Get Public App Profile",
			Description:  "Returns public application metadata and resolved policy by application identifier.",
			RequestModel: AppIDQuery{},
			Tags:         []string{"App"},
		},
		routeKey(http.MethodPost, "/api/auth/login/password"): {
			Summary:      "Password Login",
			Description:  "Authenticates a user with account and password and returns an access session payload.",
			RequestModel: PasswordLoginRequest{},
			Tags:         []string{"Auth"},
		},
		routeKey(http.MethodPost, "/api/auth/register/password"): {
			Summary:      "Password Register",
			Description:  "Creates a user account for the target application.",
			RequestModel: PasswordRegisterRequest{},
			Tags:         []string{"Auth"},
		},
		routeKey(http.MethodPost, "/api/auth/refresh"): {
			Summary:      "Refresh Token",
			Description:  "Refreshes an access session using a refresh token payload.",
			RequestModel: RefreshRequest{},
			Tags:         []string{"Auth"},
		},
		routeKey(http.MethodGet, "/api/auth/oauth2/callback"): {
			Summary:      "OAuth2 Callback",
			Description:  "Consumes the OAuth2 provider callback parameters and finalizes sign-in.",
			RequestModel: OAuthCallbackQuery{},
			Tags:         []string{"Auth"},
		},
		routeKey(http.MethodPost, "/api/auth/logout"): {
			Summary:     "Logout",
			Description: "Revokes the current access session.",
			Tags:        []string{"Auth"},
		},
		routeKey(http.MethodPost, "/api/admin/auth/login"): {
			Summary:      "Admin Login",
			Description:  "Authenticates an administrator and returns an administrator access session.",
			RequestModel: AdminLoginRequest{},
			Tags:         []string{"Admin Auth"},
		},
		routeKey(http.MethodPost, "/api/admin/auth/register"): {
			Summary:      "Admin Register",
			Description:  "Creates an administrator account and signs it in. The account is created without super-admin status and without any role assignment.",
			RequestModel: AdminRegisterRequest{},
			Tags:         []string{"Admin Auth"},
		},
		routeKey(http.MethodGet, "/api/admin/auth/me"): {
			Summary:     "Admin Session",
			Description: "Returns the active administrator session context.",
			Tags:        []string{"Admin Auth"},
		},
		routeKey(http.MethodPost, "/api/admin/auth/logout"): {
			Summary:     "Admin Logout",
			Description: "Revokes the current administrator session.",
			Tags:        []string{"Admin Auth"},
		},
		routeKey(http.MethodGet, "/api/admin/profile"): {
			Summary:     "Get Admin Profile",
			Description: "Returns the current administrator profile and assignment information.",
			Tags:        []string{"Admin Auth"},
		},
		routeKey(http.MethodPut, "/api/admin/profile"): {
			Summary:      "Update Admin Profile",
			Description:  "Updates the current administrator profile fields.",
			RequestModel: AdminProfileUpdateRequest{},
			Tags:         []string{"Admin Auth"},
		},
		routeKey(http.MethodPost, "/api/admin/profile/avatar"): {
			Summary:     "Upload Admin Avatar",
			Description: "Uploads the current administrator avatar using multipart form data.",
			RequestBody: multipartUploadRequestBody(),
			Tags:        []string{"Admin Auth"},
		},
		routeKey(http.MethodPost, "/api/user/my"): {
			Summary:     "My Dashboard",
			Description: "Returns the aggregated current-user dashboard payload.",
			Tags:        []string{"User"},
		},
		routeKey(http.MethodGet, "/api/user/profile"): {
			Summary:     "Get Profile",
			Description: "Returns the current user's profile data.",
			Tags:        []string{"User"},
		},
		routeKey(http.MethodPut, "/api/user/profile"): {
			Summary:      "Update Profile",
			Description:  "Updates the current user's profile fields.",
			RequestModel: UpdateProfileRequest{},
			Tags:         []string{"User"},
		},
		routeKey(http.MethodPost, "/api/user/profile/avatar"): {
			Summary:     "Upload User Avatar",
			Description: "Uploads the current user's avatar using multipart form data.",
			RequestBody: multipartUploadRequestBody(),
			Tags:        []string{"User"},
		},
		routeKey(http.MethodGet, "/api/user/settings"): {
			Summary:      "Get Settings",
			Description:  "Returns settings grouped by category for the current user.",
			RequestModel: SettingsCategoryQuery{},
			Tags:         []string{"User"},
		},
		routeKey(http.MethodGet, "/api/user-settings"): {
			Summary:      "Get Legacy Settings",
			Description:  "Returns settings grouped by category for the current user through the compatibility endpoint.",
			RequestModel: SettingsCategoryQuery{},
			Tags:         []string{"User Settings"},
		},
		routeKey(http.MethodPut, "/api/user/settings"): {
			Summary:      "Update Settings",
			Description:  "Updates a settings category for the current user.",
			RequestModel: UpdateSettingsRequest{},
			Tags:         []string{"User"},
		},
		routeKey(http.MethodGet, "/api/user/signin/status"): {
			Summary:     "Sign-in Status",
			Description: "Returns the current sign-in state and reward availability.",
			Tags:        []string{"Sign-in"},
		},
		routeKey(http.MethodGet, "/api/user/signin/history"): {
			Summary:      "Sign-in History",
			Description:  "Lists sign-in history for the current user.",
			RequestModel: PaginationQuery{},
			Tags:         []string{"Sign-in"},
		},
		routeKey(http.MethodGet, "/api/user/signin/history/export"): {
			Summary:      "Export Sign-in History",
			Description:  "Exports the current user's sign-in history as CSV.",
			RequestModel: PaginationQuery{},
			Tags:         []string{"Sign-in"},
		},
		routeKey(http.MethodPost, "/api/user/signin"): {
			Summary:      "Sign-in",
			Description:  "Executes the current user's sign-in request.",
			RequestModel: SignInRequest{},
			Tags:         []string{"Sign-in"},
		},
		routeKey(http.MethodGet, "/api/user/banner"): {
			Summary:      "List User Banners",
			Description:  "Returns banner content for the specified application.",
			RequestModel: AppIDQuery{},
			Tags:         []string{"User Public"},
		},
		routeKey(http.MethodGet, "/api/user/notice"): {
			Summary:      "List User Notices",
			Description:  "Returns notice content for the specified application.",
			RequestModel: AppIDQuery{},
			Tags:         []string{"User Public"},
		},
		routeKey(http.MethodGet, "/api/user/check-version"): {
			Summary:      "Check Application Version",
			Description:  "Checks whether a newer version is available for the specified application and client version.",
			RequestModel: VersionCheckQuery{},
			Tags:         []string{"User Public"},
		},
		routeKey(http.MethodGet, "/api/user/site-list"): {
			Summary:      "List Sites",
			Description:  "Returns the public site list for the current user and application scope.",
			RequestModel: SiteListQuery{},
			Tags:         []string{"Sites"},
		},
		routeKey(http.MethodGet, "/api/user/site-detail"): {
			Summary:      "Get Site Detail",
			Description:  "Returns a specific site detail record.",
			RequestModel: SiteDetailQuery{},
			Tags:         []string{"Sites"},
		},
		routeKey(http.MethodGet, "/api/user/role/applications"): {
			Summary:      "List Role Applications",
			Description:  "Lists the current user's role application records.",
			RequestModel: RoleApplicationsQuery{},
			Tags:         []string{"Roles"},
		},
		routeKey(http.MethodGet, "/api/user/audits/login"): {
			Summary:      "List User Login Audits",
			Description:  "Lists login audit records for the current user.",
			RequestModel: UserLoginAuditQuery{},
			Tags:         []string{"User Audits"},
		},
		routeKey(http.MethodGet, "/api/user/audits/login/export"): {
			Summary:      "Export User Login Audits",
			Description:  "Exports login audit records for the current user as CSV.",
			RequestModel: UserLoginAuditQuery{},
			Tags:         []string{"User Audits"},
		},
		routeKey(http.MethodGet, "/api/user/audits/sessions"): {
			Summary:      "List User Session Audits",
			Description:  "Lists session audit records for the current user.",
			RequestModel: UserSessionAuditQuery{},
			Tags:         []string{"User Audits"},
		},
		routeKey(http.MethodGet, "/api/user/audits/sessions/export"): {
			Summary:      "Export User Session Audits",
			Description:  "Exports session audit records for the current user as CSV.",
			RequestModel: UserSessionAuditQuery{},
			Tags:         []string{"User Audits"},
		},
		routeKey(http.MethodGet, "/api/notifications"): {
			Summary:      "List Notifications",
			Description:  "Lists notifications for the current user with optional filters.",
			RequestModel: NotificationQuery{},
			Tags:         []string{"Notifications"},
		},
		routeKey(http.MethodGet, "/api/notifications/unread-count"): {
			Summary:     "Unread Count",
			Description: "Returns the unread notification count.",
			Tags:        []string{"Notifications"},
			Responses:   defaultJSONResponses(successEnvelopeSchema(schemaFromProperties(map[string]*openapi3.SchemaRef{"unread": int64SchemaRef()}))),
		},
		routeKey(http.MethodPost, "/api/notifications/read"): {
			Summary:      "Mark Notification Read",
			Description:  "Marks a single notification as read.",
			RequestModel: NotificationReadRequest{},
			Tags:         []string{"Notifications"},
		},
		routeKey(http.MethodPost, "/api/notifications/read-batch"): {
			Summary:      "Batch Read Notifications",
			Description:  "Marks multiple notifications as read.",
			RequestModel: NotificationReadBatchRequest{},
			Tags:         []string{"Notifications"},
		},
		routeKey(http.MethodPost, "/api/notifications/clear"): {
			Summary:      "Clear Notifications",
			Description:  "Clears notifications based on optional filters.",
			RequestModel: NotificationClearRequest{},
			Tags:         []string{"Notifications"},
		},
		routeKey(http.MethodGet, "/api/admin/apps"): {
			Summary:     "List Applications",
			Description: "Returns the application catalog visible to the current administrator.",
			Tags:        []string{"Admin"},
		},
		routeKey(http.MethodPost, "/api/admin/apps"): {
			Summary:      "Create Application",
			Description:  "Creates or initializes an application visible to the current administrator.",
			RequestModel: AdminAppCreateRequest{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodPut, "/api/admin/apps/{appid}"): {
			Summary:      "Update Application",
			Description:  "Updates the application profile and switch configuration.",
			RequestModel: AdminAppUpsertRequest{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/signin-reward"): {
			Summary:     "Get Application Sign-in Reward Policy",
			Description: "Returns the app-level sign-in reward policy used by the daily sign-in service.",
			Tags:        []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/signin/stats"): {
			Summary:      "Get Application Sign-in Stats",
			Description:  "Returns management-side sign-in statistics, recent trend, and source distribution for the specified application.",
			RequestModel: AdminAppSignInStatsQuery{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/signin/records"): {
			Summary:      "List Application Sign-in Records",
			Description:  "Lists daily sign-in detail records for the specified application with filters and pagination.",
			RequestModel: AdminAppSignInRecordQuery{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodPut, "/api/admin/apps/{appid}/signin-reward"): {
			Summary:      "Update Application Sign-in Reward Policy",
			Description:  "Updates the app-level sign-in reward policy, including expression rules and milestone bonuses.",
			RequestModel: AdminSignInRewardPolicyUpdateRequest{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodPost, "/api/admin/apps/{appid}/signin-reward/test"): {
			Summary:      "Test Application Sign-in Reward Policy",
			Description:  "Simulates sign-in reward calculation for the specified application without creating a sign-in record.",
			RequestModel: AdminSignInRewardTestRequest{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodPost, "/api/admin/apps/{appid}/signin-reward/reset"): {
			Summary:     "Reset Application Sign-in Reward Policy",
			Description: "Resets the application sign-in reward policy back to the built-in default template.",
			Tags:        []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/signin-reward/templates"): {
			Summary:     "List Sign-in Reward Templates",
			Description: "Returns built-in sign-in reward templates for management-side initialization.",
			Tags:        []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/stats/user-trend"): {
			Summary:      "Get Application User Trend",
			Description:  "Returns the user growth trend for the specified application.",
			RequestModel: AdminAppTrendQuery{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/stats/regions"): {
			Summary:      "Get Application Region Stats",
			Description:  "Returns regional distribution statistics for the specified application.",
			RequestModel: AdminRegionStatsQuery{},
			Tags:         []string{"Admin"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/audits/login"): {
			Summary:      "List Application Login Audits",
			Description:  "Lists login audit records for the specified application.",
			RequestModel: AdminLoginAuditFilterQuery{},
			Tags:         []string{"Admin Audits"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/audits/login/export"): {
			Summary:      "Export Application Login Audits",
			Description:  "Exports application login audit records as CSV.",
			RequestModel: AdminLoginAuditFilterQuery{},
			Tags:         []string{"Admin Audits"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/audits/sessions"): {
			Summary:      "List Application Session Audits",
			Description:  "Lists session audit records for the specified application.",
			RequestModel: AdminSessionAuditFilterQuery{},
			Tags:         []string{"Admin Audits"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/audits/sessions/export"): {
			Summary:      "Export Application Session Audits",
			Description:  "Exports application session audit records as CSV.",
			RequestModel: AdminSessionAuditFilterQuery{},
			Tags:         []string{"Admin Audits"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/notifications"): {
			Summary:      "List Application Notifications",
			Description:  "Lists notification records for the specified application.",
			RequestModel: AdminNotificationFilterQuery{},
			Tags:         []string{"Admin Notifications"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/notifications/export"): {
			Summary:      "Export Application Notifications",
			Description:  "Exports application notification records as CSV.",
			RequestModel: AdminNotificationFilterQuery{},
			Tags:         []string{"Admin Notifications"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/users"): {
			Summary:      "List Application Users",
			Description:  "Lists users under the specified application with filters and pagination.",
			RequestModel: AdminAppUserFilterQuery{},
			Tags:         []string{"Admin Users"},
		},
		// 注意路径参数写 {appkey}：routeKey 是纯字符串拼接，不做任何归一化，
		// 键与 BuildOpenAPISpec 生成的路径必须**逐字节相同**才会命中。
		// 本文件里另有 21 条 {appid} 开头的条目，路由早已改名为 :appkey，那些键全部不匹配。
		routeKey(http.MethodGet, "/api/admin/apps/{appkey}/users/{userId}/audits/login"): {
			Summary:      "List User Login Audits",
			Description:  "Lists login audit records for a single application user.",
			RequestModel: AdminUserLoginAuditQuery{},
			Tags:         []string{"Admin Users"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appkey}/users/{userId}/audits/sessions"): {
			Summary:      "List User Session Audits",
			Description:  "Lists session event audit records for a single application user.",
			RequestModel: AdminUserSessionAuditQuery{},
			Tags:         []string{"Admin Users"},
		},
		routeKey(http.MethodGet, "/api/admin/apps/{appid}/users/export"): {
			Summary:      "Export Application Users",
			Description:  "Exports application users as CSV.",
			RequestModel: AdminAppUserFilterQuery{},
			Tags:         []string{"Admin Users"},
		},
		routeKey(http.MethodGet, "/api/admin/user-settings/stats"): {
			Summary:      "Get User Settings Stats",
			Description:  "Returns settings statistics for the specified application.",
			RequestModel: AdminSettingsStatsQuery{},
			Tags:         []string{"Admin Settings"},
		},
		routeKey(http.MethodGet, "/api/admin/user-settings/user"): {
			Summary:      "Get User Settings Detail",
			Description:  "Returns settings for a specific user under an application.",
			RequestModel: AdminUserSettingsQuery{},
			Tags:         []string{"Admin Settings"},
		},
		routeKey(http.MethodGet, "/api/admin/user-settings/check-integrity"): {
			Summary:      "Check Settings Integrity",
			Description:  "Checks application user settings integrity and optionally repairs invalid records.",
			RequestModel: AdminSettingsIntegrityQuery{},
			Tags:         []string{"Admin Settings"},
		},
		routeKey(http.MethodGet, "/api/admin/user-settings/cleanup"): {
			Summary:      "Cleanup Invalid Settings",
			Description:  "Cleans invalid settings records for the specified application.",
			RequestModel: AdminSettingsCleanupQuery{},
			Tags:         []string{"Admin Settings"},
		},
		routeKey(http.MethodGet, "/api/admin/system/online/stats"): {
			Summary:     "Get Online Overview",
			Description: "Returns the aggregated online status overview across applications.",
			Tags:        []string{"Admin System"},
		},
		routeKey(http.MethodGet, "/api/admin/system/settings"): {
			Summary:     "Get System Settings",
			Description: "Returns the current platform-level system settings snapshot.",
			Tags:        []string{"Admin System"},
		},
		routeKey(http.MethodPut, "/api/admin/system/settings"): {
			Summary:      "Update System Settings",
			Description:  "Updates platform-level system settings and applies them through hot reload.",
			RequestModel: AdminSystemSettingsUpdateRequest{},
			Tags:         []string{"Admin System"},
		},
		routeKey(http.MethodGet, "/api/admin/system/online/apps/{appid}"): {
			Summary:     "Get Application Online Stats",
			Description: "Returns online statistics for a specific application.",
			Tags:        []string{"Admin System"},
		},
		routeKey(http.MethodGet, "/api/admin/system/online/apps/{appid}/users"): {
			Summary:      "List Application Online Users",
			Description:  "Lists online users for a specific application.",
			RequestModel: PaginationQuery{},
			Tags:         []string{"Admin System"},
		},
		routeKey(http.MethodPost, "/api/storage/object-link"): {
			Summary:      "Create Object Link",
			Description:  "Creates a direct or proxied download link for a storage object.",
			RequestModel: StorageObjectLinkRequest{},
			Tags:         []string{"Storage"},
		},
		routeKey(http.MethodPost, "/api/storage/upload"): {
			Summary:     "Upload Object",
			Description: "Uploads a file to the configured storage backend using multipart form data.",
			RequestBody: multipartUploadRequestBody(),
			Tags:        []string{"Storage"},
		},
		routeKey(http.MethodGet, "/api/storage/proxy/{ticket}"): {
			Summary:     "Proxy Download",
			Description: "Streams a proxied storage object when private download mode is enabled.",
			Tags:        []string{"Storage"},
			Responses:   binaryDownloadResponses(),
		},
		routeKey(http.MethodGet, "/api/ws"): {
			Summary:      "WebSocket Upgrade",
			Description:  "Upgrades the connection to the global realtime WebSocket gateway. Authentication supports an Authorization bearer token or the `token` query parameter.",
			RequestModel: WebSocketQuery{},
			Tags:         []string{"Realtime"},
			Security:     websocketSecurity(),
			Responses:    websocketResponses(),
		},
	}
}

func registerSecuritySchemes(spec *openapi3.T) {
	spec.Components.SecuritySchemes["bearerAuth"] = &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "User access token passed through the Authorization header.",
	}}
	spec.Components.SecuritySchemes["adminBearerAuth"] = &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Administrator access token passed through the Authorization header.",
	}}
	spec.Components.SecuritySchemes["xAdminToken"] = &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{
		Type:        "apiKey",
		In:          "header",
		Name:        "X-Admin-Token",
		Description: "Alternative administrator token header supported by the platform.",
	}}
	spec.Components.SecuritySchemes["wsQueryToken"] = &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{
		Type:        "apiKey",
		In:          "query",
		Name:        "token",
		Description: "Query token supported by the realtime WebSocket gateway.",
	}}
}

func securityForRoute(path string) openapi3.SecurityRequirements {
	switch {
	case path == "/healthz", path == "/readyz", path == "/api/app/public", path == "/openapi.json", path == "/docs":
		return nil
	case path == "/api/ws":
		return derefSecurityRequirements(websocketSecurity())
	case path == "/api/admin/auth/login", path == "/api/admin/auth/register":
		// 注册和登录一样是**未登录**才会调的接口。归进下面那条
		// `/api/admin/` 前缀规则会给它标上 adminBearerAuth，
		// 于是文档站的调试台在没有令牌时不让发这个请求。
		return nil
	case strings.HasPrefix(path, "/api/admin/auth/"), strings.HasPrefix(path, "/api/admin/"), strings.HasPrefix(path, "/api/app/password-policy"), strings.HasPrefix(path, "/api/app/points"), strings.HasPrefix(path, "/api/app/workflow"):
		return openapi3.SecurityRequirements{
			openapi3.NewSecurityRequirement().Authenticate("adminBearerAuth"),
			openapi3.NewSecurityRequirement().Authenticate("xAdminToken"),
		}
	case strings.HasPrefix(path, "/api/auth/register/password"),
		strings.HasPrefix(path, "/api/auth/login/password"),
		strings.HasPrefix(path, "/api/auth/oauth2/auth-url"),
		strings.HasPrefix(path, "/api/auth/oauth2/callback"),
		strings.HasPrefix(path, "/api/auth/oauth2/mobile-login"),
		strings.HasPrefix(path, "/api/auth/refresh"),
		strings.HasPrefix(path, "/api/email/send-code"),
		strings.HasPrefix(path, "/api/email/verify-code"),
		strings.HasPrefix(path, "/api/email/send-password-reset"),
		strings.HasPrefix(path, "/api/email/verify-reset-token"),
		strings.HasPrefix(path, "/api/email/webhook/"),
		path == "/api/user/banner",
		path == "/api/user/notice",
		path == "/api/user/level/config",
		path == "/api/user/check-version",
		strings.HasPrefix(path, "/api/public/pay"),
		strings.HasPrefix(path, "/api/storage/proxy/"):
		return nil
	default:
		return openapi3.SecurityRequirements{
			openapi3.NewSecurityRequirement().Authenticate("bearerAuth"),
		}
	}
}

func websocketSecurity() *openapi3.SecurityRequirements {
	requirements := openapi3.SecurityRequirements{
		openapi3.NewSecurityRequirement().Authenticate("bearerAuth"),
		openapi3.NewSecurityRequirement().Authenticate("wsQueryToken"),
	}
	return &requirements
}

func buildPathParameters(path string) openapi3.Parameters {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	params := openapi3.Parameters{}
	for _, part := range parts {
		if !strings.HasPrefix(part, "{") || !strings.HasSuffix(part, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
		param := &openapi3.Parameter{
			In:          "path",
			Name:        name,
			Required:    true,
			Description: "Path parameter extracted from the route.",
			Schema:      stringSchemaRef(""),
		}
		params = append(params, &openapi3.ParameterRef{Value: param})
	}
	return params
}

func buildQueryParameters(generator *openapi3gen.Generator, spec *openapi3.T, model any) openapi3.Parameters {
	modelType := reflect.TypeOf(model)
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil
	}

	params := openapi3.Parameters{}
	appendQueryParameters(generator, spec, modelType, &params)
	return params
}

func appendQueryParameters(generator *openapi3gen.Generator, spec *openapi3.T, modelType reflect.Type, params *openapi3.Parameters) {
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			nestedType := field.Type
			for nestedType.Kind() == reflect.Pointer {
				nestedType = nestedType.Elem()
			}
			if nestedType.Kind() == reflect.Struct {
				appendQueryParameters(generator, spec, nestedType, params)
			}
			continue
		}

		name := queryFieldName(field)
		if name == "" || complexQueryField(field.Type) {
			continue
		}
		schema, err := schemaRefForType(generator, spec, field.Type)
		if err != nil {
			continue
		}
		parameter := &openapi3.Parameter{
			In:       "query",
			Name:     name,
			Required: strings.Contains(field.Tag.Get("binding"), "required"),
			Schema:   schema,
		}
		*params = append(*params, &openapi3.ParameterRef{Value: parameter})
	}
}

func requestBodyForModel(generator *openapi3gen.Generator, spec *openapi3.T, model any, contentTypes []string) (*openapi3.RequestBodyRef, error) {
	schema, err := schemaRefForValue(generator, spec, model)
	if err != nil {
		return nil, err
	}
	content := openapi3.Content{}
	for _, contentType := range contentTypes {
		content[contentType] = &openapi3.MediaType{Schema: schema}
	}
	return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{
		Required: true,
		Content:  content,
	}}, nil
}

func multipartUploadRequestBody() *openapi3.RequestBodyRef {
	fileSchema := openapi3.NewStringSchema().WithFormat("binary").NewRef()
	schema := openapi3.NewObjectSchema().
		WithPropertyRef("file", fileSchema).
		WithPropertyRef("config_name", stringSchemaRef("")).
		WithPropertyRef("object_key", stringSchemaRef("")).
		WithPropertyRef("file_name", stringSchemaRef("")).
		WithPropertyRef("content_type", stringSchemaRef("")).
		WithPropertyRef("cache_control", stringSchemaRef("")).
		WithPropertyRef("metadata", genericObjectSchemaRef()).
		WithRequired([]string{"file"})
	return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{
		Required: true,
		Content: openapi3.Content{
			"multipart/form-data": &openapi3.MediaType{Schema: schema.NewRef()},
		},
	}}
}

func defaultJSONResponses(successSchema *openapi3.SchemaRef) *openapi3.Responses {
	responses := openapi3.NewResponsesWithCapacity(7)
	responses.Set("200", jsonResponse("Successful response.", successSchema))
	responses.Set("400", jsonResponse("Bad request.", errorEnvelopeSchema()))
	responses.Set("401", jsonResponse("Unauthorized.", errorEnvelopeSchema()))
	responses.Set("403", jsonResponse("Forbidden.", errorEnvelopeSchema()))
	responses.Set("404", jsonResponse("Resource not found.", errorEnvelopeSchema()))
	responses.Set("429", jsonResponse("Too many requests.", errorEnvelopeSchema()))
	responses.Set("503", jsonResponse("Service unavailable.", errorEnvelopeSchema()))
	return responses
}

func websocketResponses() *openapi3.Responses {
	responses := openapi3.NewResponsesWithCapacity(4)
	responses.Set("101", &openapi3.ResponseRef{Value: &openapi3.Response{Description: ptr("WebSocket upgrade completed.")}})
	responses.Set("401", jsonResponse("Unauthorized.", errorEnvelopeSchema()))
	responses.Set("403", jsonResponse("Forbidden.", errorEnvelopeSchema()))
	responses.Set("503", jsonResponse("Realtime service unavailable.", errorEnvelopeSchema()))
	return responses
}

func binaryDownloadResponses() *openapi3.Responses {
	responses := openapi3.NewResponsesWithCapacity(4)
	responses.Set("200", &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: ptr("Binary object stream."),
		Content: openapi3.Content{
			"application/octet-stream": &openapi3.MediaType{
				Schema: openapi3.NewStringSchema().WithFormat("binary").NewRef(),
			},
		},
	}})
	responses.Set("400", jsonResponse("Bad request.", errorEnvelopeSchema()))
	responses.Set("404", jsonResponse("Resource not found.", errorEnvelopeSchema()))
	responses.Set("503", jsonResponse("Service unavailable.", errorEnvelopeSchema()))
	return responses
}

func jsonResponse(description string, schema *openapi3.SchemaRef) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: ptr(description),
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{Schema: schema},
		},
	}}
}

func successEnvelopeSchema(data *openapi3.SchemaRef) *openapi3.SchemaRef {
	schema := openapi3.NewObjectSchema().
		WithPropertyRef("code", openapi3.NewInt64Schema().NewRef()).
		WithPropertyRef("message", stringSchemaRef("")).
		WithPropertyRef("requestId", stringSchemaRef("")).
		WithRequired([]string{"code", "message"})
	if data != nil {
		schema.WithPropertyRef("data", data)
	}
	return schema.NewRef()
}

func errorEnvelopeSchema() *openapi3.SchemaRef {
	return openapi3.NewObjectSchema().
		WithPropertyRef("code", openapi3.NewInt64Schema().NewRef()).
		WithPropertyRef("message", stringSchemaRef("")).
		WithPropertyRef("requestId", stringSchemaRef("")).
		WithRequired([]string{"code", "message"}).
		NewRef()
}

func schemaFromProperties(properties map[string]*openapi3.SchemaRef) *openapi3.SchemaRef {
	schema := openapi3.NewObjectSchema()
	keys := make([]string, 0, len(properties))
	for key, value := range properties {
		schema.WithPropertyRef(key, value)
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		schema.WithRequired(keys)
	}
	return schema.NewRef()
}

func genericObjectSchemaRef() *openapi3.SchemaRef {
	return openapi3.NewObjectSchema().WithAnyAdditionalProperties().NewRef()
}

func stringSchemaRef(example string) *openapi3.SchemaRef {
	schema := openapi3.NewStringSchema()
	if strings.TrimSpace(example) != "" {
		schema.Example = example
	}
	return schema.NewRef()
}

func int64SchemaRef() *openapi3.SchemaRef {
	return openapi3.NewInt64Schema().NewRef()
}

func schemaRefForValue(generator *openapi3gen.Generator, spec *openapi3.T, value any) (*openapi3.SchemaRef, error) {
	return generator.NewSchemaRefForValue(value, spec.Components.Schemas)
}

func schemaRefForType(generator *openapi3gen.Generator, spec *openapi3.T, fieldType reflect.Type) (*openapi3.SchemaRef, error) {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	var value any
	switch fieldType.Kind() {
	case reflect.Struct:
		if fieldType.PkgPath() == "time" && fieldType.Name() == "Time" {
			value = time.Time{}
		} else {
			value = reflect.New(fieldType).Elem().Interface()
		}
	case reflect.Slice:
		value = reflect.MakeSlice(fieldType, 0, 0).Interface()
	case reflect.Array:
		value = reflect.New(fieldType).Elem().Interface()
	default:
		value = reflect.New(fieldType).Elem().Interface()
	}
	return schemaRefForValue(generator, spec, value)
}

func queryFieldName(field reflect.StructField) string {
	for _, key := range []string{"form", "json"} {
		tag := strings.TrimSpace(field.Tag.Get(key))
		if tag == "" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		return name
	}
	return ""
}

func complexQueryField(fieldType reflect.Type) bool {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if fieldType.PkgPath() == "time" && fieldType.Name() == "Time" {
		return false
	}
	switch fieldType.Kind() {
	case reflect.Map, reflect.Struct:
		return true
	default:
		return false
	}
}

func normalizeOpenAPIPath(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func routeKey(method string, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path)
}

func humanizeRoute(method string, path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return strings.ToUpper(method)
	}
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		part = strings.Trim(part, "{}")
		part = strings.ReplaceAll(part, "-", " ")
		part = strings.ReplaceAll(part, "_", " ")
		parts[i] = strings.Title(part)
	}
	return strings.ToUpper(method) + " " + strings.Join(parts, " ")
}

func defaultOperationDescription(method string, path string) string {
	return fmt.Sprintf("Auto-generated reference for `%s %s`.", strings.ToUpper(method), path)
}

func buildOperationID(method string, path string) string {
	replacer := strings.NewReplacer(
		"/", "__",
		"-", "_dash_",
		"{", "_by_",
		"}", "",
		":", "_",
		".", "_",
	)
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return strings.ToLower(method) + "__root"
	}
	return strings.ToLower(method) + "__" + replacer.Replace(trimmed)
}

// deriveTags 见 route_groups.go —— OpenAPI 标签与路由清单的分组读同一张规则表。

func buildSpecTags(tagSet map[string]struct{}) openapi3.Tags {
	names := make([]string, 0, len(tagSet))
	for name := range tagSet {
		names = append(names, name)
	}
	sort.Strings(names)
	tags := make(openapi3.Tags, 0, len(names))
	for _, name := range names {
		tags = append(tags, &openapi3.Tag{Name: name})
	}
	return tags
}

func allowsQueryModel(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func derefSecurityRequirements(value *openapi3.SecurityRequirements) openapi3.SecurityRequirements {
	if value == nil {
		return nil
	}
	return *value
}

func ptr[T any](value T) *T {
	return &value
}
