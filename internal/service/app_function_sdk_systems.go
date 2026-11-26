package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"

	functiondomain "aegis/internal/domain/appfunction"
	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/cloudstorage"
	"aegis/internal/domain/itemgrant"
	paymentdomain "aegis/internal/domain/payment"
	pointdomain "aegis/internal/domain/points"
	"aegis/internal/domain/pointstore"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 平台各子系统的 SDK 绑定（能力目录见 domain/appfunction/capabilities_systems.go）。
//
// 一条总原则：每个写操作都落到那个系统**面向终端用户的同一个入口**，而不是绕到仓储层改数字 ——
// 抽奖走 LotteryService.Draw、兑换走 PointsStoreService.Redeem、签到走 SignInService……
// 于是风控、幂等、通知、排行榜口径与用户自己在客户端上按那个按钮完全一致。
// 幂等键一律由 eventId 派生：同一次调用重放时，这些入口各自的幂等照样生效。

const (
	maxFunctionCallDepth = 3
	maxHistoryLimit      = 50
	maxBoardLimit        = 100
	maxGrantItems        = itemgrant.MaxItemsPerNode
)

// ── 公共小工具 ──

// toJS 经 JSON 往返转成脚本值：字段名就是客户端 API 的 json tag，脚本作者看到的与客户端文档一致。
func toJS(vm *goja.Runtime, value any) goja.Value {
	if value == nil {
		return goja.Null()
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		throw(vm, err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		throw(vm, err)
	}
	return vm.ToValue(decoded)
}

// toMap 同上，但留在 Go 侧供挑字段、删敏感字段。
func toMap(value any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(encoded, &out)
	return out
}

// session 为面向终端用户的服务造一个会话：它们只读 AppID / UserID（与少数几个客户端字段）。
func (s *ScriptSDK) session(userID int64) *authdomain.Session {
	return &authdomain.Session{
		AppID: s.appID, UserID: userID,
		DeviceID: s.caller.DeviceID, UserAgent: s.caller.UserAgent,
	}
}

// requestID 由 eventId 派生的幂等键：同一次调用的第 n 个写操作恒得到同一个键，
// eventId 重放（或 40993 之后的接管重跑）时各系统自己的幂等就能认出它。
func (s *ScriptSDK) requestID(tag string) string {
	s.requestSeq++
	return "fn-" + s.eventID + "-" + tag + strconv.Itoa(s.requestSeq)
}

// targetUser 解析读操作的目标用户（参数位 index，省略即调用者），并确认属于本应用。
func (s *ScriptSDK) targetUser(vm *goja.Runtime, call goja.FunctionCall, index int) int64 {
	var userID int64
	if len(call.Arguments) > index && !goja.IsUndefined(call.Argument(index)) && !goja.IsNull(call.Argument(index)) {
		id := call.Argument(index).ToInteger()
		if id >= userPublicIDMin {
			resolved, err := s.deps.PG.FindUserIDByPublicID(s.ctx, s.appID, id)
			if err != nil {
				return 0
			}
			return resolved
		}
		userID = id
		if userID == s.callerUserID() {
			return userID
		}
		user, err := s.deps.PG.GetUserByID(s.ctx, userID)
		if err != nil {
			throw(vm, err)
		}
		if user == nil || user.AppID != s.appID {
			return 0
		}
		return userID
	}
	return s.callerUserID()
}

func argString(call goja.FunctionCall, index int) string {
	if len(call.Arguments) <= index {
		return ""
	}
	value := call.Argument(index)
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return ""
	}
	return strings.TrimSpace(value.String())
}

// argObject 把一个可选的对象参数导出成 map（不是对象时返回空 map）。
func argObject(call goja.FunctionCall, index int) map[string]any {
	if len(call.Arguments) <= index {
		return map[string]any{}
	}
	value := call.Argument(index)
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return map[string]any{}
	}
	if exported, ok := value.Export().(map[string]any); ok {
		return exported
	}
	return map[string]any{}
}

