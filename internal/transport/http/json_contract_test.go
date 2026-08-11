package httptransport

import (
	"reflect"
	"strings"
	"testing"
	"time"

	notificationdomain "aegis/internal/domain/notification"
	notifydomain "aegis/internal/domain/notify"
	ticketdomain "aegis/internal/domain/ticket"
)

// 出网类型的 JSON 契约守卫。
//
// 起因：notifydomain.Result 漏了 json tag，Go 按字段名序列化成 LatencyMs，
// 前端读 latencyMs 得到 undefined，界面显示「测试消息已发送（undefinedms）」。
// 这类问题编译器和类型检查都发现不了，只能靠契约测试钉住。
//
// 新增任何经 handler 出网的领域类型，都要登记到 apiFacingTypes 里。

var apiFacingTypes = []any{
	// 工单
	ticketdomain.Ticket{},
	ticketdomain.ListResponse{},
	ticketdomain.Stats{},
	ticketdomain.TrendPoint{},
	ticketdomain.AgentStat{},
	ticketdomain.Category{},
	ticketdomain.Group{},
	ticketdomain.SLAPolicy{},
	ticketdomain.QuickReply{},
	ticketdomain.BulkResult{},
	ticketdomain.ScopeInfo{},
	ticketdomain.ActionSet{},
	// 统一通知出口
	notifydomain.Channel{},
	notifydomain.Subscription{},
	notifydomain.Template{},
	notifydomain.Delivery{},
	notifydomain.DeliveryPage{},
	notifydomain.DeliveryStats{},
	notifydomain.ChannelKindMeta{},
	notifydomain.EventMeta{},
	notifydomain.Result{}, // ← 本次修复的类型
	// 管理员收件箱
	notificationdomain.AdminInboxItem{},
	notificationdomain.AdminInboxPage{},
	notificationdomain.AdminInboxMutationResult{},
}

func TestAPIFacingTypesUseCamelCaseJSONTags(t *testing.T) {
	for _, sample := range apiFacingTypes {
		typ := reflect.TypeOf(sample)
		assertCamelCaseTags(t, typ, typ.Name(), map[reflect.Type]bool{})
	}
}

// assertCamelCaseTags 递归校验：每个导出字段都要有 json tag，且首字母小写。
func assertCamelCaseTags(t *testing.T, typ reflect.Type, path string, visited map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	// time.Time 有自己的 MarshalJSON，不适用字段规则
	if typ == reflect.TypeOf(time.Time{}) {
		return
	}
	if visited[typ] {
		return
	}
	visited[typ] = true

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, ok := field.Tag.Lookup("json")
		if !ok {
			t.Errorf("%s.%s 缺少 json tag —— 出网字段会变成 PascalCase，前端读不到", path, field.Name)
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue // 显式不出网
		}
		if name == "" {
			t.Errorf("%s.%s 的 json tag 没有字段名（写成了 `json:\",omitempty\"`）", path, field.Name)
			continue
		}
		if first := rune(name[0]); first >= 'A' && first <= 'Z' {
			t.Errorf("%s.%s 的 json tag %q 不是 camelCase", path, field.Name, name)
		}
		assertCamelCaseTags(t, field.Type, path+"."+field.Name, visited)
	}
}

// TestNotifyResultSerialisesLatency 直接盯住出问题的那个字段：
// 前端 `${result.latencyMs}ms` 依赖这个键名存在且为数字。
func TestNotifyResultSerialisesLatency(t *testing.T) {
	typ := reflect.TypeOf(notifydomain.Result{})
	field, ok := typ.FieldByName("LatencyMs")
	if !ok {
		t.Fatal("Result 缺少 LatencyMs 字段")
	}
	if got := field.Tag.Get("json"); got != "latencyMs" {
		t.Fatalf("LatencyMs 的 json tag 应为 latencyMs，实际 %q", got)
	}
	statusField, _ := typ.FieldByName("Status")
	if got := statusField.Tag.Get("json"); got != "status" {
		t.Fatalf("Status 的 json tag 应为 status，实际 %q", got)
	}
}
