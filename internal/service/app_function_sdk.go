package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	functiondomain "aegis/internal/domain/appfunction"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/dop251/goja"
	"go.uber.org/zap"
)

// 脚本能力。声明即授权：未声明的能力在脚本里根本不会被绑定，调用直接抛异常。
const (
	CapUserRead         = "user.read"
	CapPointsWrite      = "points.write"
	CapVipWrite         = "vip.write"
	CapKVRead           = "kv.read"
	CapKVWrite          = "kv.write"
	CapNotificationSend = "notification.send"
	CapAuditWrite       = "audit.write"
	CapHTTPFetch        = "http.fetch"
)

// 单次调用的 SDK 用量上限，避免脚本把自己变成刷接口的工具。
const (
	maxSDKCalls      = 128
	maxSDKMutations  = 32
	maxSDKFetches    = 4
	maxKVKeyLength   = 128
	maxKVValueBytes  = 32 << 10
	maxFetchBodySize = 256 << 10
)

// ScriptBusinessError 是脚本通过 aegis.fail() 主动抛出的业务错误，
// 会被原样翻译成对调用方的错误响应，而不是「函数执行失败」。
type ScriptBusinessError struct {
	Code    int
	Message string
}

func (e *ScriptBusinessError) Error() string { return e.Message }

// ScriptSDKDeps 是 SDK 需要的宿主依赖，由 AppFunctionService 注入。
type ScriptSDKDeps struct {
	Log           *zap.Logger
	PG            *pgrepo.Repository
	Points        *PointsService
	Vip           *VipService
	Notifications *NotificationService
	Audit         *AuditService
	HTTP          *AppFunctionHTTPExecutor
}

// ScriptSDK 是注入脚本的 `aegis` 全局对象，每次调用新建一个。
//
// 设计要点：
//   - 能力按 capabilities 逐个绑定，没声明就不存在，不是「调用时才报错」；
//   - 每个写操作都会记进 effects，最终写入调用审计，形成真实的副作用流水；
//   - 用户级操作一律锁定到当前调用者，脚本无法跨用户写。
type ScriptSDK struct {
	ctx  context.Context
	deps ScriptSDKDeps

	appID        int64
	appKey       string
	functionName string
	eventID      string
	caller       functiondomain.Caller
	capabilities map[string]struct{}

	effects   []functiondomain.Effect
	calls     int
	mutations int
	fetches   int

	// 脚本通过 aegis.fail() 主动终止时记录在这里
	businessError *ScriptBusinessError
}

func newScriptSDK(
	ctx context.Context,
	deps ScriptSDKDeps,
	appID int64,
	appKey string,
	functionName string,
	eventID string,
	caller functiondomain.Caller,
	capabilities []string,
) *ScriptSDK {
	set := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		set[capability] = struct{}{}
	}
	return &ScriptSDK{
		ctx: ctx, deps: deps,
		appID: appID, appKey: appKey, functionName: functionName,
		eventID: eventID, caller: caller, capabilities: set,
	}
}

// Effects 返回脚本实际产生的副作用，用于写入调用审计。
func (s *ScriptSDK) Effects() []functiondomain.Effect { return s.effects }

// BusinessError 返回脚本主动抛出的业务错误（若有）。
func (s *ScriptSDK) BusinessError() *ScriptBusinessError { return s.businessError }

func (s *ScriptSDK) has(capability string) bool {
	if _, ok := s.capabilities[capability]; ok {
		return true
	}
	// 000056 时期的旧能力名；当时 effects 从未执行，这里只做读能力的等价映射
	if capability == CapUserRead {
		_, ok := s.capabilities["user.profile.read"]
		return ok
	}
	return false
}

// callerUserID 返回当前调用者的用户 ID；非用户调用（管理员调试、服务端密钥）返回 0。
func (s *ScriptSDK) callerUserID() int64 {
	if s.caller.UserID != nil {
		return *s.caller.UserID
	}
	return 0
}

func (s *ScriptSDK) recordEffect(effectType string, arguments any) {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		encoded = []byte(`{}`)
	}
	s.effects = append(s.effects, functiondomain.Effect{Type: effectType, Arguments: encoded})
}