func optString(options map[string]any, key string) string {
	if value, ok := options[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func optInt(options map[string]any, key string, fallback int64) int64 {
	value, ok := options[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func clampSDKLimit(value, fallback, maximum int64) int {
	if value <= 0 {
		value = fallback
	}
	return int(min(value, maximum))
}

// missingDep 能力依赖的服务没装配时在绑定那一刻点名，而不是等脚本调用时空指针。
func missingDep(capability, name string) error {
	return fmt.Errorf("能力 %s 依赖的%s服务未装配", capability, name)
}

func timeString(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.Format(time.RFC3339)
}

// ── points.read ─────────────────────────────────────────────────────

func (s *ScriptSDK) bindPointsRead(vm *goja.Runtime, object *goja.Object) error {
	if err := object.Set("balance", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.targetUser(vm, call, 0)
		if userID <= 0 {
			return vm.ToValue(0)
		}
		user, err := s.deps.PG.GetUserByID(s.ctx, userID)
		if err != nil {
			throw(vm, err)
		}
		if user == nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(user.Integral)
	}); err != nil {
		return err
	}
	return object.Set("history", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.requireUser(vm)
		options := argObject(call, 0)
		limit := clampSDKLimit(optInt(options, "limit", 20), 20, maxHistoryLimit)
		var (
			items []pointdomain.Transaction
			err   error
		)
		if optString(options, "kind") == "experience" {
			items, _, err = s.deps.Points.ListExperienceTransactions(s.ctx, s.session(userID), 1, limit)
		} else {
			items, _, err = s.deps.Points.ListIntegralTransactions(s.ctx, s.session(userID), 1, limit)
		}
		if err != nil {
			throw(vm, err)
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, map[string]any{
				"transactionNo": item.TransactionNo, "type": item.Type, "category": item.Category,
				"amount": item.Amount, "balanceBefore": item.BalanceBefore, "balanceAfter": item.BalanceAfter,
				"title": item.Title, "description": item.Description, "createdAt": item.CreatedAt.Format(time.RFC3339),
			})
		}
		return vm.ToValue(out)
	})
}

// ── level.read / level.write ────────────────────────────────────────

func (s *ScriptSDK) bindLevelNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Points == nil {
		return missingDep(CapLevelRead, "积分")
	}
	if s.has(CapLevelRead) {
		if err := object.Set("get", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.targetUser(vm, call, 0)
			if userID <= 0 {
				return goja.Null()
			}
			return s.levelValue(vm, userID)
		}); err != nil {
			return err
		}
	}
	if !s.has(CapLevelWrite) {
		return nil
	}
	return object.Set("addExperience", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		amount := call.Argument(0).ToInteger()
		if amount <= 0 {
			panic(vm.ToValue("经验值必须为正数"))
		}
		reason := argString(call, 1)
		if reason == "" {
			reason = "远程函数 " + s.functionName
		}
		if s.dryRun {
			profile, err := s.deps.Points.GetMyLevel(s.ctx, s.session(userID))
			experience, level := amount, 0
			if err == nil && profile != nil {
				experience += profile.LevelInfo.TotalExperience
				level = profile.LevelInfo.CurrentLevel
			}
			s.recordEffect(CapLevelWrite, map[string]any{"userId": userID, "amount": amount, "reason": reason, "after": experience})
			return vm.ToValue(map[string]any{"experience": experience, "level": level, "levelChanged": false, "simulated": true})
		}
		result, err := s.deps.Points.AdjustUserExperience(s.ctx, userID, s.appID, amount, reason,
			pointdomain.AdminAdjustOptions{AdminAccount: s.operatorLabel(), ClientIP: s.caller.ClientIP, UserAgent: s.caller.UserAgent})
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect(CapLevelWrite, map[string]any{
			"userId": userID, "amount": amount, "reason": reason, "after": result.AfterAmount,
			"levelChanged": result.LevelChanged, "level": result.NewLevel,
		})
		return vm.ToValue(map[string]any{
			"experience": result.AfterAmount, "level": result.NewLevel, "levelChanged": result.LevelChanged,
		})
	})
}

func (s *ScriptSDK) levelValue(vm *goja.Runtime, userID int64) goja.Value {
	profile, err := s.deps.Points.GetMyLevel(s.ctx, s.session(userID))
	if err != nil {
		throw(vm, err)
	}
	if profile == nil {
		return goja.Null()
	}
	info := profile.LevelInfo
	return vm.ToValue(map[string]any{
		"level": info.CurrentLevel, "levelName": info.CurrentLevelName,
		"experience": info.TotalExperience, "expInLevel": info.ExpInCurrentLevel,
		"expToNext": info.ExpToNextLevel, "progress": info.LevelProgress,
		"nextLevel": info.NextLevel, "nextLevelName": info.NextLevelName,
		"isMax": info.IsMaxLevel, "maxLevel": info.MaxLevel, "multiplier": info.ExpMultiplier,
	})
}

// ── rights.read / rights.grant / rights.consume ─────────────────────

