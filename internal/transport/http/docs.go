package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
}

type docOperation struct {
	Summary      string
	Description  string
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
	Keyword string `form:"keyword"`
	Enabled *bool  `form:"enabled"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
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
	}
}

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
	router.GET("/docs", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderScalarHTML(opts)))
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

	routeDocs := manualRouteDocs(generator, spec)
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
		op.OperationID = buildOperationID(method, openAPIPath)
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
	case path == "/api/admin/auth/login":
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

func deriveTags(path string) []string {
	switch {
	case path == "/healthz" || path == "/readyz":
		return []string{"System"}
	case path == "/api/ws":
		return []string{"Realtime"}
	case strings.HasPrefix(path, "/api/admin/auth/"):
		return []string{"Admin Auth"}
	case strings.HasPrefix(path, "/api/admin/system/"):
		return []string{"Admin System"}
	case strings.HasPrefix(path, "/api/admin/"):
		return []string{"Admin"}
	case strings.HasPrefix(path, "/api/auth/"):
		return []string{"Auth"}
	case strings.HasPrefix(path, "/api/user-settings"):
		return []string{"User Settings"}
	case strings.HasPrefix(path, "/api/user/"):
		return []string{"User"}
	case strings.HasPrefix(path, "/api/points"):
		return []string{"Points"}
	case strings.HasPrefix(path, "/api/notifications"):
		return []string{"Notifications"}
	case strings.HasPrefix(path, "/api/email"):
		return []string{"Email"}
	case strings.HasPrefix(path, "/api/public/pay"):
		return []string{"Public Payment"}
	case strings.HasPrefix(path, "/api/pay"):
		return []string{"Payment"}
	case strings.HasPrefix(path, "/api/storage"):
		return []string{"Storage"}
	case strings.HasPrefix(path, "/api/app/workflow"):
		return []string{"Workflow"}
	case strings.HasPrefix(path, "/api/app/"):
		return []string{"App Compat"}
	default:
		return []string{"API"}
	}
}

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

func renderScalarHTML(opts DocsOptions) string {
	title := firstNonEmpty(strings.TrimSpace(opts.Title), DefaultDocsOptions().Title)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    :root {
      color-scheme: dark;
      --bg-a: #020617;
      --bg-b: #0f172a;
      --line: rgba(56, 189, 248, 0.18);
      --panel: rgba(15, 23, 42, 0.78);
      --panel-strong: rgba(15, 23, 42, 0.92);
      --text: #e2e8f0;
      --muted: #94a3b8;
      --accent: #38bdf8;
      --success: #34d399;
      --warn: #f59e0b;
      --danger: #fb7185;
    }
    * { box-sizing: border-box; }
    html, body { margin: 0; min-height: 100%%; background: linear-gradient(160deg, var(--bg-a), var(--bg-b)); }
    body::before {
      content: "";
      position: fixed;
      inset: 0;
      pointer-events: none;
      background:
        linear-gradient(rgba(148,163,184,0.05) 1px, transparent 1px),
        linear-gradient(90deg, rgba(148,163,184,0.05) 1px, transparent 1px);
      background-size: 28px 28px;
      mask-image: linear-gradient(to bottom, rgba(0,0,0,0.35), transparent 95%%);
    }
    header {
      position: sticky;
      top: 0;
      z-index: 2;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 16px 22px;
      border-bottom: 1px solid var(--line);
      backdrop-filter: blur(14px);
      background: rgba(2, 6, 23, 0.7);
    }
    .brand {
      display: flex;
      flex-direction: column;
      gap: 4px;
    }
    .eyebrow {
      color: #38bdf8;
      font: 700 11px/1.2 "JetBrains Mono", "Cascadia Code", monospace;
      letter-spacing: 0.24em;
      text-transform: uppercase;
    }
    .title {
      color: #f8fafc;
      font: 700 18px/1.2 "Segoe UI", "Microsoft YaHei UI", sans-serif;
    }
    .link {
      color: #cbd5e1;
      text-decoration: none;
      font: 500 13px/1.2 "Segoe UI", sans-serif;
      border: 1px solid rgba(148, 163, 184, 0.22);
      padding: 9px 12px;
      border-radius: 999px;
      background: rgba(15, 23, 42, 0.5);
    }
    main {
      min-height: calc(100vh - 66px);
      display: grid;
      grid-template-columns: 320px minmax(0, 1fr);
    }
    aside {
      border-right: 1px solid var(--line);
      background: rgba(2, 6, 23, 0.38);
      backdrop-filter: blur(10px);
      padding: 18px;
      overflow: auto;
      height: calc(100vh - 66px);
      position: sticky;
      top: 66px;
    }
    .sidebar-card,
    .content-card {
      border: 1px solid rgba(148, 163, 184, 0.12);
      background: var(--panel);
      border-radius: 18px;
      box-shadow: 0 18px 50px rgba(2, 6, 23, 0.22);
    }
    .sidebar-card {
      padding: 16px;
      margin-bottom: 16px;
    }
    .sidebar-title {
      margin: 0 0 6px;
      color: #f8fafc;
      font: 700 16px/1.25 "Segoe UI", "Microsoft YaHei UI", sans-serif;
    }
    .sidebar-subtitle {
      margin: 0;
      color: var(--muted);
      font: 500 12px/1.55 "Segoe UI", sans-serif;
    }
    .tag-list {
      display: grid;
      gap: 10px;
    }
    .tag-item {
      display: block;
      width: 100%%;
      border: 1px solid rgba(148, 163, 184, 0.12);
      background: rgba(15, 23, 42, 0.58);
      border-radius: 14px;
      padding: 12px 14px;
      color: inherit;
      text-decoration: none;
      cursor: pointer;
      transition: border-color .18s ease, transform .18s ease, background .18s ease;
    }
    .tag-item:hover,
    .tag-item.active {
      border-color: rgba(56, 189, 248, 0.42);
      background: rgba(14, 165, 233, 0.12);
      transform: translateY(-1px);
    }
    .tag-name {
      display: block;
      color: #f8fafc;
      font: 700 13px/1.2 "Segoe UI", sans-serif;
    }
    .tag-meta {
      display: block;
      margin-top: 6px;
      color: var(--muted);
      font: 500 12px/1.2 "Segoe UI", sans-serif;
    }
    section {
      padding: 24px;
    }
    .hero {
      padding: 22px 24px;
      margin-bottom: 20px;
    }
    .hero h1 {
      margin: 0;
      color: #f8fafc;
      font: 700 28px/1.15 "Segoe UI", "Microsoft YaHei UI", sans-serif;
    }
    .hero p {
      margin: 12px 0 0;
      color: var(--muted);
      font: 500 14px/1.7 "Segoe UI", sans-serif;
      max-width: 920px;
    }
    .hero-meta {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      margin-top: 16px;
    }
    .pill {
      border: 1px solid rgba(148, 163, 184, 0.12);
      background: rgba(30, 41, 59, 0.65);
      color: #cbd5e1;
      border-radius: 999px;
      padding: 8px 12px;
      font: 600 12px/1 "Segoe UI", sans-serif;
    }
    .content {
      display: grid;
      gap: 18px;
    }
    .content-card {
      overflow: hidden;
    }
    .card-head {
      padding: 18px 20px 14px;
      border-bottom: 1px solid rgba(148, 163, 184, 0.1);
      background: linear-gradient(180deg, rgba(30, 41, 59, 0.55), rgba(15, 23, 42, 0.2));
    }
    .op-top {
      display: flex;
      gap: 10px;
      align-items: center;
      flex-wrap: wrap;
    }
    .method {
      min-width: 74px;
      text-align: center;
      border-radius: 999px;
      padding: 7px 10px;
      font: 800 11px/1 "JetBrains Mono", "Cascadia Code", monospace;
      letter-spacing: 0.08em;
    }
    .method.get { background: rgba(52, 211, 153, 0.18); color: var(--success); }
    .method.post { background: rgba(56, 189, 248, 0.18); color: var(--accent); }
    .method.put, .method.patch { background: rgba(245, 158, 11, 0.18); color: #fbbf24; }
    .method.delete { background: rgba(251, 113, 133, 0.18); color: var(--danger); }
    .method.other { background: rgba(148, 163, 184, 0.18); color: #cbd5e1; }
    .path {
      color: #f8fafc;
      font: 700 15px/1.45 "JetBrains Mono", "Cascadia Code", monospace;
      word-break: break-all;
    }
    .summary {
      margin-top: 10px;
      color: var(--text);
      font: 700 16px/1.45 "Segoe UI", sans-serif;
    }
    .description {
      margin-top: 8px;
      color: var(--muted);
      font: 500 13px/1.7 "Segoe UI", sans-serif;
      white-space: pre-wrap;
    }
    .card-body {
      display: grid;
      gap: 16px;
      padding: 18px 20px 20px;
    }
    .meta-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px;
    }
    .meta-box {
      border: 1px solid rgba(148, 163, 184, 0.1);
      background: rgba(2, 6, 23, 0.35);
      border-radius: 14px;
      padding: 14px;
    }
    .meta-box h3 {
      margin: 0 0 10px;
      color: #f8fafc;
      font: 700 13px/1.2 "Segoe UI", sans-serif;
    }
    .meta-box p, .meta-box li {
      color: var(--muted);
      font: 500 12px/1.7 "Segoe UI", sans-serif;
    }
    .meta-box ul {
      margin: 0;
      padding-left: 16px;
    }
    .kv {
      display: grid;
      gap: 8px;
    }
    .kv-row {
      display: grid;
      gap: 4px;
      padding: 9px 10px;
      border-radius: 12px;
      background: rgba(15, 23, 42, 0.48);
    }
    .kv-key {
      color: #f8fafc;
      font: 700 12px/1.2 "JetBrains Mono", monospace;
    }
    .kv-val {
      color: var(--muted);
      font: 500 12px/1.55 "Segoe UI", sans-serif;
    }
    pre {
      margin: 0;
      padding: 14px;
      overflow: auto;
      border-radius: 14px;
      border: 1px solid rgba(148, 163, 184, 0.1);
      background: var(--panel-strong);
      color: #dbeafe;
      font: 500 12px/1.6 "JetBrains Mono", "Cascadia Code", monospace;
    }
    .empty,
    .state {
      padding: 36px 24px;
      text-align: center;
      color: var(--muted);
      font: 500 14px/1.7 "Segoe UI", sans-serif;
    }
    .error {
      color: #fecdd3;
    }
    @media (max-width: 1080px) {
      main { grid-template-columns: 1fr; }
      aside {
        height: auto;
        position: static;
        top: auto;
        border-right: 0;
        border-bottom: 1px solid var(--line);
      }
      .meta-grid {
        grid-template-columns: 1fr;
      }
    }
    @media (max-width: 640px) {
      header {
        padding: 14px 16px;
      }
      section {
        padding: 16px;
      }
      aside {
        padding: 16px;
      }
      .hero {
        padding: 18px;
      }
      .hero h1 {
        font-size: 22px;
      }
    }
  </style>
</head>
<body>
  <header>
    <div class="brand">
      <span class="eyebrow">API REFERENCE</span>
      <span class="title">%s</span>
    </div>
    <a class="link" href="/openapi.json">OpenAPI JSON</a>
  </header>
  <main>
    <aside>
      <div class="sidebar-card">
        <h2 class="sidebar-title">Reference Index</h2>
        <p class="sidebar-subtitle">Offline-ready API documentation rendered directly by the service runtime.</p>
      </div>
      <div id="tag-list" class="tag-list">
        <div class="sidebar-card state">Loading API catalog...</div>
      </div>
    </aside>
    <section>
      <div class="hero content-card">
        <h1>%s</h1>
        <p id="hero-description">Loading API metadata...</p>
        <div class="hero-meta">
          <span class="pill" id="hero-version">Version</span>
          <span class="pill" id="hero-server">Server</span>
          <span class="pill" id="hero-count">Operations</span>
        </div>
      </div>
      <div id="content" class="content">
        <div class="content-card state">Loading endpoints...</div>
      </div>
    </section>
  </main>
  <script>
    (function () {
      var tagListEl = document.getElementById('tag-list');
      var contentEl = document.getElementById('content');
      var heroDescriptionEl = document.getElementById('hero-description');
      var heroVersionEl = document.getElementById('hero-version');
      var heroServerEl = document.getElementById('hero-server');
      var heroCountEl = document.getElementById('hero-count');
      var activeTag = 'all';

      function escapeHtml(value) {
        return String(value || '')
          .replace(/&/g, '&amp;')
          .replace(/</g, '&lt;')
          .replace(/>/g, '&gt;')
          .replace(/"/g, '&quot;')
          .replace(/'/g, '&#39;');
      }

      function methodClass(method) {
        var value = String(method || '').toLowerCase();
        if (value === 'get' || value === 'post' || value === 'put' || value === 'patch' || value === 'delete') {
          return value;
        }
        return 'other';
      }

      function summarizeSchema(schemaRef) {
        if (!schemaRef || !schemaRef.value) {
          return 'Schema not specified';
        }
        var schema = schemaRef.value;
        if (schema.type === 'object' && schema.properties) {
          return 'object: ' + Object.keys(schema.properties).join(', ');
        }
        if (schema.type === 'array' && schema.items && schema.items.value && schema.items.value.type) {
          return 'array<' + schema.items.value.type + '>';
        }
        if (schema.type) {
          return schema.type;
        }
        if (schema.enum && schema.enum.length) {
          return 'enum: ' + schema.enum.join(', ');
        }
        return 'structured payload';
      }

      function renderKVRows(items) {
        if (!items || !items.length) {
          return '<p>No data</p>';
        }
        return '<div class="kv">' + items.map(function (item) {
          return '<div class="kv-row"><div class="kv-key">' + escapeHtml(item.key) + '</div><div class="kv-val">' + escapeHtml(item.value) + '</div></div>';
        }).join('') + '</div>';
      }

      function collectOperations(spec) {
        var operations = [];
        var paths = spec.paths || {};
        Object.keys(paths).sort().forEach(function (path) {
          var pathItem = paths[path] || {};
          ['get', 'post', 'put', 'patch', 'delete', 'head', 'options'].forEach(function (method) {
            if (!pathItem[method]) {
              return;
            }
            var op = pathItem[method];
            var tags = Array.isArray(op.tags) && op.tags.length ? op.tags : ['General'];
            operations.push({
              method: method.toUpperCase(),
              path: path,
              summary: op.summary || path,
              description: op.description || '',
              tags: tags,
              operationId: op.operationId || '',
              security: Array.isArray(op.security) ? op.security : [],
              parameters: Array.isArray(op.parameters) ? op.parameters : [],
              requestBody: op.requestBody || null,
              responses: op.responses || {}
            });
          });
        });
        return operations;
      }

      function buildTagMap(operations) {
        var map = { all: [] };
        operations.forEach(function (op) {
          map.all.push(op);
          op.tags.forEach(function (tag) {
            if (!map[tag]) {
              map[tag] = [];
            }
            map[tag].push(op);
          });
        });
        return map;
      }

      function renderSidebar(tagMap) {
        var tags = Object.keys(tagMap).sort(function (a, b) {
          if (a === 'all') return -1;
          if (b === 'all') return 1;
          return a.localeCompare(b);
        });
        tagListEl.innerHTML = tags.map(function (tag) {
          var isActive = tag === activeTag;
          var label = tag === 'all' ? 'All Endpoints' : tag;
          return '<button class="tag-item' + (isActive ? ' active' : '') + '" data-tag="' + escapeHtml(tag) + '">' +
            '<span class="tag-name">' + escapeHtml(label) + '</span>' +
            '<span class="tag-meta">' + tagMap[tag].length + ' operations</span>' +
            '</button>';
        }).join('');

        Array.prototype.forEach.call(tagListEl.querySelectorAll('[data-tag]'), function (node) {
          node.addEventListener('click', function () {
            activeTag = node.getAttribute('data-tag') || 'all';
            renderSidebar(tagMap);
            renderOperations(tagMap[activeTag] || []);
          });
        });
      }

      function renderOperations(operations) {
        if (!operations.length) {
          contentEl.innerHTML = '<div class="content-card empty">No endpoints available for the selected tag.</div>';
          return;
        }

        contentEl.innerHTML = operations.map(function (op) {
          var pathParams = op.parameters.filter(function (parameter) { return parameter.in === 'path'; }).map(function (parameter) {
            return {
              key: parameter.name + ' (' + parameter.in + ')',
              value: parameter.description || 'required parameter'
            };
          });
          var queryParams = op.parameters.filter(function (parameter) { return parameter.in === 'query'; }).map(function (parameter) {
            return {
              key: parameter.name + ' (' + parameter.in + ')',
              value: parameter.description || 'optional query parameter'
            };
          });

          var requestBodyRows = [];
          if (op.requestBody && op.requestBody.content) {
            Object.keys(op.requestBody.content).forEach(function (contentType) {
              var media = op.requestBody.content[contentType] || {};
              requestBodyRows.push({
                key: contentType,
                value: summarizeSchema(media.schema)
              });
            });
          }

          var responseRows = Object.keys(op.responses).sort().map(function (status) {
            var response = op.responses[status] || {};
            var description = response.description || 'response';
            if (response.content) {
              var firstType = Object.keys(response.content)[0];
              if (firstType) {
                description += ' | ' + firstType;
              }
            }
            return {
              key: status,
              value: description
            };
          });

          var securityRows = op.security.map(function (rule, index) {
            return {
              key: 'Rule ' + (index + 1),
              value: Object.keys(rule || {}).join(', ') || 'anonymous'
            };
          });

          return '<article class="content-card">' +
            '<div class="card-head">' +
              '<div class="op-top">' +
                '<span class="method ' + methodClass(op.method) + '">' + escapeHtml(op.method) + '</span>' +
                '<span class="path">' + escapeHtml(op.path) + '</span>' +
              '</div>' +
              '<div class="summary">' + escapeHtml(op.summary) + '</div>' +
              '<div class="description">' + escapeHtml(op.description || 'No description provided.') + '</div>' +
            '</div>' +
            '<div class="card-body">' +
              '<div class="meta-grid">' +
                '<div class="meta-box"><h3>Identity</h3>' + renderKVRows([
                  { key: 'Tag', value: op.tags.join(', ') },
                  { key: 'Operation ID', value: op.operationId || 'not specified' }
                ]) + '</div>' +
                '<div class="meta-box"><h3>Security</h3>' + renderKVRows(securityRows.length ? securityRows : [{ key: 'Access', value: 'Public endpoint' }]) + '</div>' +
                '<div class="meta-box"><h3>Path Parameters</h3>' + renderKVRows(pathParams) + '</div>' +
                '<div class="meta-box"><h3>Query Parameters</h3>' + renderKVRows(queryParams) + '</div>' +
                '<div class="meta-box"><h3>Request Body</h3>' + renderKVRows(requestBodyRows.length ? requestBodyRows : [{ key: 'Body', value: 'No request body' }]) + '</div>' +
                '<div class="meta-box"><h3>Responses</h3>' + renderKVRows(responseRows) + '</div>' +
              '</div>' +
            '</div>' +
          '</article>';
        }).join('');
      }

      function renderSpec(spec) {
        var operations = collectOperations(spec);
        var tagMap = buildTagMap(operations);
        heroDescriptionEl.textContent = spec.info && spec.info.description ? spec.info.description : 'Generated API reference.';
        heroVersionEl.textContent = 'Version: ' + ((spec.info && spec.info.version) || 'n/a');
        heroServerEl.textContent = 'Server: ' + (((spec.servers || [])[0] || {}).url || '/');
        heroCountEl.textContent = 'Operations: ' + operations.length;
        renderSidebar(tagMap);
        renderOperations(tagMap[activeTag] || []);
      }

      fetch('/openapi.json', { cache: 'no-store' })
        .then(function (response) {
          if (!response.ok) {
            throw new Error('failed to fetch openapi');
          }
          return response.json();
        })
        .then(renderSpec)
        .catch(function () {
          tagListEl.innerHTML = '<div class="sidebar-card state error">Failed to load API catalog.</div>';
          contentEl.innerHTML = '<div class="content-card state error">Documentation is temporarily unavailable. Please retry later or open /openapi.json directly.</div>';
          heroDescriptionEl.textContent = 'Documentation failed to load.';
          heroVersionEl.textContent = 'Version: unavailable';
          heroServerEl.textContent = 'Server: unavailable';
          heroCountEl.textContent = 'Operations: unavailable';
        });
    })();
  </script>
</body>
</html>`, title, title, title)
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