// budget 在每次 SDK 调用前记账；超限直接终止脚本。
func (s *ScriptSDK) budget(vm *goja.Runtime, mutation bool) {
	s.calls++
	if s.calls > maxSDKCalls {
		panic(vm.ToValue(fmt.Sprintf("单次调用的 SDK 使用次数超过 %d 次上限", maxSDKCalls)))
	}
	if mutation {
		s.mutations++
		if s.mutations > maxSDKMutations {
			panic(vm.ToValue(fmt.Sprintf("单次调用的写操作超过 %d 次上限", maxSDKMutations)))
		}
	}
}

func (s *ScriptSDK) requireUser(vm *goja.Runtime) int64 {
	userID := s.callerUserID()
	if userID <= 0 {
		panic(vm.ToValue("该操作要求调用者是应用用户"))
	}
	return userID
}

func throw(vm *goja.Runtime, err error) {
	panic(vm.ToValue(err.Error()))
}

// bind 把 SDK 挂到运行时的 `aegis` 全局对象上。
func (s *ScriptSDK) bind(vm *goja.Runtime) error {
	root := vm.NewObject()

	if err := root.Set("log", s.bindLog(vm)); err != nil {
		return err
	}
	if err := root.Set("fail", s.bindFail(vm)); err != nil {
		return err
	}
	if err := root.Set("crypto", s.bindCrypto(vm)); err != nil {
		return err
	}
	if s.has(CapUserRead) {
		object, err := s.bindUser(vm)
		if err != nil {
			return err
		}
		if err := root.Set("user", object); err != nil {
			return err
		}
	}
	if s.has(CapPointsWrite) {
		object, err := s.bindPoints(vm)
		if err != nil {
			return err
		}
		if err := root.Set("points", object); err != nil {
			return err
		}
	}
	if s.has(CapVipWrite) {
		object, err := s.bindVip(vm)
		if err != nil {
			return err
		}
		if err := root.Set("vip", object); err != nil {
			return err
		}
	}
	if s.has(CapKVRead) || s.has(CapKVWrite) {
		object, err := s.bindKV(vm)
		if err != nil {
			return err
		}
		if err := root.Set("kv", object); err != nil {
			return err
		}
	}
	if s.has(CapNotificationSend) {
		object, err := s.bindNotify(vm)
		if err != nil {
			return err
		}
		if err := root.Set("notify", object); err != nil {
			return err
		}
	}
	if s.has(CapAuditWrite) {
		object, err := s.bindAudit(vm)
		if err != nil {
			return err
		}
		if err := root.Set("audit", object); err != nil {
			return err
		}
	}
	if s.has(CapHTTPFetch) {
		if err := root.Set("fetch", s.bindFetch(vm)); err != nil {
			return err
		}
	}

	return vm.Set("aegis", root)
}

// ── 基础能力（无需声明） ────────────────────────────────────────────

func (s *ScriptSDK) bindLog(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		parts := make([]string, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			parts = append(parts, argument.String())
		}
		// 只进服务端日志，永远不会回给调用方
		s.deps.Log.Info("应用函数脚本日志",
			zap.Int64("app_id", s.appID),
			zap.String("function", s.functionName),
			zap.String("event_id", s.eventID),
			zap.String("message", strings.Join(parts, " ")))
		return goja.Undefined()
	}
}

// bindFail 让脚本主动返回业务错误，例如「授权已过期」「次数用尽」。
func (s *ScriptSDK) bindFail(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		message := "函数拒绝了本次调用"
		if len(call.Arguments) > 0 {
			message = call.Argument(0).String()
		}
		code := 40300
		if len(call.Arguments) > 1 {
			if parsed := call.Argument(1).ToInteger(); parsed > 0 {
				code = int(parsed)
			}
		}
		s.businessError = &ScriptBusinessError{Code: code, Message: message}
		panic(vm.ToValue(message))
	}
}

func (s *ScriptSDK) bindCrypto(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()
	_ = object.Set("sha256", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		sum := sha256.Sum256([]byte(call.Argument(0).String()))
		return vm.ToValue(hex.EncodeToString(sum[:]))
	})
	_ = object.Set("hmacSha256", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		mac := hmac.New(sha256.New, []byte(call.Argument(0).String()))
		mac.Write([]byte(call.Argument(1).String()))
		return vm.ToValue(hex.EncodeToString(mac.Sum(nil)))
	})
	_ = object.Set("randomHex", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		size := int(call.Argument(0).ToInteger())
		if size <= 0 || size > 64 {
			size = 16
		}
		buffer := make([]byte, size)
		if _, err := rand.Read(buffer); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(hex.EncodeToString(buffer))
	})
	return object
}

