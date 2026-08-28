package service

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	functiondomain "aegis/internal/domain/appfunction"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	systemdomain "aegis/internal/domain/system"
	userdomain "aegis/internal/domain/user"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/bsm/redislock"
	"github.com/dop251/goja"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// 脚本能力常量。真正的定义在 internal/domain/appfunction 的能力目录里 ——
// 那张表同时驱动服务端校验、SDK 绑定、控制台勾选框与编辑器类型提示。
// 这里只是给本包一组短名字，不要在别处另抄一份字面量。
const (
	CapUserRead         = functiondomain.CapUserRead
	CapUserWrite        = functiondomain.CapUserWrite
	CapPointsWrite      = functiondomain.CapPointsWrite
	CapVipRead          = functiondomain.CapVipRead
	CapVipWrite         = functiondomain.CapVipWrite
	CapWalletRead       = functiondomain.CapWalletRead
	CapWalletWrite      = functiondomain.CapWalletWrite
	CapKVRead           = functiondomain.CapKVRead
	CapKVWrite          = functiondomain.CapKVWrite
	CapLockAcquire      = functiondomain.CapLockAcquire
	CapNotificationSend = functiondomain.CapNotificationSend
	CapRealtimePush     = functiondomain.CapRealtimePush
	CapEmailSend        = functiondomain.CapEmailSend
	CapAuditWrite       = functiondomain.CapAuditWrite
	CapGeoRead          = functiondomain.CapGeoRead
	CapHTTPFetch        = functiondomain.CapHTTPFetch
	CapAIGenerate       = functiondomain.CapAIGenerate
)

// 单次调用的 SDK 用量上限，避免脚本把自己变成刷接口的工具。
const (
	maxSDKCalls      = 256
	maxSDKMutations  = 64
	maxSDKFetches    = 8
	maxKVKeyLength   = 128
	maxKVValueBytes  = 32 << 10
	maxFetchBodySize = 256 << 10
	maxConfigBytes   = 32 << 10
	// maxScriptLogLines 试跑时回传的日志行数上限。
	// 不设上限的话，一个 for 循环里的 console.log 能把响应撑到几十兆。
	maxScriptLogLines = 200
	maxScriptLogBytes = 2000
	// maxLockSeconds 单把分布式锁的最长持有时间。
	//
	// 锁必须自动到期：脚本可能在持锁期间被超时中断，那时没有任何代码
	// 会去释放它。上限压在函数超时上限（30s）之上留一点余量，
	// 再长就不是「保护临界区」而是「把这个键锁死」了。
	maxLockSeconds     = 60
	defaultLockSeconds = 10
)

// kvReservedPrefix 平台自用的 KV 键前缀，脚本读写不到。
//
// 频次限制的计数器就落在这个前缀下（与脚本共用一张表是为了跨实例原子），
// 不隔开的话脚本可以把限制自己的那个计数清零 —— 那个限制就等于不存在。
const kvReservedPrefix = "__aegis:"

// FunctionRuntimeLimits 随能力目录一起下发给控制台。
//
// 作者应当在动手写之前就知道额度，而不是在一次超限失败之后去翻文档。
func FunctionRuntimeLimits() functiondomain.RuntimeLimits {
	return functiondomain.RuntimeLimits{
		MaxSDKCalls:      maxSDKCalls,
		MaxSDKMutations:  maxSDKMutations,
		MaxSDKFetches:    maxSDKFetches,
		MaxSourceBytes:   maxScriptSourceBytes,
		MaxKVKeyLength:   maxKVKeyLength,
		MaxKVValueBytes:  maxKVValueBytes,
		MaxFetchBodySize: maxFetchBodySize,
		MaxConfigBytes:   maxConfigBytes,
		MaxTimeoutMs:     maxFunctionTimeoutMs,
		MaxConcurrency:   maxFunctionConcurrency,
		MaxLogLines:      maxScriptLogLines,
		MaxLockSeconds:   maxLockSeconds,
	}
}

// ScriptBusinessError 是脚本通过 aegis.fail() 主动抛出的业务错误，
// 会被原样翻译成对调用方的错误响应，而不是「函数执行失败」。
type ScriptBusinessError struct {
	Code    int
	Message string
}

func (e *ScriptBusinessError) Error() string { return e.Message }

// ScriptSDKDeps 是 SDK 需要的宿主依赖，由 AppFunctionService 注入。
//
// 每一项都对应一组能力。缺哪一项，对应能力在绑定时就会被跳过并如实报错，
// 而不是先绑上去、等脚本调用时空指针 —— 后者的表现是一句
// 「函数执行失败」，作者完全看不出是平台没装配好。
type ScriptSDKDeps struct {
	Log           *zap.Logger
	PG            *pgrepo.Repository
	Points        *PointsService
	Vip           *VipService
	Notifications *NotificationService
	Audit         *AuditService
	HTTP          *AppFunctionHTTPExecutor
	Wallet        *WalletService
	Email         *EmailService
	Bans          *AccountBanService
	// Realtime / Location / Redis 分别支撑 realtime.push / geo.read / lock.acquire。
	// 与上面几项同一条约定：缺哪一项，对应能力在绑定时点名报错。
	Realtime  *RealtimeService
	Location  *LocationService
	Redis     *redis.Client
	KeyPrefix string
	// AI 支撑 ai.generate：走应用/平台配置的 AI 通道（带链路回退）。
	AI *AIProviderService
}

// ScriptSDK 是注入脚本的 `aegis` 全局对象，每次调用新建一个。
//
// 设计要点：
//   - 能力按 capabilities 逐个绑定，没声明就不存在，不是「调用时才报错」；
//   - 每个写操作都会记进 effects，最终写入调用审计，形成真实的副作用流水；
//   - 用户级操作一律锁定到当前调用者，脚本无法跨用户写；
//   - 试跑（dryRun）时读是真的、写只记不做。
type ScriptSDK struct {
	ctx  context.Context
	deps ScriptSDKDeps

	appID        int64
	appKey       string
	functionName string
	eventID      string
	caller       functiondomain.Caller
	capabilities map[string]struct{}
	config       map[string]any

	// dryRun 控制台试跑：读真实数据，写只记录不执行。
	//
	// 反过来做（写也真写）会让「试一下」成为一次不可撤销的线上操作；
	// 而两边都假（读也假）等于什么都没测到 —— 脚本的分支几乎全部由
	// 服务端状态决定，喂假数据跑出来的结论没有意义。
	dryRun bool

	effects   []functiondomain.Effect
	logs      []functiondomain.LogEntry
	calls     int
	mutations int
	fetches   int

	// startedAt 用来给每行日志算相对耗时。脚本里没有计时器，
	// 「哪一步慢」只能靠日志之间的间隔看出来。
	startedAt time.Time

	// heldLocks 本次调用还没归还的锁。脚本被超时中断时不会有任何代码
	// 走到 release，因此收口处要统一释放 —— 锁自身的 TTL 只是最后一道保险。
	heldLocks map[string]*scriptLock

	// 脚本通过 aegis.fail() 主动终止时记录在这里
	businessError *ScriptBusinessError
}

