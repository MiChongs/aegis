package httptransport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type PostmanOptions struct {
	Name        string
	Description string
	BaseURL     string
}

type postmanCollection struct {
	Info     postmanInfo   `json:"info"`
	Item     []postmanItem `json:"item"`
	Variable []postmanKV   `json:"variable,omitempty"`
}

type postmanInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schema      string `json:"schema"`
}

type postmanItem struct {
	Name     string        `json:"name"`
	Item     []postmanItem `json:"item,omitempty"`
	Request  *postmanReq   `json:"request,omitempty"`
	Response []any         `json:"response"`
}

type postmanReq struct {
	Method      string       `json:"method"`
	Header      []postmanKV  `json:"header,omitempty"`
	Body        *postmanBody `json:"body,omitempty"`
	URL         postmanURL   `json:"url"`
	Description string       `json:"description,omitempty"`
}

type postmanBody struct {
	Mode       string            `json:"mode"`
	Raw        string            `json:"raw,omitempty"`
	URLEncoded []postmanBodyKV   `json:"urlencoded,omitempty"`
	FormData   []postmanFormData `json:"formdata,omitempty"`
	Options    *postmanBodyOpts  `json:"options,omitempty"`
}

type postmanBodyOpts struct {
	Raw postmanBodyRawOpts `json:"raw"`
}

type postmanBodyRawOpts struct {
	Language string `json:"language"`
}

type postmanURL struct {
	Raw      string      `json:"raw"`
	Host     []string    `json:"host,omitempty"`
	Path     []string    `json:"path,omitempty"`
	Query    []postmanKV `json:"query,omitempty"`
	Variable []postmanKV `json:"variable,omitempty"`
}