func (s *ScriptSDK) bindRightsNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.has(CapRightsRead) {
		if err := object.Set("summary", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.targetUser(vm, call, 0)
			if userID <= 0 {
				return goja.Null()
			}
			return vm.ToValue(s.rightsSummary(vm, userID))
		}); err != nil {
			return err
		}
		if err := object.Set("has", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			feature := argString(call, 0)
			userID := s.targetUser(vm, call, 1)
			if feature == "" || userID <= 0 {
				return vm.ToValue(false)
			}
			entitlement := s.resolveEntitlement(vm, userID)
			return vm.ToValue(entitlement != nil && entitlement.HasFeature(feature))
		}); err != nil {
			return err
		}
		if err := object.Set("featureTime", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			if s.deps.FeatureTime == nil {
				throw(vm, missingDep(CapRightsRead, "功能时长"))
			}
			feature := argString(call, 0)
			userID := s.targetUser(vm, call, 1)
			if feature == "" {
				panic(vm.ToValue("请指定功能标识"))
			}
			if userID <= 0 {
				return goja.Null()
			}
			status, err := s.deps.FeatureTime.Status(s.ctx, s.appID, userID, feature)
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, status)
		}); err != nil {
			return err
		}
		if err := object.Set("catalog", func(goja.FunctionCall) goja.Value {
			out := make([]map[string]any, 0, 20)
			for _, spec := range itemgrant.ItemCatalog() {
				if spec.Type == itemgrant.ItemManual {
					continue
				}
				out = append(out, map[string]any{
					"type": spec.Type, "label": spec.Label, "hint": spec.Hint, "group": spec.Group,
					"unit": spec.Unit, "min": spec.Min, "max": spec.Max,
				})
			}
			return vm.ToValue(out)
		}); err != nil {
			return err
		}
	}
	if s.has(CapRightsGrant) {
		if err := object.Set("grant", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, true)
			return s.grantRights(vm, call)
		}); err != nil {
			return err
		}
	}
	if s.has(CapRightsConsume) {
		if s.deps.FeatureTime == nil {
			return missingDep(CapRightsConsume, "功能时长")
		}
		if err := object.Set("consumeTime", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, true)
			userID := s.requireUser(vm)
			feature := argString(call, 0)
			seconds := call.Argument(1).ToInteger()
			if feature == "" || seconds <= 0 {
				panic(vm.ToValue("请指定功能标识与正数秒数"))
			}
			if s.dryRun {
				status, err := s.deps.FeatureTime.Status(s.ctx, s.appID, userID, feature)
				if err != nil {
					throw(vm, err)
				}
				s.recordEffect(CapRightsConsume, map[string]any{"userId": userID, "feature": feature, "seconds": seconds})
				balance := status.BalanceSeconds
				if !status.Exempt {
					balance -= seconds
				}
				return vm.ToValue(map[string]any{"exempt": status.Exempt, "seconds": seconds, "balanceSeconds": balance, "simulated": true})
			}
			lease, err := s.deps.FeatureTime.Lease(s.ctx, s.appID, userID, feature, int(seconds))
			if err != nil {
				throw(vm, err)
			}
			if lease.Exempt {
				return vm.ToValue(map[string]any{"exempt": true, "seconds": 0, "balanceSeconds": lease.BalanceSeconds})
			}
			settled, err := s.deps.FeatureTime.Settle(s.ctx, s.appID, userID, lease.LeaseNo, int(seconds))
			if err != nil {
				throw(vm, err)
			}
			s.recordEffect(CapRightsConsume, map[string]any{
				"userId": userID, "feature": feature, "seconds": settled.UsedSeconds, "leaseNo": lease.LeaseNo,
			})
			return vm.ToValue(map[string]any{"exempt": false, "seconds": settled.UsedSeconds, "balanceSeconds": settled.BalanceSeconds})
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *ScriptSDK) rightsSummary(vm *goja.Runtime, userID int64) map[string]any {
	out := map[string]any{
		"userId": userID, "vip": false, "vipLifetime": false, "features": []string{},
		"unlocks": []map[string]any{}, "adFree": false, "lotteryTickets": 0,
	}
	if entitlement := s.resolveEntitlement(vm, userID); entitlement != nil {
		out["vip"] = entitlement.IsVIP
		out["vipLifetime"] = entitlement.IsLifetime
		if expire := timeString(entitlement.ExpireAt); expire != nil {
			out["vipExpireAt"] = expire
		}
		if entitlement.Features != nil {
			out["features"] = entitlement.Features
		}
		unlocks := make([]map[string]any, 0, len(entitlement.Unlocks))
		for _, unlock := range entitlement.Unlocks {
			row := map[string]any{"tag": unlock.Tag, "permanent": unlock.Permanent}
			if expire := timeString(unlock.ExpireAt); expire != nil {
				row["expireAt"] = expire
			}
			unlocks = append(unlocks, row)
		}
		out["unlocks"] = unlocks
		out["adFree"] = entitlement.AdFree
	}
	if until, err := s.deps.PG.AdFreePassUntil(s.ctx, s.appID, userID); err == nil && until != nil && until.After(time.Now()) {
		out["adFree"] = true
		out["adFreeUntil"] = until.Format(time.RFC3339)
	}
	if account, err := s.deps.PG.LotteryTicketAccount(s.ctx, s.appID, userID); err == nil {
		out["lotteryTickets"] = account.Balance
	}
	return out
}

// grantRights aegis.rights.grant(items, { key, title })。
func (s *ScriptSDK) grantRights(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	userID := s.requireUser(vm)
	items, err := s.parseGrantItems(call.Argument(0))
	if err != nil {
		throw(vm, err)
	}
	options := argObject(call, 1)
	key := optString(options, "key")
	if key == "" {
		s.grantSeq++
		key = "event:" + s.eventID + ":" + strconv.Itoa(s.grantSeq)
	}
	if len([]rune(key)) > 120 {
		panic(vm.ToValue("幂等键不能超过 120 个字符"))
	}
	title := optString(options, "title")
	if title == "" {
		title = "远程函数 " + s.functionName
	}
	refs, err := s.deps.PG.AppFunctionItemGrantRefs(s.ctx, s.appID)
	if err != nil {
		throw(vm, err)
	}
	described := make([]string, 0, len(items))
	for _, item := range items {
		if err := checkGrantRef(item, refs); err != nil {
			throw(vm, err)
		}
		described = append(described, itemgrant.DescribeItem(item, refs))
	}
	s.recordEffect(CapRightsGrant, map[string]any{"userId": userID, "key": key, "title": title, "items": items, "described": described})
	if s.dryRun {
		results := make([]map[string]any, 0, len(items))
		for i, item := range items {
			spec, _ := itemgrant.FindItemSpec(item.Type)
			results = append(results, map[string]any{"type": item.Type, "label": spec.Label, "detail": described[i]})
		}
		return vm.ToValue(map[string]any{"grantNo": "", "replayed": false, "simulated": true, "results": results})
	}
	result, err := s.deps.PG.GrantAppFunctionItems(s.ctx, pgrepo.AppFunctionGrantInput{
		AppID: s.appID, FunctionID: s.functionID, Function: s.functionName, UserID: userID,
		EventID: s.eventID, Key: key, Title: title, Items: items,
	})
	if err != nil {
		throw(vm, err)
	}
	if !result.Replayed && s.deps.Realtime != nil {
		// 与发放规则同一个事件：客户端据此刷新会员、积分、权益
		_ = s.deps.Realtime.PublishUserEvent(s.ctx, s.appID, userID, "rights.granted", map[string]any{
			"grantNo": result.GrantNo, "function": s.functionName,
		})
	}
	return toJS(vm, result)
}