// ── user.read ───────────────────────────────────────────────────────

// bindUser 暴露调用者的服务端状态。
//
// 这是反破解的根基：VIP 是否有效、积分多少、是否被封禁，全部由服务端现查，
// 客户端既伪造不了也预测不了，脚本依赖它之后就无法在本地被复现。
func (s *ScriptSDK) bindUser(vm *goja.Runtime) (*goja.Object, error) {
	object := vm.NewObject()
	get := func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		userID := s.callerUserID()
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) {
			userID = call.Argument(0).ToInteger()
		}
		if userID <= 0 {
			return goja.Null()
		}
		user, err := s.deps.PG.GetUserByID(s.ctx, userID)
		if err != nil {
			throw(vm, err)
		}
		// 严格限定在本应用内，避免脚本探测其它租户的用户
		if user == nil || user.AppID != s.appID {
			return goja.Null()
		}

		now := time.Now()
		vipActive := user.VIPExpireAt != nil && user.VIPExpireAt.After(now)
		banned := !user.Enabled ||
			(user.DisabledEndTime != nil && user.DisabledEndTime.After(now))

		payload := map[string]any{
			"id":         user.ID,
			"account":    user.Account,
			"enabled":    user.Enabled,
			"banned":     banned,
			"vip":        vipActive,
			"integral":   user.Integral,
			"experience": user.Experience,
			"createdAt":  user.CreatedAt.Format(time.RFC3339),
		}
		if user.VIPExpireAt != nil {
			payload["vipExpireAt"] = user.VIPExpireAt.Format(time.RFC3339)
			payload["vipRemainingSeconds"] = int64(user.VIPExpireAt.Sub(now).Seconds())
		}
		if profile, err := s.deps.PG.GetUserProfileByUserID(s.ctx, userID); err == nil && profile != nil {
			payload["nickname"] = profile.Nickname
			payload["markcode"] = profile.MarkCode
			payload["role"] = profile.Role
		}
		return vm.ToValue(payload)
	}
	if err := object.Set("get", get); err != nil {
		return nil, err
	}
	return object, nil
}

// ── points.write ────────────────────────────────────────────────────

func (s *ScriptSDK) bindPoints(vm *goja.Runtime) (*goja.Object, error) {
	object := vm.NewObject()

	adjust := func(call goja.FunctionCall, sign int64) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		amount := call.Argument(0).ToInteger()
		if amount <= 0 {
			panic(vm.ToValue("积分数量必须为正数"))
		}
		reason := "远程函数 " + s.functionName
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			reason = call.Argument(1).String()
		}
		result, err := s.deps.Points.AdjustUserIntegral(s.ctx, userID, s.appID, sign*amount, reason,
			pointdomain.AdminAdjustOptions{AdminAccount: s.operatorLabel()})
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect("points.write", map[string]any{
			"userId": userID, "amount": sign * amount, "reason": reason,
			"after": result.AfterAmount,
		})
		return vm.ToValue(result.AfterAmount)
	}

	if err := object.Set("add", func(call goja.FunctionCall) goja.Value {
		return adjust(call, 1)
	}); err != nil {
		return nil, err
	}
	if err := object.Set("deduct", func(call goja.FunctionCall) goja.Value {
		return adjust(call, -1)
	}); err != nil {
		return nil, err
	}
	return object, nil
}

// ── vip.write ───────────────────────────────────────────────────────

func (s *ScriptSDK) bindVip(vm *goja.Runtime) (*goja.Object, error) {
	object := vm.NewObject()
	grant := func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		days := int(call.Argument(0).ToInteger())
		if days <= 0 {
			panic(vm.ToValue("VIP 天数必须为正数"))
		}
		reason := "远程函数 " + s.functionName
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			reason = call.Argument(1).String()
		}
		transaction, err := s.deps.Vip.AdminGrantVip(s.ctx, userID, s.appID, days, reason, 0, s.operatorLabel())
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect("vip.write", map[string]any{
			"userId": userID, "days": days, "reason": reason,
		})
		if transaction == nil {
			return goja.Null()
		}
		return vm.ToValue(map[string]any{"days": days, "userId": userID})
	}
	if err := object.Set("grant", grant); err != nil {
		return nil, err
	}
	return object, nil
}