// scriptSDKOptions 是调用链路之间的差异项，避免构造函数再长出两个位置参数。
type scriptSDKOptions struct {
	Config json.RawMessage
	DryRun bool
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
	options scriptSDKOptions,
) *ScriptSDK {
	set := make(map[string]struct{}, len(capabilities))
	for _, capability := range functiondomain.NormalizeCapabilities(capabilities) {
		set[capability] = struct{}{}
	}
	config := map[string]any{}
	if len(options.Config) > 0 {
		_ = json.Unmarshal(options.Config, &config)
	}
	return &ScriptSDK{
		ctx: ctx, deps: deps,
		appID: appID, appKey: appKey, functionName: functionName,
		eventID: eventID, caller: caller, capabilities: set,
		config: config, dryRun: options.DryRun,
		startedAt: time.Now(),
		heldLocks: map[string]*scriptLock{},
	}
}

// Effects 返回脚本实际产生的副作用，用于写入调用审计。
func (s *ScriptSDK) Effects() []functiondomain.Effect { return s.effects }

// Logs 返回脚本写下的日志（只在试跑时回传给作者）。
func (s *ScriptSDK) Logs() []functiondomain.LogEntry { return s.logs }

// Release 归还本次调用还持有的锁。
//
// 必须由调用方在 finally 位置调用：脚本被超时中断时，它自己那句 release
// 永远不会执行 —— 而一把要等 TTL 才自动到期的锁，会把后续每一次调用
// 都挡在门外，表现为「这个函数忽然全部超时」。
func (s *ScriptSDK) Release() {
	if len(s.heldLocks) == 0 {
		return
	}
	for key := range s.heldLocks {
		s.releaseLock(key)
	}
	s.heldLocks = map[string]*scriptLock{}
}

// BusinessError 返回脚本主动抛出的业务错误（若有）。
func (s *ScriptSDK) BusinessError() *ScriptBusinessError { return s.businessError }

// Usage 返回本次用掉的额度，供试跑结果展示「离上限还有多远」。
func (s *ScriptSDK) Usage() (calls, mutations, fetches int) {
	return s.calls, s.mutations, s.fetches
}

func (s *ScriptSDK) has(capability string) bool {
	_, ok := s.capabilities[capability]
	return ok
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
	s.effects = append(s.effects, functiondomain.Effect{
		Type: effectType, Arguments: encoded, Simulated: s.dryRun,
	})
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
		panic(vm.ToValue("该操作要求调用者是应用用户；在控制台试跑时请指定一个用户身份"))
	}
	return userID
}

// appendLog 收集脚本日志：进服务端日志的同时留一份给试跑面板。
//
// 只进服务端日志是不够的 —— 作者在控制台上写脚本，却要去翻服务器日志
// 才能看见自己打的那一行，这条排障链路长到没人会用。
func (s *ScriptSDK) appendLog(level string, message string) {
	if len(message) > maxScriptLogBytes {
		message = message[:maxScriptLogBytes] + "…（已截断）"
	}
	s.deps.Log.Info("应用函数脚本日志",
		zap.Int64("app_id", s.appID),
		zap.String("function", s.functionName),
		zap.String("event_id", s.eventID),
		zap.String("level", level),
		zap.Bool("dry_run", s.dryRun),
		zap.String("message", message))
	elapsed := float64(time.Since(s.startedAt).Microseconds()) / 1000
	switch {
	case len(s.logs) < maxScriptLogLines:
		s.logs = append(s.logs, functiondomain.LogEntry{
			Level: level, Message: message, ElapsedMs: elapsed,
		})
	case len(s.logs) == maxScriptLogLines:
		s.logs = append(s.logs, functiondomain.LogEntry{
			Level:     "warn",
			Message:   fmt.Sprintf("日志超过 %d 行，后续已丢弃", maxScriptLogLines),
			ElapsedMs: elapsed,
		})
	}
}

func throw(vm *goja.Runtime, err error) {
	panic(vm.ToValue(err.Error()))
}

// rethrowScriptError 把「宿主回调里跑脚本」拿到的错误原样送回 JS 栈。
//
// 直接 panic(err) 是不行的：goja 的恢复逻辑只认 Value / *Exception /
// uncatchableException 三类，别的类型会**继续向上 panic**，把整个请求
// goroutine 带崩。三类各有各的送法：
//   - 超时中断：原样抛，否则一次超时会被记成「锁内执行失败」
//   - 脚本异常：抛它的值，aegis.fail 走的就是这条路，包一层会盖掉业务错误
//   - 其余宿主错误：转成字符串异常
func rethrowScriptError(vm *goja.Runtime, err error) any {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return interrupted
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		return exception.Value()
	}
	return vm.ToValue(err.Error())
}

