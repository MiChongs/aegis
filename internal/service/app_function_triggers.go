package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	functiondomain "aegis/internal/domain/appfunction"
	"aegis/internal/domain/itemgrant"
	plugindomain "aegis/internal/domain/plugin"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 远程函数的触发器与平台化能力：平台事件 / 定时拉起、持久队列、函数互调、全景、调用详情与重放。
//
// 触发链路：钩子回调（或定时扫描）→ 入队 app_function_trigger_jobs → 后台循环抢占执行 → invoke。
// 入队与执行分开是刻意的：钩子在注册、支付这些关键链路上触发，脚本慢一点就把它们拖慢；
// 进程重启时内存里没跑完的那批也会悄悄没了。队列落库后，哪一台实例来执行都一样，失败按退避重试。

const (
	triggerTickInterval  = 15 * time.Second
	triggerJobBatch      = 20
	triggerJobLease      = 2 * time.Minute
	triggerWorkers       = 8
	triggerEventCacheTTL = 30 * time.Second
	triggerJobRetention  = 7 * 24 * time.Hour
	maxTriggerDescRunes  = 200
)

var functionCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

type triggerCacheEntry struct {
	triggers []functiondomain.Trigger
	loaded   time.Time
}

// grantableItemCatalog rights.grant 能发的物品（人工物品不开放：远程函数没有管理员去处理它）。
func grantableItemCatalog() []itemgrant.ItemSpec {
	out := make([]itemgrant.ItemSpec, 0, 20)
	for _, spec := range itemgrant.ItemCatalog() {
		if spec.Type != itemgrant.ItemManual {
			out = append(out, spec)
		}
	}
	return out
}

// ── 触发器管理 ──

// ListTriggers 一个函数的触发器；name 为空时列出整个应用的。
func (s *AppFunctionService) ListTriggers(ctx context.Context, appID int64, name string) ([]functiondomain.Trigger, error) {
	var functionID int64
	if strings.TrimSpace(name) != "" {
		function, err := s.GetFunction(ctx, appID, name)
		if err != nil {
			return nil, err
		}
		functionID = function.ID
	}
	items, err := s.pg.ListAppFunctionTriggers(ctx, appID, functionID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []functiondomain.Trigger{}
	}
	return items, nil
}

// CreateTrigger 新建触发器。
func (s *AppFunctionService) CreateTrigger(ctx context.Context, appID int64, name string,
	input functiondomain.TriggerInput, adminID *int64) (*functiondomain.Trigger, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	count, err := s.pg.CountAppFunctionTriggers(ctx, appID, function.ID)
	if err != nil {
		return nil, err
	}
	if count >= functiondomain.MaxTriggersPerFunction {
		return nil, apperrors.New(40120, http.StatusBadRequest,
			fmt.Sprintf("一个函数最多 %d 个触发器", functiondomain.MaxTriggersPerFunction))
	}
	trigger := functiondomain.Trigger{AppID: appID, FunctionID: function.ID, Kind: strings.TrimSpace(input.Kind),
		Enabled: true, CreatedBy: adminID}
	if input.Enabled != nil {
		trigger.Enabled = *input.Enabled
	}
	if err := s.applyTriggerInput(function, &trigger, input); err != nil {
		return nil, err
	}
	created, err := s.pg.CreateAppFunctionTrigger(ctx, trigger)
	if err != nil {
		return nil, err
	}
	s.invalidateTriggers(appID)
	return created, nil
}

// UpdateTrigger 修改触发器（种类不可变）。
func (s *AppFunctionService) UpdateTrigger(ctx context.Context, appID int64, name string, triggerID int64,
	input functiondomain.TriggerInput) (*functiondomain.Trigger, error) {
	function, existing, err := s.loadTrigger(ctx, appID, name, triggerID)
	if err != nil {
		return nil, err
	}
	if input.Kind != "" && input.Kind != existing.Kind {
		return nil, apperrors.New(40121, http.StatusBadRequest, "触发器种类不能修改，请删除后重建")
	}
	input.Kind = existing.Kind
	// 没传的字段沿用原值
	if input.Event == "" {
		input.Event = existing.Event
	}
	if input.Cron == "" {
		input.Cron = existing.Cron
	}
	if input.Timezone == "" {
		input.Timezone = existing.Timezone
	}
	if len(input.Input) == 0 {
		input.Input = existing.Input
	}
	updated := *existing
	if input.Enabled != nil {
		updated.Enabled = *input.Enabled
	}
	if err := s.applyTriggerInput(function, &updated, input); err != nil {
		return nil, err
	}
	result, err := s.pg.UpdateAppFunctionTrigger(ctx, updated)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, apperrors.New(40494, http.StatusNotFound, "触发器不存在")
	}
	s.invalidateTriggers(appID)
	return result, nil
}