// parseGrantItems 接受单个物品或物品数组；人工发放（manual）不开放 —— 远程函数没有管理员去处理它。
func (s *ScriptSDK) parseGrantItems(value goja.Value) ([]itemgrant.Item, error) {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, errors.New("请传入要发放的物品")
	}
	encoded, err := json.Marshal(value.Export())
	if err != nil {
		return nil, err
	}
	var items []itemgrant.Item
	if strings.HasPrefix(strings.TrimSpace(string(encoded)), "[") {
		if err := json.Unmarshal(encoded, &items); err != nil {
			return nil, fmt.Errorf("物品格式无效：%w", err)
		}
	} else {
		var item itemgrant.Item
		if err := json.Unmarshal(encoded, &item); err != nil {
			return nil, fmt.Errorf("物品格式无效：%w", err)
		}
		items = []itemgrant.Item{item}
	}
	if len(items) == 0 {
		return nil, errors.New("请传入要发放的物品")
	}
	if len(items) > maxGrantItems {
		return nil, fmt.Errorf("一次最多发放 %d 项物品", maxGrantItems)
	}
	for i := range items {
		items[i].AmountExpr = ""
		items[i] = itemgrant.NormalizeItem(items[i])
		if items[i].Type == itemgrant.ItemManual {
			return nil, errors.New("远程函数不能发放人工物品（manual）")
		}
		if err := itemgrant.ValidateItem(items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// checkGrantRef 引用的套餐 / 功能 / 装扮必须存在：在发放前说清楚，比整笔回滚之后回一句「引用不存在」好排查。
func checkGrantRef(item itemgrant.Item, refs itemgrant.Refs) error {
	switch item.Type {
	case itemgrant.ItemVipPlan:
		if _, ok := refs.Plans[item.RefID]; !ok {
			return fmt.Errorf("会员套餐 %d 不存在或已停用", item.RefID)
		}
	case itemgrant.ItemFeature, itemgrant.ItemFeatureTime:
		if _, ok := refs.Features[item.Tag]; !ok {
			return fmt.Errorf("功能 %q 不存在", item.Tag)
		}
	case itemgrant.ItemCosmetic:
		if _, ok := refs.Cosmetics[item.RefID]; !ok {
			return fmt.Errorf("装扮 %d 不存在", item.RefID)
		}
	}
	return nil
}

// ── signin.read / signin.write ──────────────────────────────────────

func (s *ScriptSDK) bindSignInNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.SignIn == nil {
		return missingDep(CapSignInRead, "签到")
	}
	if s.has(CapSignInRead) {
		if err := object.Set("status", func(goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			status, err := s.deps.SignIn.GetStatus(s.ctx, s.session(userID))
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, status)
		}); err != nil {
			return err
		}
	}
	if !s.has(CapSignInWrite) {
		return nil
	}
	return object.Set("checkIn", func(goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		if s.dryRun {
			status, err := s.deps.SignIn.GetStatus(s.ctx, s.session(userID))
			if err != nil {
				throw(vm, err)
			}
			s.recordEffect(CapSignInWrite, map[string]any{"userId": userID})
			return vm.ToValue(map[string]any{
				"alreadySigned": status.TodaySigned, "consecutiveDays": status.ConsecutiveDays,
				"integral": status.CurrentReward.IntegralReward, "experience": status.CurrentReward.ExperienceReward,
				"simulated": true,
			})
		}
		result, err := s.deps.SignIn.SignInForUser(s.ctx, userID, s.appID, "function", s.caller.UserAgent, s.caller.ClientIP, "")
		if err != nil {
			throw(vm, err)
		}
		if !result.AlreadySigned {
			s.recordEffect(CapSignInWrite, map[string]any{
				"userId": userID, "consecutiveDays": result.Record.ConsecutiveDays,
				"integral": result.Reward.IntegralReward, "experience": result.Reward.ExperienceReward,
			})
		}
		return vm.ToValue(map[string]any{
			"alreadySigned": result.AlreadySigned, "consecutiveDays": result.Record.ConsecutiveDays,
			"integral": result.Reward.IntegralReward, "experience": result.Reward.ExperienceReward,
		})
	})
}

// ── invite.read ─────────────────────────────────────────────────────

func (s *ScriptSDK) bindInviteNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Invite == nil {
		return missingDep(CapInviteRead, "邀请")
	}
	return object.Set("center", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.requireUser(vm)
		center, err := s.deps.Invite.Center(s.ctx, s.appID, userID)
		if err != nil {
			throw(vm, err)
		}
		out := toMap(center)
		// 邀请人的内部 ID 在客户端视图里是隐藏的，脚本要拿它做「给邀请人也发一份」，单独补上
		out["inviter"] = nil
		if center.Bind.Bound != nil && center.Bind.Bound.Inviter != nil {
			inviter := center.Bind.Bound.Inviter
			out["inviter"] = map[string]any{
				"userId": inviter.UserID, "code": center.Bind.Bound.Code, "nickname": inviter.Nickname,
			}
		}
		return vm.ToValue(out)
	})
}