// bind 把 SDK 挂到运行时的 `aegis` 全局对象上。
//
// 命名空间由多项能力共同组成（user.read 出 get、user.write 出 ban），
// 与 domain 能力目录里那份 TypeScript 声明一一对应 ——
// 「补全里出现什么，运行时就绑定了什么」这句话靠的就是这一对应关系，
// TestCapabilityCatalogMatchesBinders 双向钉死它。
func (s *ScriptSDK) bind(vm *goja.Runtime) error {
	root := vm.NewObject()

	if err := root.Set("log", s.logFunc(vm, "info")); err != nil {
		return err
	}
	if err := root.Set("fail", s.bindFail(vm)); err != nil {
		return err
	}
	if err := root.Set("assert", s.bindAssert(vm)); err != nil {
		return err
	}
	if err := root.Set("crypto", s.bindCrypto(vm)); err != nil {
		return err
	}
	if err := root.Set("time", s.bindTime(vm)); err != nil {
		return err
	}
	if err := root.Set("config", vm.ToValue(s.config)); err != nil {
		return err
	}

	// 标准库：纯计算，不碰平台数据，因此不需要能力声明。
	// 不给的话作者只能自己手写一份，而手写的 HMAC 拼串、日切、金额运算
	// 出错时都不报错，只是悄悄给出错误结果。
	stdlib := []struct {
		name  string
		build func(*goja.Runtime) *goja.Object
	}{
		{"text", s.bindText},
		{"encoding", s.bindEncoding},
		{"decimal", s.bindDecimal},
		{"json", s.bindJSONUtil},
		{"validate", s.bindValidate},
		{"ua", s.bindUserAgent},
	}
	for _, item := range stdlib {
		if err := root.Set(item.name, item.build(vm)); err != nil {
			return err
		}
	}

	// console 是 aegis.log 的别名。没有它的话，几乎每个作者的第一行
	// console.log 都会撞上 ReferenceError —— 而"沙箱里没有 DOM"
	// 与"没人绑 console"是两回事，后者纯粹是缺了一个别名。
	if err := s.bindConsole(vm); err != nil {
		return err
	}

	namespaces := []struct {
		name  string
		bound bool
		build func(*goja.Runtime, *goja.Object) error
	}{
		{"user", s.has(CapUserRead) || s.has(CapUserWrite), s.bindUserNamespace},
		{"points", s.has(CapPointsWrite), s.bindPointsNamespace},
		{"vip", s.has(CapVipRead) || s.has(CapVipWrite), s.bindVipNamespace},
		{"wallet", s.has(CapWalletRead) || s.has(CapWalletWrite), s.bindWalletNamespace},
		{"lock", s.has(CapLockAcquire), s.bindLockNamespace},
		{"notify", s.has(CapNotificationSend), s.bindNotifyNamespace},
		{"realtime", s.has(CapRealtimePush), s.bindRealtimeNamespace},
		{"email", s.has(CapEmailSend), s.bindEmailNamespace},
		{"audit", s.has(CapAuditWrite), s.bindAuditNamespace},
		{"geo", s.has(CapGeoRead), s.bindGeoNamespace},
		{"ai", s.has(CapAIGenerate), s.bindAINamespace},
	}
	for _, namespace := range namespaces {
		if !namespace.bound {
			continue
		}
		object := vm.NewObject()
		if err := namespace.build(vm, object); err != nil {
			return err
		}
		if err := root.Set(namespace.name, object); err != nil {
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
	if s.has(CapHTTPFetch) {
		if err := root.Set("fetch", s.bindFetch(vm)); err != nil {
			return err
		}
	}

	return vm.Set("aegis", root)
}

// ── 基础能力（无需声明） ────────────────────────────────────────────

func (s *ScriptSDK) logFunc(vm *goja.Runtime, level string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		parts := make([]string, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			// 对象直接 String() 得到的是 [object Object]，日志里毫无用处
			switch exported := argument.Export().(type) {
			case map[string]any, []any:
				if encoded, err := json.Marshal(exported); err == nil {
					parts = append(parts, string(encoded))
					continue
				}
				parts = append(parts, argument.String())
			default:
				parts = append(parts, argument.String())
			}
		}
		s.appendLog(level, strings.Join(parts, " "))
		return goja.Undefined()
	}
}

func (s *ScriptSDK) bindConsole(vm *goja.Runtime) error {
	object := vm.NewObject()
	levels := map[string]string{
		"log": "info", "debug": "info", "info": "info", "warn": "warn", "error": "error",
	}
	for method, level := range levels {
		if err := object.Set(method, s.logFunc(vm, level)); err != nil {
			return err
		}
	}
	return vm.Set("console", object)
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

// bindAssert 是 fail 的前置判定版。
//
// 「校验全部走在写入之前」是这套沙箱反复强调的写法（脚本不是一个大事务），
// 而它在代码里长成一串 `if (!x) aegis.fail(...)`。给一个断言不是省字数，
// 是让那一串前置校验在视觉上聚成一块，从而更难被漏掉一条。
func (s *ScriptSDK) bindAssert(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if call.Argument(0).ToBoolean() {
			return goja.Undefined()
		}
		message := "参数校验失败"
		if len(call.Arguments) > 1 {
			message = call.Argument(1).String()
		}
		code := 40001
		if len(call.Arguments) > 2 {
			if parsed := call.Argument(2).ToInteger(); parsed > 0 {
				code = int(parsed)
			}
		}
		s.businessError = &ScriptBusinessError{Code: code, Message: message}
		panic(vm.ToValue(message))
	}
}

func (s *ScriptSDK) bindCrypto(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()

	digest := func(sum func([]byte) []byte) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			return vm.ToValue(hex.EncodeToString(sum([]byte(call.Argument(0).String()))))
		}
	}
	// md5 / sha1 明知已不适合做安全摘要，仍然提供：接第三方接口时用什么签名
	// 算法不由我们决定（易支付系全是 MD5）。不给的话作者只能在脚本里手写一份，
	// 那才是真正危险的做法。
	_ = object.Set("md5", digest(func(input []byte) []byte { sum := md5.Sum(input); return sum[:] }))
	_ = object.Set("sha1", digest(func(input []byte) []byte { sum := sha1.Sum(input); return sum[:] }))
	_ = object.Set("sha256", digest(func(input []byte) []byte { sum := sha256.Sum256(input); return sum[:] }))
	_ = object.Set("sha512", digest(func(input []byte) []byte { sum := sha512.Sum512(input); return sum[:] }))

	mac := func(factory func() hash.Hash) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			writer := hmac.New(factory, []byte(call.Argument(0).String()))
			writer.Write([]byte(call.Argument(1).String()))
			return vm.ToValue(hex.EncodeToString(writer.Sum(nil)))
		}
	}
	_ = object.Set("hmacSha256", mac(sha256.New))
	_ = object.Set("hmacSha512", mac(sha512.New))

	encode := func(encoding *base64.Encoding) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			return vm.ToValue(encoding.EncodeToString([]byte(call.Argument(0).String())))
		}
	}
	decode := func(encoding *base64.Encoding) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			decoded, err := encoding.DecodeString(strings.TrimSpace(call.Argument(0).String()))
			if err != nil {
				throw(vm, fmt.Errorf("base64 解码失败: %w", err))
			}
			return vm.ToValue(string(decoded))
		}
	}
	_ = object.Set("base64Encode", encode(base64.StdEncoding))
	_ = object.Set("base64Decode", decode(base64.StdEncoding))
	_ = object.Set("base64UrlEncode", encode(base64.RawURLEncoding))
	_ = object.Set("base64UrlDecode", decode(base64.RawURLEncoding))

	_ = object.Set("hexEncode", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(hex.EncodeToString([]byte(call.Argument(0).String())))
	})
	_ = object.Set("hexDecode", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		decoded, err := hex.DecodeString(strings.TrimSpace(call.Argument(0).String()))
		if err != nil {
			throw(vm, fmt.Errorf("hex 解码失败: %w", err))
		}
		return vm.ToValue(string(decoded))
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
	_ = object.Set("randomInt", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		low := call.Argument(0).ToInteger()
		high := call.Argument(1).ToInteger()
		if high < low {
			low, high = high, low
		}
		// 用密码学随机而不是 Math.random：抽奖、发号这类用途一旦可预测，
		// 「把逻辑放到服务端」这件事就白做了。
		value, err := rand.Int(rand.Reader, big.NewInt(high-low+1))
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(low + value.Int64())
	})
	_ = object.Set("uuid", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(uuid.NewString())
	})
	_ = object.Set("timingSafeEqual", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		left := []byte(call.Argument(0).String())
		right := []byte(call.Argument(1).String())
		// 长度不等时仍跑一次比较，避免用「返回得特别快」把答案泄露出去
		if len(left) != len(right) {
			subtle.ConstantTimeCompare(left, left)
			return vm.ToValue(false)
		}
		return vm.ToValue(subtle.ConstantTimeCompare(left, right) == 1)
	})

	// 对称加密、JWT、TOTP、口令派生：同样是纯计算，同样是「不给就得手写」。
	// 定义在 app_function_stdlib.go，那边有逐项的取舍说明。
	s.extendCrypto(vm, object)
	return object
}

// bindTime 暴露服务端时间。
//
// 沙箱里的 Date 取的是同一台机器的时钟，所以这组函数不是为了"更准"，
// 而是为了把「每日额度的键怎么算」收成一个写法：各人各写一遍
// toISOString().slice(0,10) 迟早会出现两个函数用不同的日切。
func (s *ScriptSDK) bindTime(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()
	_ = object.Set("now", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(time.Now().UnixMilli())
	})
	_ = object.Set("unix", func(goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(time.Now().Unix())
	})
	_ = object.Set("iso", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		moment := time.Now().UTC()
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) {
			moment = time.UnixMilli(call.Argument(0).ToInteger()).UTC()
		}
		return vm.ToValue(moment.Format(time.RFC3339))
	})
	_ = object.Set("dayKey", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(time.Now().UTC().AddDate(0, 0, int(call.Argument(0).ToInteger())).Format("2006-01-02"))
	})
	_ = object.Set("monthKey", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(time.Now().UTC().AddDate(0, int(call.Argument(0).ToInteger()), 0).Format("2006-01"))
	})

	// 按时区日切、格式化、cron 下一跳：见 app_function_stdlib.go
	s.extendTime(vm, object)
	return object
}