// DeleteTrigger 删除触发器（排队中的任务一并删除）。
func (s *AppFunctionService) DeleteTrigger(ctx context.Context, appID int64, name string, triggerID int64) error {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return err
	}
	removed, err := s.pg.DeleteAppFunctionTrigger(ctx, appID, function.ID, triggerID)
	if err != nil {
		return err
	}
	if removed == 0 {
		return apperrors.New(40494, http.StatusNotFound, "触发器不存在")
	}
	s.invalidateTriggers(appID)
	return nil
}

func (s *AppFunctionService) loadTrigger(ctx context.Context, appID int64, name string, triggerID int64) (
	*functiondomain.Function, *functiondomain.Trigger, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, nil, err
	}
	trigger, err := s.pg.GetAppFunctionTrigger(ctx, appID, function.ID, triggerID)
	if err != nil {
		return nil, nil, err
	}
	if trigger == nil {
		return nil, nil, apperrors.New(40494, http.StatusNotFound, "触发器不存在")
	}
	return function, trigger, nil
}

// applyTriggerInput 校验并写入可改字段；定时触发器顺带算出下一次执行时间。
func (s *AppFunctionService) applyTriggerInput(function *functiondomain.Function, trigger *functiondomain.Trigger,
	input functiondomain.TriggerInput) error {
	description := strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(description) > maxTriggerDescRunes {
		return apperrors.New(40122, http.StatusBadRequest, fmt.Sprintf("说明不能超过 %d 字", maxTriggerDescRunes))
	}
	trigger.Description = description

	// 固定入参同样要过入参契约：放行的话，这个触发器每一次都会在调用入口被 40109 挡下
	staticInput, err := normalizeFunctionConfig(input.Input)
	if err != nil {
		return apperrors.New(40123, http.StatusBadRequest, "固定入参必须是 JSON 对象")
	}
	if err := validateFunctionInput(function.InputSchema, staticInput); err != nil {
		return err
	}
	trigger.Input = staticInput

	switch trigger.Kind {
	case functiondomain.TriggerKindEvent:
		event := strings.TrimSpace(input.Event)
		if _, ok := functiondomain.FindTriggerEvent(event); !ok {
			return apperrors.New(40124, http.StatusBadRequest, "未登记的平台事件："+event)
		}
		trigger.Event, trigger.Cron, trigger.Timezone, trigger.NextRunAt = event, "", "", nil
	case functiondomain.TriggerKindSchedule:
		spec := strings.TrimSpace(input.Cron)
		location, zone, err := triggerLocation(input.Timezone)
		if err != nil {
			return err
		}
		schedule, err := functionCronParser.Parse(spec)
		if err != nil {
			return apperrors.New(40125, http.StatusBadRequest, "cron 表达式无效："+err.Error())
		}
		first := schedule.Next(time.Now().In(location))
		if first.IsZero() {
			return apperrors.New(40125, http.StatusBadRequest, "这个 cron 表达式不会在任何时刻触发")
		}
		trigger.Event, trigger.Cron, trigger.Timezone = "", spec, zone
		if trigger.Enabled {
			trigger.NextRunAt = &first
		} else {
			trigger.NextRunAt = nil
		}
	default:
		return apperrors.New(40126, http.StatusBadRequest, "触发器种类只能是 event 或 schedule")
	}
	return nil
}

// triggerLocation 时区缺省用平台默认时区（国内接入方的日切几乎都是东八区零点）；给错直接报错，不悄悄回落到 UTC。
func triggerLocation(zone string) (*time.Location, string, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		location := timeutil.DefaultLocation()
		return location, location.String(), nil
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, "", apperrors.New(40127, http.StatusBadRequest, "未知时区："+zone)
	}
	return location, zone, nil
}

func nextTriggerRun(trigger functiondomain.Trigger, after time.Time) *time.Time {
	schedule, err := functionCronParser.Parse(trigger.Cron)
	if err != nil {
		return nil
	}
	location, _, err := triggerLocation(trigger.Timezone)
	if err != nil {
		return nil
	}
	next := schedule.Next(after.In(location))
	if next.IsZero() {
		return nil
	}
	return &next
}