// ── leaderboard.read ────────────────────────────────────────────────

func (s *ScriptSDK) bindLeaderboardNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Leaderboard == nil {
		return missingDep(CapLeaderboardRead, "排行榜")
	}
	if err := object.Set("boards", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		home, err := s.deps.Leaderboard.Boards(s.ctx, s.appID, s.callerUserID())
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, home)
	}); err != nil {
		return err
	}
	return object.Set("board", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		key := argString(call, 0)
		if key == "" {
			panic(vm.ToValue("请指定榜单键"))
		}
		limit := 20
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			limit = clampSDKLimit(call.Argument(1).ToInteger(), 20, maxBoardLimit)
		}
		page, err := s.deps.Leaderboard.Board(s.ctx, s.appID, s.callerUserID(), key, 1, limit)
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, page)
	})
}

// ── lottery.read / lottery.draw ─────────────────────────────────────

func (s *ScriptSDK) bindLotteryNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Lottery == nil {
		return missingDep(CapLotteryRead, "抽奖")
	}
	if s.has(CapLotteryRead) {
		if err := object.Set("activities", func(goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			list, err := s.deps.Lottery.List(s.ctx, s.appID)
			if err != nil {
				throw(vm, err)
			}
			out := make([]map[string]any, 0, len(list.Activities))
			for _, item := range list.Activities {
				row := toMap(item)
				out = append(out, map[string]any{
					"id": row["id"], "name": row["name"], "description": row["description"],
					"startTime": row["startTime"], "endTime": row["endTime"],
				})
			}
			return vm.ToValue(out)
		}); err != nil {
			return err
		}
		if err := object.Set("state", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			state, err := s.deps.Lottery.State(s.ctx, s.appID, userID, call.Argument(0).ToInteger())
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, state)
		}); err != nil {
			return err
		}
		if err := object.Set("tickets", func(goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			account, err := s.deps.PG.LotteryTicketAccount(s.ctx, s.appID, userID)
			if err != nil {
				throw(vm, err)
			}
			return vm.ToValue(map[string]any{
				"balance": account.Balance, "gift": account.Tickets.Gift, "purchased": account.Tickets.Purchased,
				"totalGranted": account.TotalGranted, "totalConsumed": account.TotalConsumed,
			})
		}); err != nil {
			return err
		}
	}
	if !s.has(CapLotteryDraw) {
		return nil
	}
	return object.Set("draw", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		activityID := call.Argument(0).ToInteger()
		if activityID <= 0 {
			panic(vm.ToValue("请指定抽奖活动"))
		}
		options := argObject(call, 1)
		source := optString(options, "source")
		count := optInt(options, "count", 1)
		if count < 1 || count > 10 {
			panic(vm.ToValue("连抽次数需要在 1–10 之间"))
		}
		s.recordEffect(CapLotteryDraw, map[string]any{"userId": userID, "activityId": activityID, "source": source, "count": count})
		if s.dryRun {
			return vm.ToValue(map[string]any{"draws": []any{}, "replayed": false, "simulated": true})
		}
		request := LotteryDrawRequest{
			AppID: s.appID, UserID: userID, ActivityID: activityID, Source: source,
			RequestID: s.requestID("draw"), DeviceID: s.caller.DeviceID, ClientIP: s.caller.ClientIP, UserAgent: s.caller.UserAgent,
		}
		if count == 1 {
			result, err := s.deps.Lottery.Draw(s.ctx, request)
			if err != nil {
				throw(vm, err)
			}
			out := toMap(result)
			out["draws"] = []any{out["draw"]}
			return vm.ToValue(out)
		}
		result, err := s.deps.Lottery.DrawBatch(s.ctx, LotteryBatchRequest{LotteryDrawRequest: request, Count: int(count)})
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, result)
	})
}

// ── store.read / store.redeem ───────────────────────────────────────

func (s *ScriptSDK) bindStoreNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Store == nil {
		return missingDep(CapStoreRead, "积分商城")
	}
	if s.has(CapStoreRead) {
		if err := object.Set("items", func(goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			store, err := s.deps.Store.Store(s.ctx, s.appID, s.callerUserID())
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, store)
		}); err != nil {
			return err
		}
	}
	if !s.has(CapStoreRedeem) {
		return nil
	}
	return object.Set("redeem", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		itemID := call.Argument(0).ToInteger()
		if itemID <= 0 {
			panic(vm.ToValue("请指定商品"))
		}
		s.recordEffect(CapStoreRedeem, map[string]any{"userId": userID, "itemId": itemID})
		if s.dryRun {
			return vm.ToValue(map[string]any{"redemptionNo": "", "itemTitle": "", "cost": 0, "status": "simulated",
				"balance": 0, "replayed": false, "simulated": true})
		}
		result, err := s.deps.Store.Redeem(s.ctx, pointstore.RedeemInput{
			AppID: s.appID, UserID: userID, ItemID: itemID, RequestID: s.requestID("redeem"),
			DeviceID: s.caller.DeviceID, ClientIP: s.caller.ClientIP, UserAgent: s.caller.UserAgent,
		})
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(map[string]any{
			"redemptionNo": result.Redemption.RedemptionNo, "itemTitle": result.Redemption.ItemTitle,
			"cost": result.Redemption.Cost, "status": result.Redemption.Status,
			"balance": result.Balance, "replayed": result.Replayed,
		})
	})
}