// ── user.read / user.write ──────────────────────────────────────────

// bindUserNamespace 暴露调用者的服务端状态与处置手段。
//
// 读这一半是反破解的根基：会员是否有效、积分多少、是否被封禁，全部由服务端
// 现查，客户端既伪造不了也预测不了，脚本依赖它之后就无法在本地被复现。
func (s *ScriptSDK) bindUserNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.has(CapUserRead) {
		if err := object.Set("get", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			payload := s.loadUserPayload(vm, s.resolveTargetUser(call))
			if payload == nil {
				return goja.Null()
			}
			return vm.ToValue(payload)
		}); err != nil {
			return err
		}
		if err := object.Set("entitlement", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			return s.entitlementValue(vm, s.resolveTargetUser(call))
		}); err != nil {
			return err
		}
	}
	if !s.has(CapUserWrite) {
		return nil
	}
	if s.deps.Bans == nil {
		return fmt.Errorf("能力 %s 依赖的封禁服务未装配", CapUserWrite)
	}

	if err := object.Set("ban", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		reason := strings.TrimSpace(call.Argument(0).String())
		if reason == "" {
			// 理由会出现在用户看到的提示与申诉记录里，空理由等于把
			// 「为什么被封」这个问题原样丢给客服
			panic(vm.ToValue("封禁必须给出理由"))
		}
		seconds := call.Argument(1).ToInteger()
		banType, banScope := "manual", "login"
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			if options, ok := call.Argument(2).Export().(map[string]any); ok {
				if value, ok := options["type"].(string); ok && value != "" {
					banType = value
				}
				if value, ok := options["scope"].(string); ok && value != "" {
					banScope = value
				}
			}
		}
		var endAt *time.Time
		if seconds > 0 {
			deadline := time.Now().Add(time.Duration(seconds) * time.Second)
			endAt = &deadline
		}
		s.recordEffect(CapUserWrite, map[string]any{
			"op": "ban", "userId": userID, "reason": reason,
			"seconds": seconds, "scope": banScope, "type": banType,
		})
		if s.dryRun {
			return vm.ToValue(map[string]any{"banId": 0, "simulated": true})
		}
		ban, err := s.deps.Bans.BanUser(s.ctx, s.appID, userID, userdomain.AccountBanCreateInput{
			BanType: banType, BanScope: banScope, Reason: reason, EndAt: endAt,
			Evidence: map[string]any{
				"source": "app-function", "function": s.functionName, "eventId": s.eventID,
			},
			Operator: userdomain.BanOperator{AdminName: s.operatorLabel()},
		})
		if err != nil {
			throw(vm, err)
		}
		result := map[string]any{"banId": ban.ID}
		if endAt != nil {
			result["endAt"] = endAt.UTC().Format(time.RFC3339)
		}
		return vm.ToValue(result)
	}); err != nil {
		return err
	}

	return object.Set("unban", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		reason := "远程函数 " + s.functionName
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) {
			reason = call.Argument(0).String()
		}
		s.recordEffect(CapUserWrite, map[string]any{"op": "unban", "userId": userID, "reason": reason})
		if s.dryRun {
			return vm.ToValue(true)
		}
		active, err := s.deps.Bans.GetActiveBan(s.ctx, s.appID, userID)
		if err != nil {
			throw(vm, err)
		}
		if active == nil {
			return vm.ToValue(false)
		}
		if _, err := s.deps.Bans.RevokeBan(s.ctx, s.appID, userID, active.ID, userdomain.AccountBanRevokeInput{
			Reason:   reason,
			Operator: userdomain.BanOperator{AdminName: s.operatorLabel()},
		}); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(true)
	})
}

// resolveTargetUser 解析读操作的目标用户：省略参数即当前调用者。
func (s *ScriptSDK) resolveTargetUser(call goja.FunctionCall) int64 {
	if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) {
		return call.Argument(0).ToInteger()
	}
	return s.callerUserID()
}

func (s *ScriptSDK) loadUserPayload(vm *goja.Runtime, userID int64) map[string]any {
	if userID <= 0 {
		return nil
	}
	user, err := s.deps.PG.GetUserByID(s.ctx, userID)
	if err != nil {
		throw(vm, err)
	}
	// 严格限定在本应用内，避免脚本探测其它租户的用户
	if user == nil || user.AppID != s.appID {
		return nil
	}

	now := time.Now()
	banned := !user.Enabled || (user.DisabledEndTime != nil && user.DisabledEndTime.After(now))

	// 会员判定走统一入口，不在这里重算一遍 `expireAt.After(now)`：
	// 脚本要判的往往不只是"是不是会员"，还有"是不是试用会员"
	// （试用用户能不能用这个功能，是每个接入方都要自己决定的事）。
	entitlement, err := s.deps.Vip.ResolveEntitlement(s.ctx, s.appID, userID, "")
	if err != nil {
		throw(vm, err)
	}

	payload := map[string]any{
		"id":      user.ID,
		"account": user.Account,
		"enabled": user.Enabled,
		"banned":  banned,
		"vip":     entitlement.IsVIP,
		"vipTrial": entitlement.IsTrial,
		// 功能标识：脚本可以直接判「这个用户能不能用某个能力」，
		// 而不必去猜套餐名 —— 那是运营随时会改的展示文案
		"vipFeatures": entitlement.Features,
		"vipSource":   entitlement.Source,
		"integral":    user.Integral,
		"experience":  user.Experience,
		"createdAt":   user.CreatedAt.Format(time.RFC3339),
	}
	if user.DisabledEndTime != nil && user.DisabledEndTime.After(now) {
		payload["bannedUntil"] = user.DisabledEndTime.Format(time.RFC3339)
	}
	if entitlement.ExpireAt != nil {
		payload["vipExpireAt"] = entitlement.ExpireAt.Format(time.RFC3339)
		payload["vipRemainingSeconds"] = entitlement.RemainingSeconds
	}
	if profile, err := s.deps.PG.GetUserProfileByUserID(s.ctx, userID); err == nil && profile != nil {
		payload["nickname"] = profile.Nickname
		payload["email"] = profile.Email
		payload["markcode"] = profile.MarkCode
		payload["role"] = profile.Role
	}
	return payload
}

func (s *ScriptSDK) entitlementValue(vm *goja.Runtime, userID int64) goja.Value {
	entitlement := s.resolveEntitlement(vm, userID)
	if entitlement == nil {
		return goja.Null()
	}
	payload := map[string]any{
		"isVip":            entitlement.IsVIP,
		"isTrial":          entitlement.IsTrial,
		"source":           entitlement.Source,
		"features":         entitlement.Features,
		"remainingSeconds": entitlement.RemainingSeconds,
		"remainingDays":    entitlement.RemainingDays,
		"trialAvailable":   entitlement.TrialOffer.Available,
		"trialReason":      entitlement.TrialOffer.Reason,
	}
	if entitlement.ExpireAt != nil {
		payload["expireAt"] = entitlement.ExpireAt.Format(time.RFC3339)
	}
	return vm.ToValue(payload)
}