// ── 立即触发 ──

// TriggerFireResult 控制台「立即触发」的结果。失败也回 200：作者要看的是错误内容。
type TriggerFireResult struct {
	OK        bool                    `json:"ok"`
	EventID   string                  `json:"eventId"`
	Output    json.RawMessage         `json:"output,omitempty"`
	Effects   []functiondomain.Effect `json:"effects"`
	Error     string                  `json:"error,omitempty"`
	ErrorCode int                     `json:"errorCode,omitempty"`
}

// FireTrigger 立即真实执行一次（不经队列，同步返回）。事件触发器要指定一个用户，模拟一次事件。
func (s *AppFunctionService) FireTrigger(ctx context.Context, appID int64, name string, triggerID int64,
	input functiondomain.TriggerFireInput) (*TriggerFireResult, error) {
	function, trigger, err := s.loadTrigger(ctx, appID, name, triggerID)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	if len(input.Data) > 0 && strings.TrimSpace(string(input.Data)) != "null" {
		if err := json.Unmarshal(input.Data, &data); err != nil {
			return nil, apperrors.New(40123, http.StatusBadRequest, "事件数据必须是 JSON 对象")
		}
	}
	var userID *int64
	if trigger.Kind == functiondomain.TriggerKindEvent {
		if input.UserID == nil || *input.UserID <= 0 {
			return nil, apperrors.New(40128, http.StatusBadRequest, "事件触发器请指定一个应用用户来模拟这次事件")
		}
		user, err := s.pg.GetUserByID(ctx, *input.UserID)
		if err != nil {
			return nil, err
		}
		if user == nil || user.AppID != appID {
			return nil, apperrors.New(40105, http.StatusBadRequest, "指定的用户不属于当前应用")
		}
		userID = input.UserID
	}
	job := functiondomain.TriggerJob{
		AppID: appID, FunctionID: function.ID, TriggerID: trigger.ID, EventID: uuid.NewString(),
		Kind: trigger.Kind, Event: trigger.Event, UserID: userID, FiredAt: time.Now(), Attempts: 1,
	}
	job.Data, _ = json.Marshal(data)
	result, runErr := s.runTriggerInvocation(ctx, function.Name, *trigger, job)
	_ = s.pg.RecordAppFunctionTriggerFire(ctx, trigger.ID, runErr == nil, errorText(runErr))
	out := &TriggerFireResult{OK: runErr == nil, EventID: job.EventID, Effects: []functiondomain.Effect{}}
	if runErr != nil {
		out.Error = runErr.Error()
		var appErr *apperrors.AppError
		if errors.As(runErr, &appErr) {
			out.ErrorCode = appErr.Code
		}
		return out, nil
	}
	out.Output = result.Output
	if result.Effects != nil {
		out.Effects = result.Effects
	}
	return out, nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// runTriggerInvocation 把一个触发任务变成一次函数调用。
func (s *AppFunctionService) runTriggerInvocation(ctx context.Context, function string, trigger functiondomain.Trigger,
	job functiondomain.TriggerJob) (*functiondomain.InvocationResult, error) {
	data := map[string]any{}
	_ = json.Unmarshal(job.Data, &data)
	caller := functiondomain.Caller{Type: functiondomain.CallerSchedule}
	source := functiondomain.SourceSchedule
	if job.Kind == functiondomain.TriggerKindEvent {
		caller = functiondomain.Caller{Type: functiondomain.CallerEvent, UserID: job.UserID}
		source = functiondomain.SourceEvent
	}
	triggerID := trigger.ID
	return s.invoke(ctx, job.AppID, function, job.EventID, trigger.Input, caller, invokeOptions{
		Source: source, TriggerID: &triggerID,
		Trigger: &functiondomain.TriggerContext{
			Kind: job.Kind, TriggerID: trigger.ID, Event: job.Event, Cron: trigger.Cron,
			FiredAt: job.FiredAt, Attempt: job.Attempts, Data: data,
		},
	})
}

// ListTriggerJobs 一个函数的触发任务。
func (s *AppFunctionService) ListTriggerJobs(ctx context.Context, appID int64, name string,
	query functiondomain.TriggerJobQuery) (*functiondomain.TriggerJobPage, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	return s.pg.ListAppFunctionTriggerJobs(ctx, appID, function.ID, query)
}

// RetryTriggerJob 把一条失败的触发任务放回队列。
func (s *AppFunctionService) RetryTriggerJob(ctx context.Context, appID int64, name string, jobID int64) error {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return err
	}
	ok, err := s.pg.RetryAppFunctionTriggerJob(ctx, appID, function.ID, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40129, http.StatusConflict, "只有失败的任务可以重试")
	}
	s.wakeTriggers()
	return nil
}

