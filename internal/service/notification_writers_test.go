package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	notificationdomain "aegis/internal/domain/notification"
)

// notificationWriterSources 全部非测试 Go 源码（internal/ 下），路径相对本包。
func notificationWriterSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(path)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// 站内信只能经 NotificationService.Deliver 落库。
//
// 直接调 pg.CreateUserNotification 的写入点拿不到分类与动作、不失效缓存，
// 最要命的是**不发实时事件**：用户要等下次刷新才看得到这条消息。
// 这条测试是照着重构前的现状写的 —— 当时有四处（安全事件、密码重置、异常登录、签到提醒）绕过了服务。
func TestOnlyNotificationServiceCreatesUserNotifications(t *testing.T) {
	allowed := map[string]bool{
		"../service/notification_service.go":                true, // 唯一的调用方
		"../repository/postgres/notification_repository.go": true, // 定义处
	}
	call := regexp.MustCompile(`\.CreateUserNotification\(`)
	found := 0
	for path, text := range notificationWriterSources(t) {
		if !call.MatchString(text) {
			continue
		}
		found++
		if !allowed[path] {
			t.Errorf("%s 直接调用了 CreateUserNotification —— 改走 NotificationService.Deliver（docs/notification-center.md）", path)
		}
	}
	if found == 0 {
		t.Fatal("没扫到任何 CreateUserNotification 调用，扫描规则大概失效了")
	}
}

// 群发的批量写入（一条语句写一千人）同样只能由群发执行器调：它在提交后失效这批人的缓存并推实时事件，
// 别处直接调就又回到了「写进去了、用户却看不见」。
func TestOnlyCampaignRunnerWritesCampaignBatches(t *testing.T) {
	allowed := map[string]bool{
		"../service/notification_campaign.go":                        true, // 唯一的调用方
		"../repository/postgres/notification_campaign_repository.go": true, // 定义处
	}
	call := regexp.MustCompile(`\.(DeliverNotificationCampaignBatch|RecallNotificationCampaignBatch)\(`)
	found := 0
	for path, text := range notificationWriterSources(t) {
		if !call.MatchString(text) {
			continue
		}
		found++
		if !allowed[path] {
			t.Errorf("%s 直接调用了群发批量写入 —— 改走 NotificationService.CreateCampaign（docs/notification-center.md）", path)
		}
	}
	if found == 0 {
		t.Fatal("没扫到群发批量写入的调用，扫描规则大概失效了")
	}
}

// 每个写入点用的 type 都必须在分类映射表里显式登记（或显式落 system），
// 新增写入点时忘了登记，消息会悄悄落进「系统通知」。
func TestNotificationWriterTypesAreCategorized(t *testing.T) {
	// 写入点里 type 不是字面量的地方：值来自调用方或配置，由上游负责
	dynamic := map[string]string{
		"notificationType": "notifySecurityEvent / NotifyUser / SendUserNotification / NotifyHub 的透传参数",
		"cmd.Type":         "管理员群发：由管理员填写",
		"out.Type":         "群发任务（normalizeCampaignInput）：由管理员填写，空时补 announcement",
	}
	patterns := []*regexp.Regexp{
		// notificationdomain.DeliverInput{... Type: <expr>, ...}
		regexp.MustCompile(`DeliverInput\{(?s:[^{}]{0,400}?)\bType:\s*([^,\n}]+)`),
		// SendUserNotification(ctx, session, "<type>", ...)
		regexp.MustCompile(`SendUserNotification\([^,]+,\s*[^,]+,\s*([^,]+),`),
		// s.notifySecurityEvent(ctx, appID, userID, "<type>", ...)
		regexp.MustCompile(`notifySecurityEvent\([^,]+,[^,]+,[^,]+,\s*([^,]+),`),
		// NotifyUser(ctx, appID, userID, "<type>", ...)
		regexp.MustCompile(`\.NotifyUser\([^,]+,[^,]+,[^,]+,\s*([^,]+),`),
	}
	declaration := regexp.MustCompile(`^[A-Za-z_]\w* [A-Za-z_][\w.]*$`)
	literals := map[string]string{}
	for path, text := range notificationWriterSources(t) {
		if strings.Contains(path, "/domain/") {
			continue
		}
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatch(text, -1) {
				expr := strings.TrimSpace(match[1])
				if declaration.MatchString(expr) {
					continue // 函数定义里的形参（notificationType string），不是调用
				}
				switch {
				case strings.HasPrefix(expr, `"`):
					literals[strings.Trim(expr, `"`)] = path
				case strings.HasPrefix(expr, "notificationdomain.Type"):
					literals[expr] = path
				default:
					if _, ok := dynamic[expr]; !ok {
						t.Errorf("%s：站内信 type 用了表达式 %s，既不是字面量也不在测试的透传清单里 —— 先登记它", path, expr)
					}
				}
			}
		}
	}
	constants := map[string]string{
		"notificationdomain.TypeSystem":          notificationdomain.TypeSystem,
		"notificationdomain.TypeAnnouncement":    notificationdomain.TypeAnnouncement,
		"notificationdomain.TypeSecurity":        notificationdomain.TypeSecurity,
		"notificationdomain.TypeAccountDeletion": notificationdomain.TypeAccountDeletion,
		"notificationdomain.TypeItemGrant":       notificationdomain.TypeItemGrant,
		"notificationdomain.TypeLevel":           notificationdomain.TypeLevel,
		"notificationdomain.TypeCosmetic":        notificationdomain.TypeCosmetic,
		"notificationdomain.TypeSignIn":          notificationdomain.TypeSignIn,
		"notificationdomain.TypeAutoSign":        notificationdomain.TypeAutoSign,
		"notificationdomain.TypePointsStore":     notificationdomain.TypePointsStore,
		"notificationdomain.TypeInvite":          notificationdomain.TypeInvite,
		"notificationdomain.TypeLeaderboard":     notificationdomain.TypeLeaderboard,
		"notificationdomain.TypeLottery":         notificationdomain.TypeLottery,
		"notificationdomain.TypeCardKey":         notificationdomain.TypeCardKey,
		"notificationdomain.TypeTicket":          notificationdomain.TypeTicket,
		"notificationdomain.TypeFeedbackReward":  notificationdomain.TypeFeedbackReward,
	}
	names := make([]string, 0, len(literals))
	for name := range literals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := name
		if constant, ok := constants[name]; ok {
			value = constant
		} else if strings.HasPrefix(name, "notificationdomain.") {
			t.Errorf("%s：常量 %s 不在测试的常量表里", literals[name], name)
			continue
		}
		if !notificationdomain.TypeMapped(value) {
			t.Errorf("%s：站内信 type %q 没在 internal/domain/notification/category.go 的映射表里登记", literals[name], value)
		}
	}
	// 重构时确认过的写入点：安全、注销、签到、自动签到、积分商城、邀请、排行榜
	for _, must := range []string{"security", "account_deletion", "signin", "auto_sign", "points_store", "invite", "leaderboard"} {
		if _, ok := literals[must]; !ok {
			t.Errorf("没扫到 type %q 的写入点，扫描规则大概失效了（扫到：%v）", must, names)
		}
	}
}