// resolveEntitlement 取会员结论；跨应用或不存在的用户返回 nil。
func (s *ScriptSDK) resolveEntitlement(vm *goja.Runtime, userID int64) *vipdomain.Entitlement {
	if userID <= 0 {
		return nil
	}
	user, err := s.deps.PG.GetUserByID(s.ctx, userID)
	if err != nil {
		throw(vm, err)
	}
	if user == nil || user.AppID != s.appID {
		return nil
	}
	entitlement, err := s.deps.Vip.ResolveEntitlement(s.ctx, s.appID, userID, "")
	if err != nil {
		throw(vm, err)
	}
	return entitlement
}

// ── points.write ────────────────────────────────────────────────────

func (s *ScriptSDK) bindPointsNamespace(vm *goja.Runtime, object *goja.Object) error {
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
		if s.dryRun {
			// 试跑返回「如果真的执行会变成多少」：读一次真实余额再算。
			// 恒回 0 会让脚本后续的分支走错，等于什么都没测到。
			after := int64(0)
			if user, err := s.deps.PG.GetUserByID(s.ctx, userID); err == nil && user != nil {
				after = user.Integral + sign*amount
			}
			s.recordEffect(CapPointsWrite, map[string]any{
				"userId": userID, "amount": sign * amount, "reason": reason, "after": after,
			})
			return vm.ToValue(after)
		}
		result, err := s.deps.Points.AdjustUserIntegral(s.ctx, userID, s.appID, sign*amount, reason,
			pointdomain.AdminAdjustOptions{AdminAccount: s.operatorLabel()})
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect(CapPointsWrite, map[string]any{
			"userId": userID, "amount": sign * amount, "reason": reason,
			"after": result.AfterAmount,
		})
		return vm.ToValue(result.AfterAmount)
	}

	if err := object.Set("add", func(call goja.FunctionCall) goja.Value {
		return adjust(call, 1)
	}); err != nil {
		return err
	}
	return object.Set("deduct", func(call goja.FunctionCall) goja.Value {
		return adjust(call, -1)
	})
}

// ── vip.read / vip.write ────────────────────────────────────────────

func (s *ScriptSDK) bindVipNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.has(CapVipRead) {
		if err := object.Set("status", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			return s.entitlementValue(vm, s.resolveTargetUser(call))
		}); err != nil {
			return err
		}
		// 功能标识判定单独给一个入口：让脚本自己 indexOf 一遍 features
		// 数组，等于把「标识大小写、别名」这套归一化规则复制到每个脚本里。
		if err := object.Set("hasFeature", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			tag := strings.TrimSpace(call.Argument(0).String())
			if tag == "" {
				return vm.ToValue(false)
			}
			userID := s.callerUserID()
			if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
				userID = call.Argument(1).ToInteger()
			}
			entitlement := s.resolveEntitlement(vm, userID)
			if entitlement == nil {
				return vm.ToValue(false)
			}
			return vm.ToValue(entitlement.HasFeature(tag))
		}); err != nil {
			return err
		}
	}
	if !s.has(CapVipWrite) {
		return nil
	}

	grant := func(call goja.FunctionCall, sign int) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		days := int(call.Argument(0).ToInteger())
		if days <= 0 {
			panic(vm.ToValue("会员天数必须为正数"))
		}
		reason := "远程函数 " + s.functionName
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			reason = call.Argument(1).String()
		}
		s.recordEffect(CapVipWrite, map[string]any{
			"userId": userID, "days": sign * days, "reason": reason,
		})
		if s.dryRun {
			return vm.ToValue(map[string]any{"days": sign * days, "userId": userID, "simulated": true})
		}
		if _, err := s.deps.Vip.AdminGrantVip(s.ctx, AdminVipGrantInput{
			UserID:   userID,
			AppID:    s.appID,
			Days:     sign * days,
			Reason:   reason,
			Operator: s.operatorLabel(),
		}); err != nil {
			throw(vm, err)
		}
		result := map[string]any{"days": sign * days, "userId": userID}
		// 回读到期时间：脚本经常要把它直接回给客户端展示，
		// 让作者再调一次 status() 是多打一次库。
		if entitlement, err := s.deps.Vip.ResolveEntitlement(s.ctx, s.appID, userID, ""); err == nil &&
			entitlement != nil && entitlement.ExpireAt != nil {
			result["expireAt"] = entitlement.ExpireAt.Format(time.RFC3339)
		}
		return vm.ToValue(result)
	}

	if err := object.Set("grant", func(call goja.FunctionCall) goja.Value {
		return grant(call, 1)
	}); err != nil {
		return err
	}
	return object.Set("revoke", func(call goja.FunctionCall) goja.Value {
		return grant(call, -1)
	})
}

// ── wallet.read / wallet.write ──────────────────────────────────────

func (s *ScriptSDK) bindWalletNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Wallet == nil {
		return fmt.Errorf("能力 %s 依赖的钱包服务未装配", CapWalletRead)
	}
	if s.has(CapWalletRead) {
		if err := object.Set("get", func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			userID := s.resolveTargetUser(call)
			if userID <= 0 {
				return goja.Null()
			}
			wallet, err := s.deps.Wallet.AdminGetWallet(s.ctx, userID, s.appID)
			if err != nil {
				throw(vm, err)
			}
			if wallet == nil {
				return goja.Null()
			}
			return vm.ToValue(walletPayload(wallet.UserID, wallet.Balance, wallet.Frozen,
				wallet.TotalRecharged, wallet.TotalConsumed))
		}); err != nil {
			return err
		}
	}
	if !s.has(CapWalletWrite) {
		return nil
	}
	return object.Set("adjust", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		// 金额按字符串解析：JS 的 number 是双精度浮点，0.1+0.2 那套问题
		// 落在钱上就是对不上账。作者传 number 我们也接，但内部一律走定点小数。
		amount, err := decimal.NewFromString(strings.TrimSpace(call.Argument(0).String()))
		if err != nil {
			throw(vm, fmt.Errorf("金额格式无效: %w", err))
		}
		if amount.IsZero() {
			panic(vm.ToValue("调账金额不能为 0"))
		}
		reason := "远程函数 " + s.functionName
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			reason = call.Argument(1).String()
		}
		s.recordEffect(CapWalletWrite, map[string]any{
			"userId": userID, "amount": amount.String(), "reason": reason,
		})
		if s.dryRun {
			balance := decimal.Zero
			if wallet, err := s.deps.Wallet.AdminGetWallet(s.ctx, userID, s.appID); err == nil && wallet != nil {
				balance = wallet.Balance
			}
			return vm.ToValue(walletPayload(userID, balance.Add(amount),
				decimal.Zero, decimal.Zero, decimal.Zero))
		}
		result, err := s.deps.Wallet.AdminAdjust(s.ctx, userID, s.appID, amount, reason, s.operatorLabel(), "")
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(walletPayload(result.Wallet.UserID, result.Wallet.Balance, result.Wallet.Frozen,
			result.Wallet.TotalRecharged, result.Wallet.TotalConsumed))
	})
}