// ── 平台事件入队 ──

// OnHook 插件钩子观察者：把平台事件转成触发任务。
func (s *AppFunctionService) OnHook(hookName string, data map[string]any, meta plugindomain.HookMetadata) {
	event, ok := functiondomain.TriggerEventForHook(hookName)
	if !ok || meta.AppID == nil || meta.UserID == nil || *meta.AppID <= 0 || *meta.UserID <= 0 {
		return
	}
	appID, userID := *meta.AppID, *meta.UserID
	key := triggerDedupeKey(event, userID, data)
	s.emitEvent(appID, userID, event, key, triggerEventData(event, data))
}

// OnSignIn 签到观察者。同一个人同一天只入队一次。
func (s *AppFunctionService) OnSignIn(appID, userID int64, consecutiveDays int) {
	date := time.Now().In(timeutil.DefaultLocation()).Format(time.DateOnly)
	s.emitEvent(appID, userID, functiondomain.EventUserSignIn, strconv.FormatInt(userID, 10)+":"+date,
		map[string]any{"consecutiveDays": consecutiveDays})
}

// triggerDedupeKey 同一个事件只入队一次：注册按人、支付按订单；其余每次都是一个新事件。
func triggerDedupeKey(event string, userID int64, data map[string]any) string {
	uid := strconv.FormatInt(userID, 10)
	switch event {
	case functiondomain.EventUserRegistered:
		return "register:" + uid
	case functiondomain.EventPaymentCompleted:
		if order := fmt.Sprint(data["orderId"]); order != "" && order != "<nil>" {
			return "paid:" + order
		}
	case functiondomain.EventPaymentRefunded:
		if refund := fmt.Sprint(data["refundNo"]); refund != "" && refund != "<nil>" {
			return "refund:" + refund
		}
	}
	return event + ":" + uid + ":" + uuid.NewString()
}

// triggerEventData 只把事件目录里登记过的字段交给脚本（加上 userId），钩子里的其余内部字段不外泄。
func triggerEventData(event string, data map[string]any) map[string]any {
	out := map[string]any{}
	spec, ok := functiondomain.FindTriggerEvent(event)
	if !ok {
		return out
	}
	for _, field := range spec.Fields {
		if value, ok := data[field.Key]; ok && value != nil {
			out[field.Key] = fmt.Sprint(value)
			if field.Type == "number" {
				if number, err := strconv.ParseFloat(fmt.Sprint(value), 64); err == nil {
					out[field.Key] = number
				}
			}
		}
	}
	return out
}