// ── cardkey.redeem ──────────────────────────────────────────────────

func (s *ScriptSDK) bindCardKeyNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.CardKeys == nil {
		return missingDep(CapCardKeyRedeem, "卡密")
	}
	return object.Set("redeem", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		code := argString(call, 0)
		if code == "" {
			panic(vm.ToValue("请传入卡密"))
		}
		masked := code
		if len(masked) > 6 {
			masked = masked[:3] + strings.Repeat("*", len(masked)-6) + masked[len(masked)-3:]
		}
		// 卡密本身不进 effects：调用审计是给管理员看的，没必要在那里留一份可用的卡密
		s.recordEffect(CapCardKeyRedeem, map[string]any{"userId": userID, "code": masked})
		if s.dryRun {
			return vm.ToValue(map[string]any{"code": masked, "kind": "", "batchName": "", "results": []any{}, "simulated": true})
		}
		result, err := s.deps.CardKeys.Redeem(s.ctx, CardKeyClientContext{
			AppID: s.appID, UserID: userID, DeviceID: s.caller.DeviceID,
			ClientIP: s.caller.ClientIP, UserAgent: s.caller.UserAgent,
		}, code)
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, result)
	})
}

// ── cosmetic.read / cosmetic.write ──────────────────────────────────

func (s *ScriptSDK) bindCosmeticNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Cosmetics == nil {
		return missingDep(CapCosmeticRead, "装扮")
	}
	if s.has(CapCosmeticRead) {
		if err := object.Set("owned", func(goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			center, err := s.deps.Cosmetics.Center(s.ctx, s.appID, userID, "")
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, center)
		}); err != nil {
			return err
		}
		if err := object.Set("loadout", func(goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			loadout, err := s.deps.Cosmetics.MyLoadout(s.ctx, s.appID, userID, "")
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, loadout)
		}); err != nil {
			return err
		}
	}
	if !s.has(CapCosmeticWrite) {
		return nil
	}
	return object.Set("equip", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		slot := argString(call, 0)
		cosmeticID := call.Argument(1).ToInteger()
		position := 0
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			position = int(call.Argument(2).ToInteger())
		}
		if slot == "" {
			panic(vm.ToValue("请指定装扮位"))
		}
		s.recordEffect(CapCosmeticWrite, map[string]any{"userId": userID, "slot": slot, "cosmeticId": cosmeticID, "position": position})
		if s.dryRun {
			return vm.ToValue(map[string]any{"simulated": true})
		}
		loadout, err := s.deps.Cosmetics.Equip(s.ctx, EquipInput{
			AppID: s.appID, UserID: userID, Slot: slot, Position: position, CosmeticID: cosmeticID,
		})
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, loadout)
	})
}

// ── number.read ─────────────────────────────────────────────────────

func (s *ScriptSDK) bindNumberNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Numbers == nil {
		return missingDep(CapNumberRead, "号码")
	}
	return object.Set("get", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.targetUser(vm, call, 0)
		if userID <= 0 {
			return goja.Null()
		}
		view, err := s.deps.Numbers.Me(s.ctx, s.appID, userID)
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, view)
	})
}

// ── payment.read ────────────────────────────────────────────────────

// orderPayload 订单只出脚本用得上的字段：回调原文、客户端 IP、元数据不给。
func orderPayload(order paymentdomain.Order) map[string]any {
	return map[string]any{
		"orderNo": order.OrderNo, "subject": order.Subject, "amount": order.Amount.String(),
		"currency": order.Currency, "paymentMethod": order.PaymentMethod, "status": order.Status,
		"refundedAmount": order.RefundedAmount.String(), "paidAt": timeString(order.PaidAt),
		"createdAt": order.CreatedAt.Format(time.RFC3339),
	}
}

func (s *ScriptSDK) bindPaymentNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Payment == nil {
		return missingDep(CapPaymentRead, "支付")
	}
	if err := object.Set("orders", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.requireUser(vm)
		options := argObject(call, 0)
		result, err := s.deps.Payment.ListUserOrders(s.ctx, s.session(userID), paymentdomain.OrderListQuery{
			Status: optString(options, "status"), Page: 1, Limit: clampSDKLimit(optInt(options, "limit", 20), 20, maxHistoryLimit),
		})
		if err != nil {
			throw(vm, err)
		}
		out := make([]map[string]any, 0, len(result.Items))
		for _, order := range result.Items {
			out = append(out, orderPayload(order))
		}
		return vm.ToValue(out)
	}); err != nil {
		return err
	}
	return object.Set("order", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.requireUser(vm)
		orderNo := argString(call, 0)
		if orderNo == "" {
			return goja.Null()
		}
		order, err := s.deps.Payment.GetUserOrder(s.ctx, s.session(userID), orderNo)
		if err != nil {
			if isNotFound(err) {
				return goja.Null()
			}
			throw(vm, err)
		}
		if order == nil {
			return goja.Null()
		}
		return vm.ToValue(orderPayload(*order))
	})
}

// ── cloud.read / cloud.write ────────────────────────────────────────