// walletPayload 金额一律以字符串出境 —— 转 float64 会丢分，
// 而钱包的每一分都要能与对账单逐行核对。
func walletPayload(userID int64, balance, frozen, recharged, consumed decimal.Decimal) map[string]any {
	return map[string]any{
		"userId":         userID,
		"balance":        balance.String(),
		"frozen":         frozen.String(),
		"totalRecharged": recharged.String(),
		"totalConsumed":  consumed.String(),
	}
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
	guardReserved := func(key string) {
		// 平台自用前缀（频次计数器就在这里）对脚本不可见：
		// 能读能写就意味着脚本可以把限制自己的那个计数清零。
		if strings.HasPrefix(key, kvReservedPrefix) {
			panic(vm.ToValue("KV 键名不能以 " + kvReservedPrefix + " 开头（平台保留）"))
		}
	}
	requireKey := func(call goja.FunctionCall) string {
		key := strings.TrimSpace(call.Argument(0).String())
		if key == "" || len(key) > maxKVKeyLength {
			panic(vm.ToValue(fmt.Sprintf("KV 键名必须非空且不超过 %d 字符", maxKVKeyLength)))
		}
		guardReserved(key)
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

	if err := object.Set("has", func(call goja.FunctionCall) goja.Value {
		requireRead()
		s.budget(vm, false)
		entry, err := s.deps.PG.GetAppFunctionKV(s.ctx, s.appID, scope, scopeID(), requireKey(call))
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(entry != nil)
	}); err != nil {
		return nil, err
	}

	if err := object.Set("list", func(call goja.FunctionCall) goja.Value {
		requireRead()
		s.budget(vm, false)
		prefix := ""
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) {
			prefix = strings.TrimSpace(call.Argument(0).String())
			guardReserved(prefix)
		}
		keys, err := s.deps.PG.ListAppFunctionKVKeys(s.ctx, s.appID, scope, scopeID(),
			prefix, int(call.Argument(1).ToInteger()))
		if err != nil {
			throw(vm, err)
		}
		// 空前缀查询会把保留键一起捞上来，这里再滤一次
		filtered := make([]string, 0, len(keys))
		for _, key := range keys {
			if !strings.HasPrefix(key, kvReservedPrefix) {
				filtered = append(filtered, key)
			}
		}
		return vm.ToValue(filtered)
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
		s.recordEffect(CapKVWrite, map[string]any{"scope": scope, "key": key, "op": "set"})
		if s.dryRun {
			return goja.Undefined()
		}
		if err := s.deps.PG.SetAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key, encoded, ttl); err != nil {
			throw(vm, err)
		}
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
		if s.dryRun {
			// 试跑要算出「如果真的自增会是多少」：额度判断全靠这个返回值，
			// 恒回 delta 会让「今日额度已用尽」那条分支永远测不到。
			current := int64(0)
			if entry, err := s.deps.PG.GetAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key); err == nil && entry != nil {
				_ = json.Unmarshal(entry.Value, &current)
			}
			after := current + delta
			s.recordEffect(CapKVWrite, map[string]any{
				"scope": scope, "key": key, "op": "incr", "delta": delta, "after": after,
			})
			return vm.ToValue(after)
		}
		// 原子自增：频次限制与剩余次数依赖它，并发调用不会读到同一个旧值
		value, err := s.deps.PG.IncrAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key, delta, ttl)
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect(CapKVWrite, map[string]any{
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
		s.recordEffect(CapKVWrite, map[string]any{"scope": scope, "key": key, "op": "del"})
		if s.dryRun {
			return vm.ToValue(true)
		}
		removed, err := s.deps.PG.DeleteAppFunctionKV(s.ctx, s.appID, scope, scopeID(), key)
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(removed)
	}); err != nil {
		return nil, err
	}

	return object, nil
}

// ── lock.acquire ────────────────────────────────────────────────────

// bindLockNamespace 跨实例互斥锁。
//
// 「先查后写」这段临界区在单实例下靠运气也能对，多实例部署下必然出事：
// 两个请求同时读到「还没领过」，然后各发一份。KV 的 incr 能挡住计数型
// 的并发，但挡不住「判断 A、修改 B」这种跨键的临界区 —— 那正是发奖、
// 兑换、抽奖这几类脚本的标准形状。
//
// 锁落在 Redis 而不是 app_function_kv：这里要的是「抢不到就立刻失败」，
// 而数据库的 UPSERT 语义给不了这个（它总会成功）。SET NX PX 恰好就是它。
func (s *ScriptSDK) bindLockNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Redis == nil {
		return fmt.Errorf("能力 %s 依赖的 Redis 未装配", CapLockAcquire)
	}

	if err := object.Set("acquire", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		key := s.requireLockKey(vm, call.Argument(0))
		token, ok := s.acquireLock(vm, key, lockSeconds(call.Argument(1)))
		if !ok {
			return goja.Null()
		}
		return vm.ToValue(token)
	}); err != nil {
		return err
	}

	if err := object.Set("release", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		key := s.requireLockKey(vm, call.Argument(0))
		if held, ok := s.heldLocks[key]; !ok || held.token != call.Argument(1).String() {
			// 令牌对不上说明这把锁已经过期并被别人抢走了。
			// 照删不误会把别人的临界区打开，那比锁没生效更糟。
			return vm.ToValue(false)
		}
		return vm.ToValue(s.releaseLock(key))
	}); err != nil {
		return err
	}

	// run 是给作者用的那一个：acquire / release 成对写在脚本里，
	// 中间任何一次 aegis.fail 都会跳过 release，而那正是最常写的分支。
	return object.Set("run", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		key := s.requireLockKey(vm, call.Argument(0))
		body, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("aegis.lock.run 的第二个参数必须是函数"))
		}
		if _, acquired := s.acquireLock(vm, key, lockSeconds(call.Argument(2))); !acquired {
			panic(vm.ToValue("未能获得锁 " + key + "，请稍后重试"))
		}
		defer s.releaseLock(key)
		returned, err := body(goja.Undefined())
		if err != nil {
			panic(rethrowScriptError(vm, err))
		}
		return returned
	})
}

func lockSeconds(value goja.Value) int {
	seconds := int(value.ToInteger())
	if seconds <= 0 {
		return defaultLockSeconds
	}
	if seconds > maxLockSeconds {
		return maxLockSeconds
	}
	return seconds
}

// requireLockKey 锁键按 (应用, 函数) 加前缀。
//
// 不加的话两个应用用同一个键名就会互相锁住对方，而那种串扰
// 在任何一侧都看不出来 —— 只会表现为「偶尔抢不到锁」。
func (s *ScriptSDK) requireLockKey(vm *goja.Runtime, value goja.Value) string {
	key := strings.TrimSpace(value.String())
	if key == "" || len(key) > maxKVKeyLength {
		panic(vm.ToValue(fmt.Sprintf("锁键必须非空且不超过 %d 字符", maxKVKeyLength)))
	}
	return key
}

// scriptLock 一把已持有的锁。dryRun 时 handle 为 nil（没有真的占锁），
// 但令牌照发 —— 脚本里那句 release(key, token) 在试跑与真跑时行为一致。
type scriptLock struct {
	token  string
	handle *redislock.Lock
}

func (s *ScriptSDK) lockRedisKey(key string) string {
	return fmt.Sprintf("%sfunction:lock:%d:%s:%s", s.deps.KeyPrefix, s.appID, s.functionName, key)
}