// emitEvent 入队（后台执行，不阻塞钩子所在的关键链路）。
func (s *AppFunctionService) emitEvent(appID, userID int64, event, key string, data map[string]any) {
	if s == nil || s.pg == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("函数触发器入队崩溃", zap.String("event", event), zap.Any("panic", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		triggers, err := s.eventTriggers(ctx, appID, event)
		if err != nil {
			s.log.Warn("函数触发器：读取事件触发器失败", zap.Int64("app_id", appID), zap.String("event", event), zap.Error(err))
			return
		}
		if len(triggers) == 0 {
			return
		}
		raw, err := json.Marshal(data)
		if err != nil {
			raw = []byte(`{}`)
		}
		queued := false
		for _, trigger := range triggers {
			uid := userID
			inserted, err := s.pg.EnqueueAppFunctionTriggerJob(ctx, functiondomain.TriggerJob{
				AppID: appID, FunctionID: trigger.FunctionID, TriggerID: trigger.ID, EventID: uuid.NewString(),
				Kind: functiondomain.TriggerKindEvent, Event: event, UserID: &uid, Data: raw,
			}, key)
			if err != nil {
				s.log.Warn("函数触发器：入队失败", zap.Int64("trigger", trigger.ID), zap.Error(err))
				continue
			}
			queued = queued || inserted
		}
		if queued {
			s.wakeTriggers()
		}
	}()
}

func (s *AppFunctionService) eventTriggers(ctx context.Context, appID int64, event string) ([]functiondomain.Trigger, error) {
	key := strconv.FormatInt(appID, 10) + "|" + event
	s.triggerMu.Lock()
	entry, ok := s.triggerCache[key]
	s.triggerMu.Unlock()
	if ok && time.Since(entry.loaded) < triggerEventCacheTTL {
		return entry.triggers, nil
	}
	triggers, err := s.pg.ActiveAppFunctionEventTriggers(ctx, appID, event)
	if err != nil {
		return nil, err
	}
	s.triggerMu.Lock()
	s.triggerCache[key] = triggerCacheEntry{triggers: triggers, loaded: time.Now()}
	s.triggerMu.Unlock()
	return triggers, nil
}

func (s *AppFunctionService) invalidateTriggers(appID int64) {
	prefix := strconv.FormatInt(appID, 10) + "|"
	s.triggerMu.Lock()
	for key := range s.triggerCache {
		if strings.HasPrefix(key, prefix) {
			delete(s.triggerCache, key)
		}
	}
	s.triggerMu.Unlock()
}

// ── 后台循环 ──

// StartTriggers 后台循环：到点的定时触发器入队、抢占并执行队列里的任务、清理旧任务。
func (s *AppFunctionService) StartTriggers(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("函数触发器循环崩溃", zap.Any("panic", r))
			}
		}()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.triggerStop:
				return
			case <-s.triggerKick:
				s.triggerTick(ctx)
			case <-timer.C:
				s.triggerTick(ctx)
				timer.Reset(triggerTickInterval)
			}
		}
	}()
}

// StopTriggers 停止后台循环。
func (s *AppFunctionService) StopTriggers() { s.stopOnce.Do(func() { close(s.triggerStop) }) }

func (s *AppFunctionService) wakeTriggers() {
	select {
	case s.triggerKick <- struct{}{}:
	default:
	}
}

func (s *AppFunctionService) triggerTick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("函数触发器单轮崩溃", zap.Any("panic", r))
		}
	}()
	s.scheduleDue(ctx)
	s.drainTriggerJobs(ctx)
	if time.Since(s.lastPurge) > time.Hour {
		s.lastPurge = time.Now()
		if removed, err := s.pg.PurgeAppFunctionTriggerJobs(ctx, time.Now().Add(-triggerJobRetention)); err == nil && removed > 0 {
			s.log.Info("函数触发器：清理旧任务", zap.Int64("removed", removed))
		}
	}
}

// scheduleDue 到点的定时触发器：比较交换推进下一次时间，推得动的那台实例负责入队。
func (s *AppFunctionService) scheduleDue(ctx context.Context) {
	now := time.Now()
	due, err := s.pg.DueAppFunctionScheduleTriggers(ctx, now, 50)
	if err != nil {
		s.log.Warn("函数触发器：读取到点的定时触发器失败", zap.Error(err))
		return
	}
	for _, trigger := range due {
		expected := *trigger.NextRunAt
		claimed, err := s.pg.AdvanceAppFunctionSchedule(ctx, trigger.ID, expected, nextTriggerRun(trigger, now))
		if err != nil || !claimed {
			continue
		}
		if _, err := s.pg.EnqueueAppFunctionTriggerJob(ctx, functiondomain.TriggerJob{
			AppID: trigger.AppID, FunctionID: trigger.FunctionID, TriggerID: trigger.ID, EventID: uuid.NewString(),
			Kind: functiondomain.TriggerKindSchedule, Data: json.RawMessage(`{}`),
		}, "schedule:"+strconv.FormatInt(expected.Unix(), 10)); err != nil {
			s.log.Warn("函数触发器：定时任务入队失败", zap.Int64("trigger", trigger.ID), zap.Error(err))
		}
	}
}

// drainTriggerJobs 一批一批抢任务执行，直到队列里没有到点的。
func (s *AppFunctionService) drainTriggerJobs(ctx context.Context) {
	for round := 0; round < 50; round++ {
		if ctx.Err() != nil {
			return
		}
		jobs, err := s.pg.ClaimAppFunctionTriggerJobs(ctx, triggerJobBatch, triggerJobLease)
		if err != nil {
			s.log.Warn("函数触发器：抢占任务失败", zap.Error(err))
			return
		}
		if len(jobs) == 0 {
			return
		}
		var wg sync.WaitGroup
		slots := make(chan struct{}, triggerWorkers)
		for _, job := range jobs {
			wg.Add(1)
			slots <- struct{}{}
			go func(job functiondomain.TriggerJob) {
				defer func() {
					<-slots
					wg.Done()
					if r := recover(); r != nil {
						s.log.Error("函数触发任务崩溃", zap.Int64("job", job.ID), zap.Any("panic", r))
					}
				}()
				s.runTriggerJob(ctx, job)
			}(job)
		}
		wg.Wait()
	}
}

