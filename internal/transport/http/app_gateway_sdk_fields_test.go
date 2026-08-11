package httptransport

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"aegis/internal/service"
)

// Kotlin SDK 传的请求字段名必须与 handler 真正解析的字段名一致。
//
// 路径对齐已经由 TestGatewayCatalogMatchesRegisteredRoutes 与 SDK 覆盖度测试守住，
// 但那只保证「打得到这个接口」。字段名错了同样是这个能力不可用：服务端拿 gin 的
// binding:"required" 挡下来，客户端只看到一个 40000，而错在哪个字段没人说。
//
// 这条测试就是照着这个漏洞写的 —— confirmProfileChange 曾经传 `token`，
// 而 handler 要的是 `field`，于是「绑定邮箱」的最后一步从来没有成功过，
// 路径、方法、鉴权全都对，只有字段名不对。
//
// 判据刻意保守：只检查必填字段有没有出现在 SDK 对应方法体里，多传的不管。
// 宁可漏报也不误报 —— 一条会误伤的测试很快就会被加例外，然后失去意义。
func TestKotlinSDKSendsEveryRequiredRequestField(t *testing.T) {
	source, err := os.ReadFile("../../../sdk/kotlin/src/main/kotlin/dev/aegis/sdk/AegisApi.kt")
	if err != nil {
		t.Fatalf("读取 SDK 源码失败：%v", err)
	}
	blocks := splitKotlinFunctions(string(source))
	// 网关接口的请求模型登记在 gatewayRequestModels，不在 generatedRouteModels ——
	// 后者一条网关路由都没有，拿它查会让这条测试对每个网关接口静默跳过，
	// 也就是一条永远绿的测试。
	models := gatewayRequestModels()

	for _, op := range service.GatewayOperations() {
		if op.Method != "POST" && op.Method != "PUT" {
			continue
		}
		if op.Upload {
			// multipart 上传没有 JSON 请求体可比。
			continue
		}
		model, ok := models[op.Key]
		if !ok || model == nil {
			// 无请求体的 POST（logout / totpEnroll 这类）。
			continue
		}
		required := requiredJSONFields(model, op.Path)
		if len(required) == 0 {
			continue
		}
		body, found := kotlinBodyForCall(blocks, op.Method, op.Path)
		if !found {
			// 覆盖度由 TestKotlinSDKCoversEveryGatewayOperation 单独负责。
			continue
		}
		for _, field := range required {
			if !body[field] {
				t.Errorf("SDK 调用 %s %s（key=%s）时没有传必填字段 %q —— "+
					"服务端会以 40000 拒绝，且不会说是哪个字段",
					op.Method, op.Path, op.Key, field)
			}
		}
	}
}

// requiredJSONFields 取出 DTO 里 binding:"required" 的 json 字段名。
// 路径参数不算：它们从 URL 来，不在请求体里。
func requiredJSONFields(model any, path string) []string {
	value := reflect.ValueOf(model)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	pathParams := map[string]bool{}
	for _, match := range regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(path, -1) {
		pathParams[strings.ToLower(match[1])] = true
	}

	var fields []string
	structType := value.Type()
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !strings.Contains(field.Tag.Get("binding"), "required") {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if pathParams[strings.ToLower(name)] {
			continue
		}
		// appid 由网关从路径上的 appKey 解析，handler 用的是解析结果而不是请求体里的值。
		// 这些 DTO 与旧 /api/auth/* 命名空间共用，那边才需要客户端显式带 appid。
		if strings.EqualFold(name, "appid") {
			continue
		}
		fields = append(fields, name)
	}
	return fields
}

// splitKotlinFunctions 把源码切成一个个函数体，这样字段提取不会窜到相邻方法上。
func splitKotlinFunctions(source string) []string {
	splitter := regexp.MustCompile(`(?m)^\s{4}(?:@\w+\s*)*(?:private\s+)?fun\s`)
	indexes := splitter.FindAllStringIndex(source, -1)
	blocks := make([]string, 0, len(indexes))
	for i, span := range indexes {
		end := len(source)
		if i+1 < len(indexes) {
			end = indexes[i+1][0]
		}
		blocks = append(blocks, source[span[0]:end])
	}
	return blocks
}

// kotlinBodyForCall 找出发起该「方法 + 路径」调用的那个函数体里的所有 `"key" to` 字段名。
//
// 必须连方法一起匹配：/me/settings、/tickets、/pay/orders、/me/passkeys 都是
// GET 与写操作共用同一条路径，只按路径找会命中前面那个 GET 方法，
// 于是取到一个空的字段集，把每个写接口都误报成"什么都没传"。
//
// 找不到的情况会跳过检查：登录类接口经由私有的 login(body, path) 收口，
// 路径与请求体不在同一处字面量上。那部分的覆盖由
// TestKotlinSDKCoversEveryGatewayOperation 负责。
func kotlinBodyForCall(blocks []string, method, path string) (map[string]bool, bool) {
	// `\s*` 在括号之后也要有：SDK 里长参数列表是换行写的
	// （client.call( 换行 "POST", "/tickets/$ticketId/rating",），
	// 要求紧邻会让所有多行调用漏检 —— 那正是 ticketRating 那处字段错误藏身的地方。
	needle := regexp.MustCompile(
		`client\.call\(\s*"` + regexp.QuoteMeta(method) + `",\s*` + kotlinPathLiteral(path))
	keyPattern := regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"\s+to\b`)
	for _, block := range blocks {
		if !needle.MatchString(block) {
			continue
		}
		keys := map[string]bool{}
		for _, match := range keyPattern.FindAllStringSubmatch(block, -1) {
			keys[match[1]] = true
		}
		return keys, true
	}
	return nil, false
}

func kotlinPathLiteral(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segments[i] = `\$\{?[A-Za-z_][A-Za-z0-9_.]*\}?`
			continue
		}
		segments[i] = regexp.QuoteMeta(segment)
	}
	return `"` + strings.Join(segments, "/") + `"`
}