type postmanKV struct {
	Key         string `json:"key"`
	Value       any    `json:"value,omitempty"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type postmanBodyKV struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type postmanFormData struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

func DefaultPostmanOptions() PostmanOptions {
	return PostmanOptions{
		Name:        "Aegis 接口测试集合",
		Description: "基于服务端 OpenAPI 自动生成的 Postman 中文测试集合。",
		BaseURL:     "{{baseUrl}}",
	}
}

func BuildPostmanCollection(spec *openapi3.T, opts PostmanOptions) (*postmanCollection, error) {
	if spec == nil {
		return nil, fmt.Errorf("openapi spec is required")
	}
	opts = normalizePostmanOptions(opts)

	folders := map[string][]postmanItem{}
	pathVariables := map[string]struct{}{}

	paths := spec.Paths.Map()
	pathKeys := make([]string, 0, len(paths))
	for path := range paths {
		pathKeys = append(pathKeys, path)
	}
	sort.Strings(pathKeys)

	for _, path := range pathKeys {
		pathItem := paths[path]
		for _, operation := range pathItemOperations(pathItem) {
			item, vars, err := buildPostmanRequestItem(path, operation.method, operation.op, opts)
			if err != nil {
				return nil, err
			}
			for _, name := range vars {
				pathVariables[name] = struct{}{}
			}
			tag := "通用接口"
			if len(operation.op.Tags) > 0 {
				tag = translateTagCN(operation.op.Tags[0])
			}
			folders[tag] = append(folders[tag], item)
		}
	}

	folderNames := make([]string, 0, len(folders))
	for name := range folders {
		folderNames = append(folderNames, name)
	}
	sort.Strings(folderNames)

	items := make([]postmanItem, 0, len(folderNames))
	for _, folder := range folderNames {
		requests := folders[folder]
		sort.SliceStable(requests, func(i, j int) bool {
			return requests[i].Name < requests[j].Name
		})
		items = append(items, postmanItem{
			Name:     folder,
			Item:     requests,
			Response: []any{},
		})
	}

	variables := []postmanKV{
		{Key: "baseUrl", Value: "http://127.0.0.1:8080"},
		{Key: "userToken", Value: "请填写用户令牌"},
		{Key: "adminToken", Value: "请填写管理员令牌"},
		{Key: "appid", Value: "10000"},
	}

	pathVarNames := make([]string, 0, len(pathVariables))
	for name := range pathVariables {
		if name == "appid" {
			continue
		}
		pathVarNames = append(pathVarNames, name)
	}
	sort.Strings(pathVarNames)
	for _, name := range pathVarNames {
		variables = append(variables, postmanKV{
			Key:   name,
			Value: defaultVariableValue(name),
		})
	}

	return &postmanCollection{
		Info: postmanInfo{
			Name:        opts.Name,
			Description: opts.Description,
			Schema:      "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Item:     items,
		Variable: variables,
	}, nil
}

type pathOperation struct {
	method string
	op     *openapi3.Operation
}

func pathItemOperations(pathItem *openapi3.PathItem) []pathOperation {
	if pathItem == nil {
		return nil
	}
	items := []pathOperation{}
	appendIf := func(method string, op *openapi3.Operation) {
		if op != nil {
			items = append(items, pathOperation{method: method, op: op})
		}
	}
	appendIf("GET", pathItem.Get)
	appendIf("POST", pathItem.Post)
	appendIf("PUT", pathItem.Put)
	appendIf("PATCH", pathItem.Patch)
	appendIf("DELETE", pathItem.Delete)
	appendIf("HEAD", pathItem.Head)
	appendIf("OPTIONS", pathItem.Options)
	return items
}

func buildPostmanRequestItem(path string, method string, op *openapi3.Operation, opts PostmanOptions) (postmanItem, []string, error) {
	url, pathVars := buildPostmanURL(path, op.Parameters, opts.BaseURL)
	headers := []postmanKV{
		{Key: "Accept", Value: "application/json", Type: "text"},
	}
	mergeHeaders(&headers, authHeadersForOperation(op))

	body, contentType, err := buildPostmanBody(op.RequestBody)
	if err != nil {
		return postmanItem{}, nil, err
	}
	if contentType != "" {
		headers = append(headers, postmanKV{Key: "Content-Type", Value: contentType, Type: "text"})
	}

	description := strings.TrimSpace(op.Description)
	if description == "" {
		description = strings.TrimSpace(op.Summary)
	}

	return postmanItem{
		Name: localizedOperationName(method, path, op),
		Request: &postmanReq{
			Method:      method,
			Header:      headers,
			Body:        body,
			URL:         url,
			Description: description,
		},
		Response: []any{},
	}, pathVars, nil
}

func buildPostmanURL(path string, params openapi3.Parameters, baseURL string) (postmanURL, []string) {
	rawPath := path
	pathVars := []postmanKV{}
	varNames := []string{}
	query := []postmanKV{}

	for _, paramRef := range params {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		switch param.In {
		case "path":
			value := fmt.Sprintf("{{%s}}", param.Name)
			rawPath = strings.ReplaceAll(rawPath, "{"+param.Name+"}", value)
			pathVars = append(pathVars, postmanKV{
				Key:         param.Name,
				Value:       defaultVariableValue(param.Name),
				Description: param.Description,
			})
			varNames = append(varNames, param.Name)
		case "query":
			query = append(query, postmanKV{
				Key:         param.Name,
				Value:       defaultParameterValue(param.Name, schemaValue(param.Schema)),
				Description: param.Description,
			})
		}
	}

	pathSegments := []string{}
	for _, segment := range strings.Split(strings.Trim(rawPath, "/"), "/") {
		if strings.TrimSpace(segment) != "" {
			pathSegments = append(pathSegments, segment)
		}
	}

	raw := strings.TrimRight(baseURL, "/")
	if rawPath != "/" {
		raw += rawPath
	}
	if len(query) > 0 {
		parts := make([]string, 0, len(query))
		for _, item := range query {
			parts = append(parts, fmt.Sprintf("%s=%v", item.Key, item.Value))
		}
		raw += "?" + strings.Join(parts, "&")
	}

	return postmanURL{
		Raw:      raw,
		Host:     []string{baseURL},
		Path:     pathSegments,
		Query:    query,
		Variable: pathVars,
	}, varNames
}

func buildPostmanBody(bodyRef *openapi3.RequestBodyRef) (*postmanBody, string, error) {
	if bodyRef == nil || bodyRef.Value == nil {
		return nil, "", nil
	}
	content := bodyRef.Value.Content
	if len(content) == 0 {
		return nil, "", nil
	}

	if media, ok := content["application/json"]; ok {
		payload := exampleValueFromSchema(media.Schema, 0)
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return nil, "", err
		}
		return &postmanBody{
			Mode: "raw",
			Raw:  string(encoded),
			Options: &postmanBodyOpts{
				Raw: postmanBodyRawOpts{Language: "json"},
			},
		}, "application/json", nil
	}

	if media, ok := content["application/x-www-form-urlencoded"]; ok {
		items := formParamsFromSchema(media.Schema)
		return &postmanBody{
			Mode:       "urlencoded",
			URLEncoded: items,
		}, "application/x-www-form-urlencoded", nil
	}

	if media, ok := content["multipart/form-data"]; ok {
		items := formDataFromSchema(media.Schema)
		return &postmanBody{
			Mode:     "formdata",
			FormData: items,
		}, "", nil
	}

	for contentType, media := range content {
		payload := exampleValueFromSchema(media.Schema, 0)
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return nil, "", err
		}
		return &postmanBody{
			Mode: "raw",
			Raw:  string(encoded),
		}, contentType, nil
	}

	return nil, "", nil
}

func formParamsFromSchema(schemaRef *openapi3.SchemaRef) []postmanBodyKV {
	schema := schemaValue(schemaRef)
	if schema == nil {
		return nil
	}
	keys := sortedSchemaKeys(schema.Properties)
	items := make([]postmanBodyKV, 0, len(keys))
	for _, key := range keys {
		items = append(items, postmanBodyKV{
			Key:   key,
			Value: fmt.Sprintf("%v", normalizeBodyValue(exampleValueFromSchema(schema.Properties[key], 1))),
			Type:  "text",
		})
	}
	return items
}

func formDataFromSchema(schemaRef *openapi3.SchemaRef) []postmanFormData {
	schema := schemaValue(schemaRef)
	if schema == nil {
		return nil
	}
	keys := sortedSchemaKeys(schema.Properties)
	items := make([]postmanFormData, 0, len(keys))
	for _, key := range keys {
		property := schemaValue(schema.Properties[key])
		itemType := "text"
		value := fmt.Sprintf("%v", normalizeBodyValue(exampleValueFromSchema(schema.Properties[key], 1)))
		if property != nil && property.Format == "binary" {
			itemType = "file"
			value = ""
		}
		items = append(items, postmanFormData{
			Key:   key,
			Value: value,
			Type:  itemType,
		})
	}
	return items
}

func authHeadersForOperation(op *openapi3.Operation) []postmanKV {
	if op == nil || op.Security == nil {
		return nil
	}
	headers := []postmanKV{}
	hasUserBearer := false
	hasAdminBearer := false
	hasAdminHeader := false
	for _, requirement := range *op.Security {
		if _, ok := requirement["bearerAuth"]; ok {
			hasUserBearer = true
		}
		if _, ok := requirement["adminBearerAuth"]; ok {
			hasAdminBearer = true
		}
		if _, ok := requirement["xAdminToken"]; ok {
			hasAdminHeader = true
		}
	}
	if hasAdminBearer {
		headers = append(headers, postmanKV{Key: "Authorization", Value: "Bearer {{adminToken}}", Type: "text"})
	}
	if hasUserBearer && !hasAdminBearer {
		headers = append(headers, postmanKV{Key: "Authorization", Value: "Bearer {{userToken}}", Type: "text"})
	}
	if hasAdminHeader {
		headers = append(headers, postmanKV{Key: "X-Admin-Token", Value: "{{adminToken}}", Type: "text", Disabled: true})
	}
	return headers
}

func mergeHeaders(dst *[]postmanKV, src []postmanKV) {
	if len(src) == 0 {
		return
	}
	*dst = append(*dst, src...)
}

func exampleValueFromSchema(schemaRef *openapi3.SchemaRef, depth int) any {
	if depth > 4 {
		return nil
	}
	schema := schemaValue(schemaRef)
	if schema == nil {
		return nil
	}
	if schema.Example != nil {
		return schema.Example
	}
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}
	switch {
	case isSchemaType(schema, "string"):
		return exampleStringValue(schema)
	case isSchemaType(schema, "integer"):
		return 0
	case isSchemaType(schema, "number"):
		return 0
	case isSchemaType(schema, "boolean"):
		return false
	case isSchemaType(schema, "array"):
		if schema.Items != nil {
			return []any{exampleValueFromSchema(schema.Items, depth+1)}
		}
		return []any{}
	case isSchemaType(schema, "object"):
		if len(schema.Properties) == 0 {
			return map[string]any{}
		}
		result := map[string]any{}
		for _, key := range sortedSchemaKeys(schema.Properties) {
			result[key] = exampleValueFromSchema(schema.Properties[key], depth+1)
		}
		return result
	default:
		if len(schema.Properties) > 0 {
			result := map[string]any{}
			for _, key := range sortedSchemaKeys(schema.Properties) {
				result[key] = exampleValueFromSchema(schema.Properties[key], depth+1)
			}
			return result
		}
	}
	return nil
}

func schemaValue(schemaRef *openapi3.SchemaRef) *openapi3.Schema {
	if schemaRef == nil {
		return nil
	}
	return schemaRef.Value
}

func isSchemaType(schema *openapi3.Schema, typ string) bool {
	return schema != nil && schema.Type != nil && schema.Type.Is(typ)
}

func sortedSchemaKeys(properties openapi3.Schemas) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func exampleStringValue(schema *openapi3.Schema) string {
	if schema == nil {
		return ""
	}
	switch schema.Format {
	case "date-time":
		return "2026-03-20T00:00:00Z"
	case "date":
		return "2026-03-20"
	case "email":
		return "demo@example.com"
	case "binary":
		return ""
	default:
		return "示例值"
	}
}

func normalizeBodyValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return ""
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	default:
		return typed
	}
}

func normalizePostmanOptions(opts PostmanOptions) PostmanOptions {
	defaults := DefaultPostmanOptions()
	if strings.TrimSpace(opts.Name) == "" {
		opts.Name = defaults.Name
	}
	if strings.TrimSpace(opts.Description) == "" {
		opts.Description = defaults.Description
	}
	if strings.TrimSpace(opts.BaseURL) == "" {
		opts.BaseURL = defaults.BaseURL
	}
	return opts
}

func translateTagCN(tag string) string {
	if mapped, ok := map[string]string{
		"System":              "系统接口",
		"App":                 "应用公开接口",
		"Auth":                "用户认证",
		"Admin Auth":          "管理员认证",
		"User":                "用户中心",
		"User Settings":       "用户设置",
		"Sign-in":             "签到中心",
		"User Public":         "用户公开接口",
		"Sites":               "站点管理",
		"Roles":               "角色申请",
		"User Audits":         "用户审计",
		"Notifications":       "通知中心",
		"Admin":               "管理端应用",
		"Admin Audits":        "管理端审计",
		"Admin Notifications": "管理端通知",
		"Admin Users":         "管理端用户",
		"Admin Settings":      "管理端设置",
		"Admin System":        "管理端系统",
		"Storage":             "存储管理",
		"Realtime":            "实时通信",
		"Points":              "积分等级",
		"Email":               "邮件系统",
		"Payment":             "支付系统",
		"Public Payment":      "公开支付",
		"Workflow":            "工作流",
		"App Compat":          "兼容接口",
		"API":                 "通用接口",
	}[tag]; ok {
		return mapped
	}
	return tag
}

func localizedOperationName(method string, path string, op *openapi3.Operation) string {
	key := strings.ToUpper(strings.TrimSpace(method)) + " " + path
	if mapped, ok := map[string]string{
		"GET /healthz":                                    "健康检查",
		"GET /readyz":                                     "就绪检查",
		"GET /api/app/public":                             "获取应用公开信息",
		"POST /api/auth/login/password":                   "密码登录",
		"POST /api/auth/register/password":                "密码注册",
		"POST /api/auth/refresh":                          "刷新令牌",
		"POST /api/auth/logout":                           "用户登出",
		"GET /api/admin/auth/me":                          "获取管理员会话",
		"POST /api/admin/auth/login":                      "管理员登录",
		"POST /api/admin/auth/logout":                     "管理员登出",
		"POST /api/user/my":                               "获取我的主页",
		"GET /api/user/profile":                           "获取用户资料",
		"PUT /api/user/profile":                           "更新用户资料",
		"GET /api/user/settings":                          "获取用户设置",
		"PUT /api/user/settings":                          "更新用户设置",
		"GET /api/user/signin/status":                     "获取签到状态",
		"GET /api/user/signin/history":                    "获取签到历史",
		"GET /api/user/signin/history/export":             "导出签到历史",
		"POST /api/user/signin":                           "执行签到",
		"GET /api/notifications":                          "获取通知列表",
		"GET /api/notifications/unread-count":             "获取未读通知数",
		"POST /api/notifications/read":                    "标记通知已读",
		"POST /api/notifications/read-batch":              "批量标记通知已读",
		"POST /api/notifications/clear":                   "清空通知",
		"GET /api/ws":                                     "建立实时连接",
		"POST /api/storage/object-link":                   "生成存储对象链接",
		"POST /api/storage/upload":                        "上传存储对象",
		"GET /api/storage/proxy/{ticket}":                 "代理下载存储对象",
		"GET /api/admin/system/online/stats":              "获取全站在线概览",
		"GET /api/admin/system/online/apps/{appid}":       "获取应用在线统计",
		"GET /api/admin/system/online/apps/{appid}/users": "获取应用在线用户",
	}[key]; ok {
		return mapped
	}

	action := localizedMethodAction(method)
	segments := translatedPathSegments(path)
	if len(segments) == 0 {
		return action + "接口"
	}
	if mappedAction, strip := trailingSegmentAction(segments); mappedAction != "" {
		action = mappedAction
		if strip {
			segments = segments[:len(segments)-1]
		}
	}
	if strings.Contains(path, "/status") {
		action = "获取"
		if len(segments) == 0 || segments[len(segments)-1] != "状态" {
			segments = append(segments, "状态")
		}
	}
	if strings.Contains(path, "/stats") {
		action = "获取"
		if len(segments) == 0 || segments[len(segments)-1] != "统计" {
			segments = append(segments, "统计")
		}
	}
	if strings.Contains(path, "/detail") {
		action = "获取"
		if len(segments) == 0 || segments[len(segments)-1] != "详情" {
			segments = append(segments, "详情")
		}
	}
	if summary := strings.TrimSpace(op.Summary); strings.Contains(summary, "Export") {
		action = "导出"
	}
	return action + strings.Join(compactSegments(segments), "")
}

func localizedMethodAction(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET":
		return "获取"
	case "POST":
		return "提交"
	case "PUT", "PATCH":
		return "更新"
	case "DELETE":
		return "删除"
	default:
		return strings.ToUpper(method)
	}
}

func translatedPathSegments(path string) []string {
	replacer := strings.NewReplacer("{", "", "}", "")
	source := strings.Split(strings.Trim(path, "/"), "/")
	segments := make([]string, 0, len(source))
	for _, segment := range source {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "api" {
			continue
		}
		if strings.HasPrefix(segment, "{") {
			continue
		}
		segment = replacer.Replace(segment)
		if mapped, ok := map[string]string{
			"admin":             "管理端",
			"admins":            "管理员",
			"auth":              "认证",
			"login":             "登录",
			"logout":            "登出",
			"register":          "注册",
			"refresh":           "刷新",
			"password":          "密码",
			"oauth2":            "第三方登录",
			"callback":          "回调",
			"me":                "当前会话",
			"app":               "应用",
			"apps":              "应用",
			"public":            "公开",
			"user":              "用户",
			"users":             "用户",
			"my":                "我的",
			"profile":           "资料",
			"settings":          "设置",
			"signin":            "签到",
			"history":           "历史",
			"status":            "状态",
			"banner":            "横幅",
			"notice":            "公告",
			"notifications":     "通知",
			"unread-count":      "未读数",
			"read":              "已读",
			"read-batch":        "批量已读",
			"read-all":          "全部已读",
			"clear":             "清空",
			"storage":           "存储",
			"upload":            "上传",
			"proxy":             "代理下载",
			"object-link":       "对象链接",
			"system":            "系统",
			"online":            "在线",
			"stats":             "统计",
			"config":            "配置",
			"check-version":     "检查版本",
			"version":           "版本",
			"versions":          "版本",
			"channel":           "渠道",
			"channels":          "渠道",
			"site-list":         "站点列表",
			"site-detail":       "站点详情",
			"site":              "站点",
			"sites":             "站点",
			"role":              "角色",
			"applications":      "申请",
			"application":       "申请",
			"audits":            "审计",
			"sessions":          "会话",
			"session":           "会话",
			"export":            "导出",
			"list":              "列表",
			"detail":            "详情",
			"create":            "创建",
			"update":            "更新",
			"delete":            "删除",
			"get":               "获取",
			"set":               "设置",
			"test":              "测试",
			"reset":             "重置",
			"policy":            "策略",
			"password-policy":   "密码策略",
			"points":            "积分",
			"workflow":          "工作流",
			"email":             "邮件",
			"pay":               "支付",
			"epay":              "易支付",
			"ws":                "实时连接",
			"available":         "可用",
			"daily":             "日签",
			"ranking":           "排行",
			"security":          "安全",
			"test-notification": "测试通知",
			"toggle-pin":        "切换置顶",
			"preview-match":     "预览匹配",
			"user-trend":        "用户趋势",
			"auth-sources":      "认证来源",
			"audit":             "审核",
			"audit-list":        "审核列表",
			"batch-audit":       "批量审核",
			"batch-review":      "批量审核",
			"review":            "审核",
			"statistics":        "统计",
		}[segment]; ok {
			segments = append(segments, mapped)
			continue
		}
		segments = append(segments, segment)
	}
	return segments
}

func trailingSegmentAction(segments []string) (string, bool) {
	if len(segments) == 0 {
		return "", false
	}
	switch segments[len(segments)-1] {
	case "导出":
		return "导出", true
	case "获取":
		return "获取", true
	case "设置":
		return "设置", true
	case "测试":
		return "测试", true
	case "重置":
		return "重置", true
	case "创建":
		return "创建", true
	case "更新":
		return "更新", true
	case "删除":
		return "删除", true
	case "审核":
		return "审核", true
	case "批量审核":
		return "批量审核", true
	case "切换置顶":
		return "切换置顶", true
	case "预览匹配":
		return "预览匹配", true
	case "批量已读":
		return "批量标记已读", true
	case "全部已读":
		return "全部标记已读", true
	case "已读":
		return "标记已读", true
	case "清空":
		return "清空", true
	}
	return "", false
}

func compactSegments(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if len(result) > 0 && result[len(result)-1] == item {
			continue
		}
		result = append(result, item)
	}
	return result
}

func defaultVariableValue(name string) string {
	if mapped, ok := map[string]string{
		"appid":          "{{appid}}",
		"userId":         "1",
		"adminId":        "1",
		"applicationId":  "1",
		"bannerId":       "1",
		"noticeId":       "1",
		"notificationId": "1",
		"tokenHash":      "示例会话哈希",
		"ticket":         "示例下载票据",
		"channelId":      "1",
		"versionId":      "1",
		"siteId":         "1",
		"orderNo":        "示例订单号",
	}[name]; ok {
		return mapped
	}
	return "1"
}

func defaultParameterValue(name string, schema *openapi3.Schema) any {
	switch name {
	case "appid":
		return "{{appid}}"
	case "page":
		return 1
	case "limit", "pageSize":
		return 20
	case "days":
		return 7
	case "category":
		return "basic"
	case "provider":
		return "github"
	case "code":
		return "demo_code"
	case "state":
		return "demo_state"
	case "token":
		return "{{userToken}}"
	case "versionCode":
		return 1
	case "platform":
		return "android"
	case "status":
		return "active"
	case "keyword":
		return "test"
	}
	if schema != nil {
		switch {
		case isSchemaType(schema, "integer"), isSchemaType(schema, "number"):
			return 0
		case isSchemaType(schema, "boolean"):
			return false
		}
	}
	return "示例值"
}