func (s *AppFunctionService) runTriggerJob(ctx context.Context, job functiondomain.TriggerJob) {
	trigger, err := s.pg.GetAppFunctionTrigger(ctx, job.AppID, job.FunctionID, job.TriggerID)
	if err != nil {
		s.finishTriggerJob(ctx, job, err)
		return
	}
	if trigger == nil || !trigger.Enabled {
		_ = s.pg.FinishAppFunctionTriggerJob(ctx, job.ID, functiondomain.JobFailed, "触发器已删除或已停用，跳过", nil)
		return
	}
	_, runErr := s.runTriggerInvocation(ctx, trigger.Function, *trigger, job)
	s.finishTriggerJob(ctx, job, runErr)
	if runErr == nil || !triggerRetryable(runErr) || job.Attempts >= functiondomain.MaxTriggerAttempts {
		_ = s.pg.RecordAppFunctionTriggerFire(ctx, trigger.ID, runErr == nil, errorText(runErr))
	}
}

// finishTriggerJob 结束或安排重试。
func (s *AppFunctionService) finishTriggerJob(ctx context.Context, job functiondomain.TriggerJob, runErr error) {
	if runErr == nil {
		_ = s.pg.FinishAppFunctionTriggerJob(ctx, job.ID, functiondomain.JobSucceeded, "", nil)
		return
	}
	if triggerRetryable(runErr) && job.Attempts < functiondomain.MaxTriggerAttempts {
		// 退避：30s、2min、4.5min、8min
		delay := time.Duration(job.Attempts*job.Attempts) * 30 * time.Second
		retryAt := time.Now().Add(delay)
		_ = s.pg.FinishAppFunctionTriggerJob(ctx, job.ID, functiondomain.JobPending, runErr.Error(), &retryAt)
		return
	}
	_ = s.pg.FinishAppFunctionTriggerJob(ctx, job.ID, functiondomain.JobFailed, runErr.Error(), nil)
}

// triggerRetryable 只重试「还没开始执行」的失败：未激活、并发满、频次满、执行中被别人占着、基础设施故障。
//
// 脚本已经开始执行之后的失败（50290、aegis.fail 的业务错误、入参不合契约）一律不重试：
// 脚本不是一个大事务，前面的写入不会回滚，同一个 eventId 也会被幂等挡下（40991）——
// 重试只会让人误以为它「再跑了一遍」。
func triggerRetryable(err error) bool {
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		return true
	}
	switch appErr.Code {
	case 40990, 40992, 40993, 42990, 42991:
		return true
	}
	return false
}

// ── 函数互调 ──

// callNested aegis.functions.call 的宿主实现。
func (s *AppFunctionService) callNested(ctx context.Context, request FunctionCallRequest) (*FunctionCallResult, error) {
	if request.DryRun {
		return s.callNestedDryRun(ctx, request)
	}
	result, err := s.invoke(ctx, request.AppID, request.Name, request.EventID, request.Input, request.Caller,
		invokeOptions{Source: functiondomain.SourceFunction, Depth: request.Depth})
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			// 被调函数的一切拒绝（业务错误、未激活、限流）都以业务错误交还外层脚本，外层可以 catch
			return &FunctionCallResult{Business: &ScriptBusinessError{Code: appErr.Code, Message: appErr.Message}}, nil
		}
		return nil, err
	}
	return &FunctionCallResult{Output: result.Output}, nil
}