// ── kv.read / kv.write ──────────────────────────────────────────────

// bindKV 暴露服务端独占的键值状态。
//
// aegis.kv.*      → 应用级共享
// aegis.kv.user.* → 按调用者隔离，脚本无法跨用户读写
func (s *ScriptSDK) bindKV(vm *goja.Runtime) (*goja.Object, error) {
	object, err := s.kvNamespace(vm, functiondomain.KVScopeApp)
	if err != nil {
		return nil, err
	}
	userScope, err := s.kvNamespace(vm, functiondomain.KVScopeUser)
	if err != nil {
		return nil, err
	}
	if err := object.Set("user", userScope); err != nil {
		return nil, err
	}
	return object, nil
}

func (s *ScriptSDK) kvNamespace(vm *goja.Runtime, scope string) (*goja.Object, error) {
	object := vm.NewObject()

	scopeID := func() int64 {
		if scope == functiondomain.KVScopeUser {
			return s.requireUser(vm)
		}
		return 0
	}
	requireKey := func(call goja.FunctionCall) string {
		key := strings.TrimSpace(call.Argument(0).String())
		if key == "" || len(key) > maxKVKeyLength {
			panic(vm.ToValue(fmt.Sprintf("KV 键名必须非空且不超过 %d 字符", maxKVKeyLength)))
		}
		return key
	}
	requireRead := func() {
		if !s.has(CapKVRead) {
			panic(vm.ToValue("缺少 kv.read 能力"))
		}
	}
	requireWrite := func() {
		if !s.has(CapKVWrite) {
			panic(vm.ToValue("缺少 kv.write 能力"))
		}
	}

	if err := object.Set("get", func(call goja.FunctionCall) goja.Value {
		requireRead()
		s.budget(vm, false)
		entry, err := s.deps.PG.GetAppFunctionKV(s.ctx, s.appID, scope, scopeID(), requireKey(call))
		if err != nil {
			throw(vm, err)
		}
		if entry == nil {
			return goja.Null()
		}
		var value any
		if err := json.Unmarshal(entry.Value, &value); err != nil {
			return goja.Null()
		}
		return vm.ToValue(value)
	}); err != nil {
		return nil, err
	}

	if err := object.Set("set", func(call goja.FunctionCall) goja.Value {
		requireWrite()
		s.budget(vm, true)
		key := requireKey(call)
		encoded, err := json.Marshal(call.Argument(1).Export())
		if err != nil {
			throw(vm, err)
		}
		if len(encoded) > maxKVValueBytes {
			panic(vm.ToValue(fmt.Sprintf("KV 值超过 %d KB 上限", maxKVValueBytes>>10)))
		}
		ttl := time.Duration(call.Argument(2).ToInteger()) * time.Second
		if err := s.deps.PG.SetAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key, encoded, ttl); err != nil {
			throw(vm, err)
		}
		s.recordEffect("kv.write", map[string]any{"scope": scope, "key": key, "op": "set"})
		return goja.Undefined()
	}); err != nil {
		return nil, err
	}

	if err := object.Set("incr", func(call goja.FunctionCall) goja.Value {
		requireWrite()
		s.budget(vm, true)
		key := requireKey(call)
		delta := int64(1)
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			delta = call.Argument(1).ToInteger()
		}
		ttl := time.Duration(call.Argument(2).ToInteger()) * time.Second
		// 原子自增：频次限制与剩余次数依赖它，并发调用不会读到同一个旧值
		value, err := s.deps.PG.IncrAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key, delta, ttl)
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect("kv.write", map[string]any{
			"scope": scope, "key": key, "op": "incr", "delta": delta, "after": value,
		})
		return vm.ToValue(value)
	}); err != nil {
		return nil, err
	}

	if err := object.Set("del", func(call goja.FunctionCall) goja.Value {
		requireWrite()
		s.budget(vm, true)
		key := requireKey(call)
		removed, err := s.deps.PG.DeleteAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key)
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect("kv.write", map[string]any{"scope": scope, "key": key, "op": "del"})
		return vm.ToValue(removed)
	}); err != nil {
		return nil, err
	}

	return object, nil
}

