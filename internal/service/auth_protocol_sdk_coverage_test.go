package service

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 官方 Kotlin SDK 必须覆盖接口目录里的每一条 operation。
//
// 目录已经由 TestGatewayCatalogMatchesRegisteredRoutes 与真实路由双向钉死，
// 但那条测试管不到 SDK：后端新增一条网关接口、目录跟着加一条之后，SDK 依旧编译通过、
// 测试依旧全绿，只是那个能力对所有用官方客户端的接入方**不存在**。
// 这种缺失没有任何报错会提示，只会表现为「文档里有、SDK 里找不到方法」。
//
// 这里直接读 SDK 源码找路径字面量。做法粗糙但抓的正是要抓的东西：
// 路径是客户端与服务端之间唯一必须逐字一致的东西，SDK 里没有那串字面量，
// 就说明没有任何方法能调到它。
func TestKotlinSDKCoversEveryGatewayOperation(t *testing.T) {
	sources := []string{
		"../../sdk/kotlin/src/main/kotlin/dev/aegis/sdk/AegisApi.kt",
		"../../sdk/kotlin/src/main/kotlin/dev/aegis/sdk/AegisClient.kt",
	}
	var combined strings.Builder
	for _, path := range sources {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 SDK 源码失败 %s：%v", path, err)
		}
		combined.Write(data)
	}
	sdk := combined.String()

	// 客户端调不到、也不该调的两条：
	//   config      —— SDK 用专门的 config() 方法，路径在那里是拼出来的完整 URL
	//   oauthCallback —— 由第三方重定向浏览器发起，客户端根本没有机会调用它
	exempt := map[string]string{
		"config":        "SDK 用 AegisClient.config() 单独实现，路径以完整 URL 形式拼接",
		"oauthCallback": "浏览器回跳入口，客户端不发起",
	}

	for _, op := range GatewayOperations() {
		if reason, skip := exempt[op.Key]; skip {
			t.Logf("跳过 %s：%s", op.Key, reason)
			continue
		}
		pattern := kotlinPathPattern(op.Path)
		matched, err := regexp.MatchString(pattern, sdk)
		if err != nil {
			t.Fatalf("构造匹配式失败 %s：%v", op.Path, err)
		}
		if !matched {
			t.Errorf("目录里的 %s %s（key=%s）在 Kotlin SDK 里找不到对应路径，"+
				"这个能力对所有官方客户端的接入方都不存在", op.Method, op.Path, op.Key)
		}
	}
}

// kotlinPathPattern 把目录里的 `/tickets/{ticketId}/rating`
// 转成能匹配 SDK 里 `"/tickets/$ticketId/rating"` 的正则。
// 参数名两边不要求一致 —— 要钉的是路径形状，不是变量怎么起名。
func kotlinPathPattern(path string) string {
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

// 反过来的方向：SDK 不应该调目录里没有的路径 —— 那种调用一定是 404。
func TestKotlinSDKCallsNothingOutsideTheCatalog(t *testing.T) {
	data, err := os.ReadFile("../../sdk/kotlin/src/main/kotlin/dev/aegis/sdk/AegisApi.kt")
	if err != nil {
		t.Fatalf("读取 SDK 源码失败：%v", err)
	}

	catalog := map[string]bool{}
	for _, op := range GatewayOperations() {
		catalog[normalizeSDKPath(op.Path)] = true
	}

	// client.call("METHOD", "/path" 与 client.upload("/path"
	callPattern := regexp.MustCompile(`client\.(?:call\("[A-Z]+",\s*|upload\()"([^"]+)"`)
	for _, match := range callPattern.FindAllStringSubmatch(string(data), -1) {
		path := normalizeSDKPath(match[1])
		if !catalog[path] {
			t.Errorf("SDK 调用了目录里没有的路径 %q —— 这条请求在服务端是 404", match[1])
		}
	}
}

// normalizeSDKPath 把两侧的参数占位统一成 `{}`，只比较路径形状。
func normalizeSDKPath(path string) string {
	path = regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(path, "{}")
	path = regexp.MustCompile(`\$\{[^}]*\}`).ReplaceAllString(path, "{}")
	path = regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*`).ReplaceAllString(path, "{}")
	return path
}