// callNestedDryRun 试跑里的互调：被调函数同样只记不做，不写调用记录。
func (s *AppFunctionService) callNestedDryRun(ctx context.Context, request FunctionCallRequest) (*FunctionCallResult, error) {
	function, err := s.GetFunction(ctx, request.AppID, request.Name)
	if err != nil {
		return business(err)
	}
	if function.Status != functiondomain.StatusActive || function.ActiveVersion == "" {
		return &FunctionCallResult{Business: &ScriptBusinessError{Code: 40990, Message: "应用函数尚未激活"}}, nil
	}
	if function.Runtime != functiondomain.RuntimeScript {
		return &FunctionCallResult{Business: &ScriptBusinessError{Code: 40106, Message: "试跑里只能调用 script 运行时的函数"}}, nil
	}
	if err := validateFunctionInput(function.InputSchema, request.Input); err != nil {
		return business(err)
	}
	version, err := s.pg.GetAppFunctionVersion(ctx, request.AppID, function.ID, function.ActiveVersion)
	if err != nil || version == nil {
		return &FunctionCallResult{Business: &ScriptBusinessError{Code: 40992, Message: "激活版本不可用"}}, nil
	}
	appKey := s.resolveAppKey(ctx, request.AppID)
	sdk := newScriptSDK(ctx, s.scriptDeps(), request.AppID, appKey, function.Name, request.EventID, request.Caller,
		function.Capabilities, scriptSDKOptions{Config: function.Config, DryRun: true, FunctionID: function.ID, Depth: request.Depth})
	defer sdk.Release()
	output, err := s.script.Execute(ctx, version.Source, functiondomain.ScriptContext{
		EventID: request.EventID, AppID: request.AppID, AppKey: appKey, Function: function.Name,
		Version: version.Version, Caller: request.Caller, Input: request.Input, DryRun: true,
		Source: functiondomain.SourceFunction, Depth: request.Depth,
	}, sdk, function.MaxResponseBytes)
	if err != nil {
		if failure := sdk.BusinessError(); failure != nil {
			return &FunctionCallResult{Business: failure}, nil
		}
		return nil, fmt.Errorf("被调函数 %s 执行失败：%w", function.Name, err)
	}
	return &FunctionCallResult{Output: output}, nil
}

func business(err error) (*FunctionCallResult, error) {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return &FunctionCallResult{Business: &ScriptBusinessError{Code: appErr.Code, Message: appErr.Message}}, nil
	}
	return nil, err
}

// ── 全景、调用详情、重放 ──

// Overview 一个应用全部函数近 windowHours 小时的运行状况。
func (s *AppFunctionService) Overview(ctx context.Context, appID int64, windowHours int) (*functiondomain.AppOverview, error) {
	return s.pg.AppFunctionOverview(ctx, appID, windowHours)
}

// GetInvocation 一条调用的完整记录（入参、日志、副作用、额度）。
func (s *AppFunctionService) GetInvocation(ctx context.Context, appID int64, name string, invocationID int64) (*functiondomain.Invocation, error) {
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	invocation, err := s.pg.GetAppFunctionInvocation(ctx, appID, function.ID, invocationID)
	if err != nil {
		return nil, err
	}
	if invocation == nil {
		return nil, apperrors.New(40495, http.StatusNotFound, "调用记录不存在")
	}
	return invocation, nil
}

// ReplayInvocation 用一次真实调用的入参与身份，在当前激活版本上试跑一遍（只记不做）。
func (s *AppFunctionService) ReplayInvocation(ctx context.Context, appID int64, name string, invocationID int64,
	adminID *int64) (*functiondomain.TestResult, error) {
	invocation, err := s.GetInvocation(ctx, appID, name, invocationID)
	if err != nil {
		return nil, err
	}
	if invocation.InputTruncated || len(invocation.Input) == 0 {
		return nil, apperrors.New(40130, http.StatusBadRequest, "这次调用的入参超过留存上限，没有留下，无法重放")
	}
	function, err := s.GetFunction(ctx, appID, name)
	if err != nil {
		return nil, err
	}
	if function.Runtime != functiondomain.RuntimeScript {
		return nil, apperrors.New(40106, http.StatusBadRequest, "只有 script 运行时支持重放")
	}
	if function.ActiveVersion == "" {
		return nil, apperrors.New(40990, http.StatusConflict, "应用函数尚未激活")
	}
	version, err := s.pg.GetAppFunctionVersion(ctx, appID, function.ID, function.ActiveVersion)
	if err != nil {
		return nil, err
	}
	if version == nil {
		return nil, apperrors.New(40992, http.StatusConflict, "激活版本不可用")
	}
	request := functiondomain.TestRequest{Source: version.Source, Input: invocation.Input}
	if invocation.CallerID != nil && (invocation.CallerType == functiondomain.CallerUser || invocation.CallerType == functiondomain.CallerEvent) {
		request.AsUserID = *invocation.CallerID
	}
	return s.TestScript(ctx, appID, name, request, adminID)
}
