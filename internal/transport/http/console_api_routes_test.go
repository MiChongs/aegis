package httptransport

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 控制台调的每一条接口，路由表里都必须真有。
//
// 这条测试是照着一个真实故障写的：`/api/admin/auth/register` 的 handler、DTO、
// 审计埋点全都写好了，甚至 router.go 里那段注释就列着「注册」，唯独少了注册路由
// 那一行。于是控制台的注册页每次提交都落到 NoRoute 上，拿回 40400
// 「请求的页面不存在」—— 从前端看像是接口地址写错了，从后端看什么日志都没有，
// 而 handler 的单元测试全绿。
//
// 判据刻意保守：只取**没有插值的字符串字面量**路径。带 `${}` 的模板串要还原成
// gin 的 `:param` 形式才能比对，还原规则一旦有偏差就会误报，
// 而一条会误伤的测试很快就会被加例外、然后失去意义（与 SDK 字段测试同一取舍）。
func TestConsoleAPIPathsAreRegistered(t *testing.T) {
	engine := newTestRouter(t)

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	root := filepath.Join("..", "..", "..", "aegis-console", "src", "lib")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("跳过：找不到控制台源码目录（%v）", err)
	}

	calls, err := collectConsoleAPICalls(root)
	if err != nil {
		t.Fatalf("扫描控制台 API 调用失败：%v", err)
	}
	if len(calls) == 0 {
		t.Fatal("一条控制台 API 调用都没扫到，说明扫描规则失效了，而不是真的没有")
	}

	var missing []string
	for key, where := range calls {
		if !registered[key] {
			missing = append(missing, key+"（"+where+"）")
		}
	}
	sort.Strings(missing)
	for _, item := range missing {
		t.Errorf("控制台调用了未注册的路由：%s", item)
	}
}

var (
	// apiRequest<T>("/api/...", { method: "POST" }) —— 方法在选项对象里，可能不在同一行
	consoleCallPattern = regexp.MustCompile(`apiRequest\s*(?:<[^>]*>)?\s*\(\s*"(/api/[^"$]*)"([\s\S]{0,200})`)
	consoleMethodHint  = regexp.MustCompile(`method\s*:\s*"([A-Z]+)"`)
	// 后视窗口必须在本次调用结束处截断。不截断的话，一个没写 method 的 GET
	// 会读到紧随其后那个函数里的 `method: "POST"`，于是五条只读接口被报成
	// 「POST 未注册」—— 一轮误报就足以让人把整条测试关掉。
	consoleCallEnders = []string{"apiRequest", "\nexport ", "\nfunction "}
)

// truncateAtNextCall 把后视窗口切到下一处调用之前。
func truncateAtNextCall(window string) string {
	cut := len(window)
	for _, ender := range consoleCallEnders {
		if index := strings.Index(window, ender); index >= 0 && index < cut {
			cut = index
		}
	}
	return window[:cut]
}

// collectConsoleAPICalls 返回 `METHOD /path` → 出处文件。
func collectConsoleAPICalls(root string) (map[string]string, error) {
	calls := map[string]string{}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".ts") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range consoleCallPattern.FindAllStringSubmatch(string(source), -1) {
			route := match[1]
			// 只保留纯字面量：带 `?` 查询串的按路径部分算，带插值的上面的正则已经排掉
			if index := strings.IndexByte(route, '?'); index >= 0 {
				route = route[:index]
			}
			method := "GET"
			if hint := consoleMethodHint.FindStringSubmatch(truncateAtNextCall(match[2])); hint != nil {
				method = hint[1]
			}
			key := method + " " + route
			if _, exists := calls[key]; !exists {
				calls[key] = filepath.Base(path)
			}
		}
		return nil
	})

	return calls, err
}