func (s *ScriptSDK) bindCloudNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Cloud == nil {
		return missingDep(CapCloudRead, "云存储")
	}
	if s.has(CapCloudRead) {
		if err := object.Set("get", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			namespace, key := argString(call, 0), argString(call, 1)
			if namespace == "" || key == "" {
				panic(vm.ToValue("请指定命名空间与键"))
			}
			item, err := s.deps.Cloud.GetItem(s.ctx, s.appID, userID, namespace, key, 0)
			if err != nil {
				if isNotFound(err) {
					return goja.Null()
				}
				throw(vm, err)
			}
			if item == nil || item.Deleted {
				return goja.Null()
			}
			var content any
			if item.Encoding == "json" && len(item.Content) > 0 {
				_ = json.Unmarshal(item.Content, &content)
			} else if len(item.Content) > 0 {
				var text string
				if json.Unmarshal(item.Content, &text) == nil {
					content = text
				}
			}
			return vm.ToValue(map[string]any{
				"revision": item.Revision, "contentType": item.ContentType, "encoding": item.Encoding,
				"content": content, "omitted": item.ContentOmitted, "updatedAt": item.UpdatedAt.Format(time.RFC3339),
			})
		}); err != nil {
			return err
		}
	}
	if !s.has(CapCloudWrite) {
		return nil
	}
	return object.Set("put", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		namespace, key := argString(call, 0), argString(call, 1)
		if namespace == "" || key == "" {
			panic(vm.ToValue("请指定命名空间与键"))
		}
		value := call.Argument(2)
		encoding, contentType := "json", "application/json"
		var data []byte
		if exported, ok := value.Export().(string); ok {
			encoding, contentType, data = "text", "text/plain; charset=utf-8", []byte(exported)
		} else {
			encoded, err := json.Marshal(value.Export())
			if err != nil {
				throw(vm, err)
			}
			data = encoded
		}
		if int64(len(data)) > cloudstorage.JSONWriteLimit {
			panic(vm.ToValue("内容超过云存储单条上限"))
		}
		options := argObject(call, 3)
		var ifRevision *int64
		if _, ok := options["ifRevision"]; ok {
			revision := optInt(options, "ifRevision", 0)
			ifRevision = &revision
		}
		s.recordEffect(CapCloudWrite, map[string]any{"userId": userID, "namespace": namespace, "key": key, "bytes": len(data)})
		if s.dryRun {
			return vm.ToValue(map[string]any{"revision": 0, "simulated": true})
		}
		item, err := s.deps.Cloud.PutItem(s.ctx, CloudWriteInput{
			AppID: s.appID, UserID: userID, Namespace: namespace, Key: key, Data: data,
			Encoding: encoding, ContentType: contentType, IfRevision: ifRevision,
			DeviceID: s.caller.DeviceID, Source: cloudstorage.SourceWrite,
			Metadata: map[string]any{"function": s.functionName},
		})
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(map[string]any{"revision": item.Revision})
	})
}

// ── feedback.read / feedback.write ──────────────────────────────────
//
// 应用用户只有意见反馈一个诉求入口，工单只对管理员开放，函数也不能替用户提工单。

func (s *ScriptSDK) bindFeedbackNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Tickets == nil {
		return missingDep(CapFeedbackRead, "意见反馈")
	}
	if s.has(CapFeedbackRead) {
		if err := object.Set("list", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.requireUser(vm)
			options := argObject(call, 0)
			var statuses []string
			if status := optString(options, "status"); status != "" {
				statuses = []string{status}
			}
			limit := clampSDKLimit(optInt(options, "limit", 20), 20, maxHistoryLimit)
			result, err := s.deps.Tickets.ListFeedback(s.ctx, s.session(userID), statuses, 1, limit)
			if err != nil {
				throw(vm, err)
			}
			return toJS(vm, result.Items)
		}); err != nil {
			return err
		}
	}
	if !s.has(CapFeedbackWrite) {
		return nil
	}
	return object.Set("create", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		options := argObject(call, 0)
		title, content := optString(options, "title"), optString(options, "content")
		categoryID := optInt(options, "categoryId", 0)
		if title == "" || content == "" || categoryID <= 0 {
			panic(vm.ToValue("反馈需要分类、标题与内容"))
		}
		s.recordEffect(CapFeedbackWrite, map[string]any{"userId": userID, "title": title})
		if s.dryRun {
			return vm.ToValue(map[string]any{"id": "0", "ticketNo": "", "status": "simulated", "simulated": true})
		}
		detail, err := s.deps.Tickets.CreateFeedback(s.ctx, s.session(userID), FeedbackCreateInput{
			CategoryID: categoryID, Title: title, Content: content, Contact: optString(options, "contact"),
			Client: &FeedbackClient{Platform: "function", Device: s.functionName},
		})
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(map[string]any{"id": strconv.FormatInt(detail.ID, 10), "ticketNo": detail.TicketNo, "status": detail.Status})
	})
}

// ── content.read ────────────────────────────────────────────────────

func (s *ScriptSDK) bindContentNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Apps == nil {
		return missingDep(CapContentRead, "应用")
	}
	if err := object.Set("banners", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		banners, err := s.deps.Apps.GetBanners(s.ctx, s.appID)
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, banners)
	}); err != nil {
		return err
	}
	return object.Set("notices", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		notices, err := s.deps.Apps.GetNotices(s.ctx, s.appID)
		if err != nil {
			throw(vm, err)
		}
		return toJS(vm, notices)
	})
}