// ── notification.send ───────────────────────────────────────────────

func (s *ScriptSDK) bindNotify(vm *goja.Runtime) (*goja.Object, error) {
	object := vm.NewObject()
	send := func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		title := strings.TrimSpace(call.Argument(0).String())
		content := strings.TrimSpace(call.Argument(1).String())
		if title == "" || content == "" {
			panic(vm.ToValue("通知标题与内容不能为空"))
		}
		level := "info"
		notificationType := "system"
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			if options, ok := call.Argument(2).Export().(map[string]any); ok {
				if value, ok := options["level"].(string); ok && value != "" {
					level = value
				}
				if value, ok := options["type"].(string); ok && value != "" {
					notificationType = value
				}
			}
		}
		result, err := s.deps.Notifications.AdminBulkSend(s.ctx, s.appID, notificationdomain.AdminBulkSendCommand{
			UserIDs: []int64{userID},
			Limit:   1,
			Type:    notificationType,
			Title:   title,
			Content: content,
			Level:   level,
			Metadata: map[string]any{
				"source":   "app-function",
				"function": s.functionName,
				"eventId":  s.eventID,
			},
		})
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect("notification.send", map[string]any{
			"userId": userID, "title": title, "level": level,
		})
		if result == nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(result.Delivered)
	}
	if err := object.Set("send", send); err != nil {
		return nil, err
	}
	return object, nil
}

// ── audit.write ─────────────────────────────────────────────────────

func (s *ScriptSDK) bindAudit(vm *goja.Runtime) (*goja.Object, error) {
	object := vm.NewObject()
	record := func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		action := strings.TrimSpace(call.Argument(0).String())
		if action == "" {
			panic(vm.ToValue("审计 action 不能为空"))
		}
		summary := call.Argument(1).String()
		s.deps.Audit.Record(systemdomain.AuditEntry{
			AdminName:  s.operatorLabel(),
			Action:     "function." + action,
			Category:   "app-function",
			Severity:   "info",
			Resource:   "app_function",
			ResourceID: s.functionName,
			Summary:    summary,
			Detail:     fmt.Sprintf("eventId=%s appKey=%s", s.eventID, s.appKey),
		})
		s.recordEffect("audit.write", map[string]any{"action": action, "summary": summary})
		return goja.Undefined()
	}
	if err := object.Set("log", record); err != nil {
		return nil, err
	}
	return object, nil
}

// ── http.fetch ──────────────────────────────────────────────────────

// bindFetch 允许脚本访问外部 HTTPS 接口，复用远程函数既有的 SSRF 防护：
// 仅 HTTPS、禁止重定向、连接时重新解析并拒绝内网/元数据地址。
func (s *ScriptSDK) bindFetch(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		s.fetches++
		if s.fetches > maxSDKFetches {
			panic(vm.ToValue(fmt.Sprintf("单次调用的出站请求超过 %d 次上限", maxSDKFetches)))
		}
		endpoint := strings.TrimSpace(call.Argument(0).String())

		method := http.MethodGet
		var body []byte
		headers := map[string]string{}
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			if options, ok := call.Argument(1).Export().(map[string]any); ok {
				if value, ok := options["method"].(string); ok && value != "" {
					method = strings.ToUpper(value)
				}
				if value, ok := options["headers"].(map[string]any); ok {
					for name, raw := range value {
						if text, ok := raw.(string); ok {
							headers[name] = text
						}
					}
				}
				if raw, ok := options["body"]; ok && raw != nil {
					encoded, err := json.Marshal(raw)
					if err != nil {
						throw(vm, err)
					}
					body = encoded
				}
			}
		}

		status, payload, err := s.deps.HTTP.Fetch(s.ctx, method, endpoint, headers, body, maxFetchBodySize)
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect("http.fetch", map[string]any{
			"method": method, "url": endpoint, "status": status,
		})

		result := map[string]any{"status": status, "text": string(payload)}
		var decoded any
		if json.Unmarshal(payload, &decoded) == nil {
			result["json"] = decoded
		}
		return vm.ToValue(result)
	}
}

// operatorLabel 是脚本产生的写操作在积分流水、审计里的操作者标识。
func (s *ScriptSDK) operatorLabel() string {
	return "function:" + s.functionName
}