func (s *ScriptSDK) acquireLock(vm *goja.Runtime, key string, seconds int) (string, bool) {
	if existing, held := s.heldLocks[key]; held {
		// 同一次调用里重复抢同一把锁：直接给回原令牌而不是判失败。
		// 判失败会让「函数 A 调用的公共段里也加了同一把锁」这种写法必然死锁。
		return existing.token, true
	}
	if s.dryRun {
		// 试跑不真的占锁：占了就会把线上正在跑的那次挡在外面。
		token := scriptUUID()
		s.recordEffect(CapLockAcquire, map[string]any{"op": "acquire", "key": key, "ttl": seconds})
		s.heldLocks[key] = &scriptLock{token: token}
		return token, true
	}
	// 用 redislock 而不是裸 SetNX + DEL：释放必须校验持有者，否则超时之后
	// 那一删会打开**别人**的临界区。幂等中间件出于同一个理由用的也是它，
	// 同一件事在一个仓库里不该有两套实现。
	lock, err := redislock.New(s.deps.Redis).Obtain(s.ctx, s.lockRedisKey(key),
		time.Duration(seconds)*time.Second, nil)
	if errors.Is(err, redislock.ErrNotObtained) {
		return "", false
	}
	if err != nil {
		throw(vm, fmt.Errorf("获取锁失败: %w", err))
	}
	s.recordEffect(CapLockAcquire, map[string]any{"op": "acquire", "key": key, "ttl": seconds})
	s.heldLocks[key] = &scriptLock{token: lock.Token(), handle: lock}
	return lock.Token(), true
}

func (s *ScriptSDK) releaseLock(key string) bool {
	held, ok := s.heldLocks[key]
	if !ok {
		return false
	}
	delete(s.heldLocks, key)
	if held.handle == nil {
		return true
	}
	// 释放用不带取消的 ctx：脚本超时后 s.ctx 已经取消，而这时恰恰最需要
	// 把锁还回去 —— 用已取消的 context 发命令会直接失败。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 2*time.Second)
	defer cancel()
	if err := held.handle.Release(ctx); err != nil {
		s.deps.Log.Warn("释放函数锁失败（等待 TTL 自动到期）",
			zap.Int64("app_id", s.appID), zap.String("function", s.functionName),
			zap.String("key", key), zap.Error(err))
		return false
	}
	return true
}

// ── notification.send ───────────────────────────────────────────────

func (s *ScriptSDK) bindNotifyNamespace(vm *goja.Runtime, object *goja.Object) error {
	return object.Set("send", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		title := strings.TrimSpace(call.Argument(0).String())
		content := strings.TrimSpace(call.Argument(1).String())
		if title == "" || content == "" {
			panic(vm.ToValue("通知标题与内容不能为空"))
		}
		level, notificationType := "info", "system"
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
		s.recordEffect(CapNotificationSend, map[string]any{
			"userId": userID, "title": title, "level": level,
		})
		if s.dryRun {
			return vm.ToValue(1)
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
		if result == nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(result.Delivered)
	})
}

// ── realtime.push ───────────────────────────────────────────────────

// bindRealtimeNamespace 给当前调用者的在线连接推一条事件。
//
// 与站内信的分工写在返回值里：这条**不落库、不补发**，离线即丢，
// 因此返回 false 是正常结果而不是错误。两者混用是最常见的误解 ——
// 「重要的通知用实时推送发出去了，用户没收到」。
func (s *ScriptSDK) bindRealtimeNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Realtime == nil {
		return fmt.Errorf("能力 %s 依赖的实时服务未装配", CapRealtimePush)
	}
	return object.Set("send", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		event := strings.TrimSpace(call.Argument(0).String())
		if event == "" {
			panic(vm.ToValue("事件名不能为空"))
		}
		payload := map[string]any{}
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			if exported, ok := call.Argument(1).Export().(map[string]any); ok {
				payload = exported
			}
		}
		// 事件名带命名空间：客户端要能一眼看出这条是哪个函数推的，
		// 否则两个函数用同一个事件名时客户端无从分辨。
		payload["function"] = s.functionName
		s.recordEffect(CapRealtimePush, map[string]any{"userId": userID, "event": event})
		if s.dryRun {
			return vm.ToValue(true)
		}
		if err := s.deps.Realtime.PublishUserEvent(s.ctx, s.appID, userID,
			"function."+event, payload); err != nil {
			s.appendLog("warn", "实时推送失败："+err.Error())
			return vm.ToValue(false)
		}
		return vm.ToValue(true)
	})
}

// ── email.send ──────────────────────────────────────────────────────

// bindEmailNamespace 发信给当前调用者。
//
// 收件地址由服务端从账号资料里取，**脚本填不了** —— 允许指定收件人
// 等于把平台变成一个任何人都能驱动的转发器（凭证邮件那条链路上是同一条约束）。
func (s *ScriptSDK) bindEmailNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Email == nil {
		return fmt.Errorf("能力 %s 依赖的邮件服务未装配", CapEmailSend)
	}
	return object.Set("send", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		userID := s.requireUser(vm)
		subject := strings.TrimSpace(call.Argument(0).String())
		body := strings.TrimSpace(call.Argument(1).String())
		if subject == "" || body == "" {
			panic(vm.ToValue("邮件主题与正文不能为空"))
		}
		profile, err := s.deps.PG.GetUserProfileByUserID(s.ctx, userID)
		if err != nil {
			throw(vm, err)
		}
		if profile == nil || strings.TrimSpace(profile.Email) == "" {
			// 静默成功会让作者以为信发出去了。返回 false 让脚本自己决定怎么办。
			s.appendLog("warn", "调用者未绑定邮箱，邮件未发送")
			return vm.ToValue(false)
		}
		s.recordEffect(CapEmailSend, map[string]any{
			"userId": userID, "subject": subject, "to": maskEmail(profile.Email),
		})
		if s.dryRun {
			return vm.ToValue(true)
		}
		if err := s.deps.Email.SendNotificationEmail(s.ctx, s.appID, profile.Email, subject, body, ""); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(true)
	})
}

// ── audit.write ─────────────────────────────────────────────────────

func (s *ScriptSDK) bindAuditNamespace(vm *goja.Runtime, object *goja.Object) error {
	return object.Set("log", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, true)
		action := strings.TrimSpace(call.Argument(0).String())
		if action == "" {
			panic(vm.ToValue("审计 action 不能为空"))
		}
		summary := call.Argument(1).String()
		s.recordEffect(CapAuditWrite, map[string]any{"action": action, "summary": summary})
		if s.dryRun {
			return goja.Undefined()
		}
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
		return goja.Undefined()
	})
}

// ── geo.read ────────────────────────────────────────────────────────

// bindGeoNamespace IP 归属地查询。
//
// 查不到时返回的是一条 resolved=false 的记录而不是 null：脚本里几乎
// 一定会接着读 .country，返回 null 会让那一行抛 TypeError，
// 而「查不到归属地」本来是一个应当被正常处理的分支。
func (s *ScriptSDK) bindGeoNamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.Location == nil {
		return fmt.Errorf("能力 %s 依赖的位置服务未装配", CapGeoRead)
	}
	return object.Set("lookup", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		address := strings.TrimSpace(call.Argument(0).String())
		if address == "" {
			panic(vm.ToValue("IP 不能为空"))
		}
		location := s.deps.Location.Resolve(s.ctx, address)
		payload := map[string]any{
			"ip": location.IP,
			// 内网地址与查不到的地址都算「没查到」：把 127.0.0.1 当成
			// 一个有国家的地址，会让基于归属地的判定得出荒谬结论。
			"resolved":    location.Country != "" && !location.IsPrivate,
			"country":     location.Country,
			"countryCode": location.CountryCode,
			"region":      location.Region,
			"city":        location.City,
			"timezone":    location.Timezone,
			"isp":         location.ISP,
			"asn":         location.Network.ASN,
			"private":     location.IsPrivate,
		}
		if location.Coordinates != nil {
			if location.Coordinates.Latitude != nil {
				payload["latitude"] = *location.Coordinates.Latitude
			}
			if location.Coordinates.Longitude != nil {
				payload["longitude"] = *location.Coordinates.Longitude
			}
		}
		return vm.ToValue(payload)
	})
}