// ── device.read ─────────────────────────────────────────────────────

func (s *ScriptSDK) bindDevicesNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Devices == nil {
		return missingDep(CapDeviceRead, "设备")
	}
	return object.Set("list", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.requireUser(vm)
		status, err := s.deps.Devices.Status(s.ctx, s.appID, userID, "")
		if err != nil {
			throw(vm, err)
		}
		out := toMap(status)
		// IP 不给脚本：设备列表是给「设备数」「当前设备」这类判断用的，不是定位工具
		if devices, ok := out["devices"].([]any); ok {
			for _, device := range devices {
				if row, ok := device.(map[string]any); ok {
					delete(row, "ip")
				}
			}
		}
		return vm.ToValue(out)
	})
}

// ── function.call ───────────────────────────────────────────────────

// FunctionCallRequest 函数互调（aegis.functions.call）。
type FunctionCallRequest struct {
	AppID   int64
	Name    string
	Input   json.RawMessage
	Caller  functiondomain.Caller
	Depth   int
	DryRun  bool
	Parent  string
	EventID string
}

// FunctionCallResult 被调函数的输出（或它抛的业务错误）。
type FunctionCallResult struct {
	Output   json.RawMessage
	Business *ScriptBusinessError
}

func (s *ScriptSDK) bindFunctionsNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.CallFunction == nil {
		return missingDep(CapFunctionCall, "函数调用")
	}
	return object.Set("call", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		name := argString(call, 0)
		if name == "" {
			panic(vm.ToValue("请指定函数名"))
		}
		if name == s.functionName {
			panic(vm.ToValue("函数不能调用自己"))
		}
		if s.depth+1 > maxFunctionCallDepth {
			panic(vm.ToValue(fmt.Sprintf("函数互调最多嵌套 %d 层", maxFunctionCallDepth)))
		}
		input := json.RawMessage(`{}`)
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
			encoded, err := json.Marshal(call.Argument(1).Export())
			if err != nil {
				throw(vm, err)
			}
			input = encoded
		}
		s.callSeq++
		s.recordEffect(CapFunctionCall, map[string]any{"function": name, "depth": s.depth + 1})
		result, err := s.deps.CallFunction(s.ctx, FunctionCallRequest{
			AppID: s.appID, Name: name, Input: input, Caller: s.caller, Depth: s.depth + 1,
			DryRun: s.dryRun, Parent: s.functionName,
			EventID: deriveEventID(s.eventID, name, s.callSeq),
		})
		if err != nil {
			throw(vm, err)
		}
		if result.Business != nil {
			// 被调函数的业务错误原样冒上来：外层脚本可以 catch 它，不 catch 就成为外层的业务错误
			s.businessError = result.Business
			panic(vm.ToValue(result.Business.Message))
		}
		if len(result.Output) == 0 {
			return goja.Undefined()
		}
		var output any
		if err := json.Unmarshal(result.Output, &output); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(output)
	})
}

// 子系统能力的短名字（定义在能力目录里）。
const (
	CapPointsRead      = functiondomain.CapPointsRead
	CapLevelRead       = functiondomain.CapLevelRead
	CapLevelWrite      = functiondomain.CapLevelWrite
	CapRightsRead      = functiondomain.CapRightsRead
	CapRightsGrant     = functiondomain.CapRightsGrant
	CapRightsConsume   = functiondomain.CapRightsConsume
	CapSignInRead      = functiondomain.CapSignInRead
	CapSignInWrite     = functiondomain.CapSignInWrite
	CapInviteRead      = functiondomain.CapInviteRead
	CapLeaderboardRead = functiondomain.CapLeaderboardRead
	CapLotteryRead     = functiondomain.CapLotteryRead
	CapLotteryDraw     = functiondomain.CapLotteryDraw
	CapStoreRead       = functiondomain.CapStoreRead
	CapStoreRedeem     = functiondomain.CapStoreRedeem
	CapCardKeyRedeem   = functiondomain.CapCardKeyRedeem
	CapCosmeticRead    = functiondomain.CapCosmeticRead
	CapCosmeticWrite   = functiondomain.CapCosmeticWrite
	CapNumberRead      = functiondomain.CapNumberRead
	CapPaymentRead     = functiondomain.CapPaymentRead
	CapCloudRead       = functiondomain.CapCloudRead
	CapCloudWrite      = functiondomain.CapCloudWrite
	CapFeedbackRead    = functiondomain.CapFeedbackRead
	CapFeedbackWrite   = functiondomain.CapFeedbackWrite
	CapContentRead     = functiondomain.CapContentRead
	CapDeviceRead      = functiondomain.CapDeviceRead
	CapFunctionCall    = functiondomain.CapFunctionCall
)

// isNotFound 服务层的「不存在」统一回 null 给脚本，而不是抛错 —— 查不到是正常分支。
func isNotFound(err error) bool {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.HTTPStatus == 404
	}
	return false
}

// deriveEventID 被调函数的 eventId 由外层派生（UUID v5）：外层重放时被调函数也命中同一个幂等记录。
func deriveEventID(parent, name string, seq int) string {
	base, err := uuid.Parse(parent)
	if err != nil {
		return uuid.NewString()
	}
	return uuid.NewSHA1(base, []byte(name+"#"+strconv.Itoa(seq))).String()
}