// ── ai.generate ─────────────────────────────────────────────────────

// bindAINamespace 让脚本调用应用/平台配置的 AI 通道生成文本。
//
// 与 fetch 同一档管制：计入出网次数预算（LLM 调用比普通 HTTP 更贵），
// 每次调用记一条 effect（谁在花钱要看得见）。试跑时**真实执行** ——
// 生成是无副作用的读，作者要在控制台里调 prompt，跳过它试跑就没有意义。
func (s *ScriptSDK) bindAINamespace(vm *goja.Runtime, object *goja.Object) error {
	if s.deps.AI == nil {
		return fmt.Errorf("能力 %s 依赖的 AI 服务未装配", CapAIGenerate)
	}
	return object.Set("generate", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		s.fetches++
		if s.fetches > maxSDKFetches {
			panic(vm.ToValue(fmt.Sprintf("单次调用的出站请求超过 %d 次上限", maxSDKFetches)))
		}
		prompt := strings.TrimSpace(call.Argument(0).String())
		if prompt == "" || prompt == "undefined" {
			panic(vm.ToValue("prompt 不能为空"))
		}

		var (
			model       string
			system      string
			maxTokens   = 1024
			jsonMode    bool
			temperature *float64
		)
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			if options, ok := call.Argument(1).Export().(map[string]any); ok {
				if value, ok := options["model"].(string); ok {
					model = strings.TrimSpace(value)
				}
				if value, ok := options["system"].(string); ok {
					system = value
				}
				if value, ok := options["maxTokens"].(int64); ok && value > 0 {
					maxTokens = int(value)
				} else if value, ok := options["maxTokens"].(float64); ok && value > 0 {
					maxTokens = int(value)
				}
				if value, ok := options["json"].(bool); ok {
					jsonMode = value
				}
				switch value := options["temperature"].(type) {
				case int64:
					parsed := float64(value)
					temperature = &parsed
				case float64:
					temperature = &value
				}
			}
		}
		// 上限压住：脚本受函数超时约束，超长生成只会以超时收场，
		// 而钱在超时前就已经花出去了。
		if maxTokens > 4096 {
			maxTokens = 4096
		}
		if temperature != nil {
			clamped := *temperature
			if clamped < 0 {
				clamped = 0
			}
			if clamped > 2 {
				clamped = 2
			}
			temperature = &clamped
		}

		response, channel, err := s.deps.AI.Chat(s.ctx, aiChatArgs{
			AppID: s.appID,
			Request: aidomain.ChatRequest{
				Model:       model,
				System:      system,
				Messages:    []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, prompt)},
				MaxTokens:   maxTokens,
				Temperature: temperature,
				JSONMode:    jsonMode,
			},
		})
		if err != nil {
			var upstream *aiUpstreamError
			if errors.As(err, &upstream) {
				throw(vm, fmt.Errorf("AI 生成失败：%s", upstream.Message))
			}
			throw(vm, err)
		}
		s.recordEffect(CapAIGenerate, map[string]any{
			"provider": channel.Config.Provider, "model": response.Model,
			"inputTokens": response.Usage.InputTokens, "outputTokens": response.Usage.OutputTokens,
		})

		result := map[string]any{
			"text":  response.Text,
			"json":  nil,
			"model": response.Model,
			"usage": map[string]any{
				"inputTokens":  response.Usage.InputTokens,
				"outputTokens": response.Usage.OutputTokens,
			},
		}
		if jsonMode {
			var decoded any
			if json.Unmarshal([]byte(strings.TrimSpace(response.Text)), &decoded) == nil {
				result["json"] = decoded
			}
		}
		return vm.ToValue(result)
	})
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
					// 字符串原样发送：对面要的可能是表单或 XML，把它再 JSON 编码
					// 一次会多出一对引号，而这个错误只会显示在对方的日志里。
					if text, ok := raw.(string); ok {
						body = []byte(text)
					} else {
						encoded, err := json.Marshal(raw)
						if err != nil {
							throw(vm, err)
						}
						body = encoded
					}
				}
				// form 是给「对方只收 application/x-www-form-urlencoded」
				// 那一大类接口的（易支付系、微信老接口全是）。让作者自己拼
				// 意味着他要自己处理转义与顺序，而顺序错了签名就过不去。
				if raw, ok := options["form"].(map[string]any); ok && len(raw) > 0 {
					form := url.Values{}
					for name, value := range raw {
						form.Set(name, scriptScalarString(value))
					}
					body = []byte(form.Encode())
					if _, exists := headers["Content-Type"]; !exists {
						headers["Content-Type"] = "application/x-www-form-urlencoded"
					}
				}
				if raw, ok := options["query"].(map[string]any); ok && len(raw) > 0 {
					endpoint = appendQueryParams(vm, endpoint, raw)
				}
			}
		}

		// 试跑对非安全方法不实际发出请求：GET/HEAD 是读，多跑一次没有代价；
		// POST 可能是一次扣款、一条短信、一封信，而「试一下」不该有这些后果。
		if s.dryRun && method != http.MethodGet && method != http.MethodHead {
			s.recordEffect(CapHTTPFetch, map[string]any{
				"method": method, "url": endpoint, "skipped": true,
			})
			s.appendLog("warn", "试跑跳过了 "+method+" "+endpoint+"（非安全方法不实际发出）")
			return vm.ToValue(map[string]any{
				"status": 200, "ok": true, "headers": map[string]string{},
				"text": "", "simulated": true,
			})
		}

		status, responseHeaders, payload, err := s.deps.HTTP.FetchWithHeaders(
			s.ctx, method, endpoint, headers, body, maxFetchBodySize)
		if err != nil {
			throw(vm, err)
		}
		s.recordEffect(CapHTTPFetch, map[string]any{
			"method": method, "url": endpoint, "status": status,
		})

		result := map[string]any{
			"status":  status,
			"ok":      status >= 200 && status < 300,
			"headers": responseHeaders,
			"text":    string(payload),
		}
		var decoded any
		if json.Unmarshal(payload, &decoded) == nil {
			result["json"] = decoded
		}
		return vm.ToValue(result)
	}
}

// appendQueryParams 把 query 选项拼到 URL 上，保留原有参数。
func appendQueryParams(vm *goja.Runtime, endpoint string, params map[string]any) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		throw(vm, fmt.Errorf("URL 无效: %w", err))
	}
	values := parsed.Query()
	for name, value := range params {
		values.Set(name, scriptScalarString(value))
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

// operatorLabel 是脚本产生的写操作在积分流水、审计里的操作者标识。
func (s *ScriptSDK) operatorLabel() string {
	return "function:" + s.functionName
}
