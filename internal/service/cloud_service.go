package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	cloudstorage "aegis/internal/domain/cloudstorage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 云服务（docs/cloud-service.md）：云存储升级后的三块新能力。
//
//	档位     按会员档位区分空间与同步服务（cloudstorage.Plan），写入、配额、回收站都按用户此刻的档位算
//	同步     记录级的跨设备同步：听歌、影视、番剧、短剧、小说、听书……逐条最后写入者胜、按序号增量拉取
//	设备     登记过的同步设备、各自同步了什么、正在播放 / 阅读什么，以及把一条记录接力到另一台设备
//
// 实时：被接受的写入、设备变化、正在播放、接力都经 UserEventPublisher 推给同一用户的全部在线连接，
// 事件只是「去拉一下」的提示（不带记录内容），漏掉一个也只是晚一点同步 —— 客户端另按 pollSeconds 兜底轮询。

// 云服务新增的错误码（docs/cloud-service.md）。
const (
	errCodeCloudSyncRecordLimit     = 41363 // 413 同步记录条数已达档位上限
	errCodeCloudSyncDeviceLimit     = 41364 // 413 同步设备数已达档位上限
	errCodeCloudSyncBatchTooLarge   = 41365 // 413 一批改动超过上限
	errCodeCloudSyncInvalidRecord   = 42264 // 422 集合名 / 记录 ID / 内容 / 时间不合法
	errCodeCloudSyncCollectionPlan  = 42265 // 422 当前档位不能同步这个集合
	errCodeCloudSyncDisabledPlan    = 42266 // 422 当前档位没有同步服务
	errCodeCloudSyncHandoffPlan     = 42267 // 422 当前档位没有接力
	errCodeCloudSyncInvalidDevice   = 42268 // 422 设备号缺失或格式不对
	errCodeCloudSyncDeviceNotFound  = 40434 // 404 设备没登记或已被移除
	errCodeCloudSyncHandoffNotFound = 40435 // 404 接力请求不存在、已过期或已领取
)

// 推给客户端的实时事件类型。
const (
	cloudEventSync     = "cloud.sync"
	cloudEventDevices  = "cloud.devices"
	cloudEventActivity = "cloud.activity"
	cloudEventHandoff  = "cloud.handoff"
)

const (
	cloudHistoryUsersPerRound = 500
	cloudTombstonesPerRound   = 5000
)

// SetEventPublisher 接上实时推送（没有时只是不推，客户端靠轮询）。
func (s *CloudStorageService) SetEventPublisher(publisher UserEventPublisher) { s.events = publisher }

func (s *CloudStorageService) publish(ctx context.Context, appID, userID int64, eventType string, data map[string]any) {
	if s.events == nil {
		return
	}
	if err := s.events.PublishUserEvent(context.WithoutCancel(ctx), appID, userID, eventType, data); err != nil {
		s.log.Debug("云服务实时事件推送失败", zap.String("type", eventType), zap.Error(err))
	}
}

// ════════════════════════════════════════════════════════════
//  档位
// ════════════════════════════════════════════════════════════

// userPlan 用户此刻生效的档位（持有的会员档位 → 档位表），以及套上档位之后的配置。
func (s *CloudStorageService) userPlan(ctx context.Context, cfg cloudstorage.Config, userID int64) (cloudstorage.Config, cloudstorage.PlanView, error) {
	holdings, err := s.pg.GetVipHoldings(ctx, cfg.AppID, userID)
	if err != nil {
		return cfg, cloudstorage.PlanView{}, err
	}
	ids := make([]int64, 0, len(holdings.Tiers))
	names := make(map[int64]string, len(holdings.Tiers))
	for _, tier := range holdings.Tiers {
		ids = append(ids, tier.ID)
		names[tier.ID] = tier.Name
	}
	plan := cfg.ResolvePlan(ids)
	view := cloudstorage.PlanView{Plan: plan, Current: true, TierName: names[plan.TierID]}
	if view.Name == "" {
		view.Name = view.TierName
	}
	if view.Name == "" {
		view.Name = "免费"
	}
	return cfg.WithPlan(plan), view, nil
}

// userConfig 已开启云存储的应用里，按用户档位套好限制的配置。
func (s *CloudStorageService) userConfig(ctx context.Context, appID, userID int64) (cloudstorage.Config, cloudstorage.PlanView, error) {
	cfg, err := s.enabledConfig(ctx, appID)
	if err != nil {
		return cfg, cloudstorage.PlanView{}, err
	}
	return s.userPlan(ctx, cfg, userID)
}

// Plans 档位对比：免费档在前，其余按优先级倒序（便宜的在前），标出用户当前所在的档位。
func (s *CloudStorageService) Plans(ctx context.Context, appID, userID int64) ([]cloudstorage.PlanView, error) {
	cfg, err := s.enabledConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	_, current, err := s.userPlan(ctx, cfg, userID)
	if err != nil {
		return nil, err
	}
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	tierNames := make(map[int64]string, len(tiers))
	for _, tier := range tiers {
		tierNames[tier.ID] = tier.Name
	}
	free := cfg.FreePlan()
	out := []cloudstorage.PlanView{planView(free, "", current)}
	paid := []cloudstorage.PlanView{}
	for _, plan := range cfg.Plans {
		if plan.TierID == cloudstorage.FreeTierID {
			continue
		}
		name, ok := tierNames[plan.TierID]
		if !ok {
			// 档位已被删掉：这一档谁也拿不到，不展示。
			continue
		}
		paid = append(paid, planView(plan, name, current))
	}
	for i := len(paid) - 1; i >= 0; i-- {
		out = append(out, paid[i])
	}
	return out, nil
}

func planView(plan cloudstorage.Plan, tierName string, current cloudstorage.PlanView) cloudstorage.PlanView {
	view := cloudstorage.PlanView{Plan: plan, TierName: tierName, Current: plan.TierID == current.TierID}
	if view.Name == "" {
		view.Name = tierName
	}
	if view.Name == "" {
		view.Name = "免费"
	}
	return view
}

// ════════════════════════════════════════════════════════════
//  设备
// ════════════════════════════════════════════════════════════

// CloudDeviceInput 设备登记：会话里的设备信息为准，请求里的只补会话没有的。
type CloudDeviceInput struct {
	cloudstorage.DeviceInfo
	// Rename 为真时 Name 是用户起的名字（之后不再随型号刷新）。
	Rename bool
}

func cleanDeviceInput(info cloudstorage.DeviceInfo) cloudstorage.DeviceInfo {
	cut := func(value string, limit int) string {
		value = strings.TrimSpace(value)
		if runes := []rune(value); len(runes) > limit {
			return string(runes[:limit])
		}
		return value
	}
	info.DeviceID = strings.TrimSpace(info.DeviceID)
	info.Name = cloudstorage.TruncateDeviceName(strings.TrimSpace(info.Name))
	info.Platform = cut(strings.ToLower(info.Platform), 32)
	info.Model = cut(info.Model, 64)
	info.OS = cut(info.OS, 32)
	info.OSVersion = cut(info.OSVersion, 32)
	info.AppVersion = cut(info.AppVersion, 32)
	return info
}

// RegisterDevice 登记（或刷新）当前设备。新设备超出档位设备数时回 41364，客户端引导用户移除一台旧设备。
func (s *CloudStorageService) RegisterDevice(ctx context.Context, appID, userID int64, input CloudDeviceInput) (*cloudstorage.SyncDevice, *cloudstorage.PlanView, error) {
	_, plan, err := s.userConfig(ctx, appID, userID)
	if err != nil {
		return nil, nil, err
	}
	if !plan.SyncEnabled {
		return nil, &plan, syncDisabledError(plan)
	}
	info := cleanDeviceInput(input.DeviceInfo)
	if !cloudstorage.ValidDeviceID(info.DeviceID) {
		return nil, &plan, invalidDeviceError()
	}
	if info.Name == "" && !input.Rename {
		info.Name = info.Model
	}
	if input.Rename && info.Name == "" {
		return nil, &plan, apperrors.New(40000, http.StatusBadRequest, "设备名称不能为空")
	}
	device, created, err := s.pg.RegisterCloudSyncDevice(ctx, appID, userID, info, input.Rename, plan.MaxDevices, time.Now())
	if err != nil {
		return nil, &plan, mapCloudSyncError(err, plan)
	}
	device.Current = true
	device.Online = true
	if created || input.Rename {
		s.publish(ctx, appID, userID, cloudEventDevices, map[string]any{"deviceId": info.DeviceID, "change": "registered"})
	}
	return device, &plan, nil
}

// ListDevices 登记过的设备（在线状态按最近一次心跳算）。
func (s *CloudStorageService) ListDevices(ctx context.Context, appID, userID int64, currentDevice string) ([]cloudstorage.SyncDevice, error) {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return nil, err
	}
	return s.listDevices(ctx, appID, userID, currentDevice)
}

func (s *CloudStorageService) listDevices(ctx context.Context, appID, userID int64, currentDevice string) ([]cloudstorage.SyncDevice, error) {
	devices, err := s.pg.ListCloudSyncDevices(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range devices {
		devices[i].Current = devices[i].DeviceID == currentDevice
		devices[i].Online = devices[i].Current || now.Sub(devices[i].LastSeenAt) <= cloudstorage.DeviceOnlineWindow
		// 下线的设备不再显示「正在播放」：那只是它最后一刻的状态。
		if !devices[i].Online && devices[i].Activity != nil && devices[i].Activity.State != cloudstorage.ActivityIdle {
			devices[i].Activity.State = cloudstorage.ActivityPaused
		}
	}
	// 当前设备排第一，其余在线的在前。
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].Current != devices[j].Current {
			return devices[i].Current
		}
		return devices[i].Online && !devices[j].Online
	})
	return devices, nil
}

// RenameDevice 给一台设备改名（可以改别的设备）。
func (s *CloudStorageService) RenameDevice(ctx context.Context, appID, userID int64, deviceID, name string) error {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return err
	}
	name = cloudstorage.TruncateDeviceName(strings.TrimSpace(name))
	if name == "" {
		return apperrors.New(40000, http.StatusBadRequest, "设备名称不能为空")
	}
	if err := s.pg.RenameCloudSyncDevice(ctx, appID, userID, deviceID, name); err != nil {
		return mapCloudSyncError(err, cloudstorage.PlanView{})
	}
	s.publish(ctx, appID, userID, cloudEventDevices, map[string]any{"deviceId": deviceID, "change": "renamed"})
	return nil
}

// RemoveDevice 移除一台设备：腾出名额，它之后的推送被拒（直到重新登记）。它写过的记录保留。
func (s *CloudStorageService) RemoveDevice(ctx context.Context, appID, userID int64, deviceID string) error {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return err
	}
	if err := s.pg.RemoveCloudSyncDevice(ctx, appID, userID, deviceID); err != nil {
		return mapCloudSyncError(err, cloudstorage.PlanView{})
	}
	s.publish(ctx, appID, userID, cloudEventDevices, map[string]any{"deviceId": deviceID, "change": "removed"})
	return nil
}

// SetActivity 上报这台设备正在播放 / 阅读的内容（activity 为 nil 表示停了）。
func (s *CloudStorageService) SetActivity(ctx context.Context, appID, userID int64, deviceID string, activity *cloudstorage.Activity) error {
	_, plan, err := s.userConfig(ctx, appID, userID)
	if err != nil {
		return err
	}
	if !plan.SyncEnabled {
		return syncDisabledError(plan)
	}
	now := time.Now()
	if activity != nil {
		if !cloudstorage.ValidCollection(activity.Collection) || !cloudstorage.ValidActivityState(activity.State) {
			return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "播放状态格式无效")
		}
		if activity.RecordID != "" && !cloudstorage.ValidRecordID(activity.RecordID) {
			return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "记录 ID 格式无效")
		}
		if len(activity.Payload) > 0 {
			if len(activity.Payload) > cloudstorage.MaxActivityBytes || !isJSONObject(activity.Payload) {
				return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
					fmt.Sprintf("播放状态需为不超过 %s 的 JSON 对象", cloudstorage.FormatBytes(cloudstorage.MaxActivityBytes)))
			}
		}
		activity.At = now
	}
	if err := s.pg.SetCloudSyncActivity(ctx, appID, userID, deviceID, activity, now); err != nil {
		return mapCloudSyncError(err, plan)
	}
	if plan.Realtime {
		data := map[string]any{"deviceId": deviceID}
		if activity != nil {
			data["activity"] = activity
		}
		s.publish(ctx, appID, userID, cloudEventActivity, data)
	}
	return nil
}

// Heartbeat 设备心跳（前台时每分钟一次），让别的设备看到它在线。
func (s *CloudStorageService) Heartbeat(ctx context.Context, appID, userID int64, deviceID string) error {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return err
	}
	ok, err := s.pg.TouchCloudSyncDevice(ctx, appID, userID, deviceID, time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return mapCloudSyncError(pgrepo.ErrCloudSyncDeviceUnknown, cloudstorage.PlanView{})
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  同步
// ════════════════════════════════════════════════════════════

// Push 推一批改动。被接受的立即可被别的设备拉到，并实时提示它们。
func (s *CloudStorageService) Push(ctx context.Context, appID, userID int64, deviceID string, changes []cloudstorage.SyncChange) (*cloudstorage.SyncPushResult, error) {
	cfg, plan, err := s.userConfig(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if !plan.SyncEnabled {
		return nil, syncDisabledError(plan)
	}
	if !cloudstorage.ValidDeviceID(deviceID) {
		return nil, invalidDeviceError()
	}
	if len(changes) > cloudstorage.MaxSyncPushRecords {
		return nil, apperrors.New(errCodeCloudSyncBatchTooLarge, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("一次最多推送 %d 条改动", cloudstorage.MaxSyncPushRecords))
	}
	now := time.Now()
	collections := map[string]bool{}
	for i := range changes {
		change := &changes[i]
		if err := normalizeSyncChange(change, now); err != nil {
			return nil, err
		}
		if !plan.AllowsCollection(change.Collection) {
			info := cloudstorage.LookupCollection(change.Collection)
			return nil, apperrors.New(errCodeCloudSyncCollectionPlan, http.StatusUnprocessableEntity,
				fmt.Sprintf("「%s」档位不支持同步「%s」", plan.Name, info.Name))
		}
		collections[change.Collection] = true
	}
	if len(changes) == 0 {
		result := &cloudstorage.SyncPushResult{Rejected: []cloudstorage.SyncRecord{}, Applied: []cloudstorage.SyncAck{}}
		state, err := s.pg.GetCloudSyncState(ctx, appID, userID)
		if err != nil {
			return nil, err
		}
		result.Seq = state.Seq
		return result, nil
	}
	result, err := s.pg.PushCloudSync(ctx, pgrepo.CloudSyncPushPlan{
		AppID: appID, UserID: userID, DeviceID: deviceID, Changes: changes, Now: now,
		QuotaBytes: cfg.QuotaBytes, MaxSyncRecords: plan.MaxSyncRecords,
	})
	if err != nil {
		return nil, mapCloudSyncError(err, plan)
	}
	if result.Accepted > 0 && plan.Realtime {
		touched := make([]string, 0, len(collections))
		for key := range collections {
			touched = append(touched, key)
		}
		sort.Strings(touched)
		s.publish(ctx, appID, userID, cloudEventSync, map[string]any{
			"seq": result.Seq, "deviceId": deviceID, "collections": touched, "count": result.Accepted,
		})
	}
	return result, nil
}

func normalizeSyncChange(change *cloudstorage.SyncChange, now time.Time) error {
	change.Collection = strings.TrimSpace(change.Collection)
	if !cloudstorage.ValidCollection(change.Collection) {
		return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
			fmt.Sprintf("集合名 %q 格式无效：需为「领域.名称」，如 music.history", change.Collection))
	}
	if !cloudstorage.ValidRecordID(change.ID) {
		return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
			"记录 ID 需为 1 到 200 个可见 ASCII 字符")
	}
	if change.UpdatedAt.IsZero() {
		return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "记录缺少修改时间 updatedAt")
	}
	// 毫秒精度：各端的时间戳都是毫秒，留微秒会让「同一时刻」在不同端比较出不同结果。
	change.UpdatedAt = change.UpdatedAt.UTC().Truncate(time.Millisecond)
	if limit := now.Add(cloudstorage.MaxClockSkew); change.UpdatedAt.After(limit) {
		change.UpdatedAt = now.UTC().Truncate(time.Millisecond)
	}
	if change.Deleted {
		change.Payload = nil
		return nil
	}
	if len(change.Payload) == 0 || !isJSONObject(change.Payload) {
		return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
			fmt.Sprintf("记录 %s/%s 的内容需为 JSON 对象", change.Collection, change.ID))
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, change.Payload); err != nil {
		return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "记录内容不是合法的 JSON")
	}
	if compacted.Len() > cloudstorage.MaxSyncRecordBytes {
		return apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
			fmt.Sprintf("单条记录不能超过 %s", cloudstorage.FormatBytes(cloudstorage.MaxSyncRecordBytes)))
	}
	change.Payload = compacted.Bytes()
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 1 && trimmed[0] == '{' && json.Valid(trimmed)
}

// Changes 增量拉取。拉取也算一次心跳。
func (s *CloudStorageService) Changes(ctx context.Context, query cloudstorage.SyncChangesQuery, deviceID string) (*cloudstorage.SyncChangesPage, error) {
	_, plan, err := s.userConfig(ctx, query.AppID, query.UserID)
	if err != nil {
		return nil, err
	}
	if !plan.SyncEnabled {
		return nil, syncDisabledError(plan)
	}
	if query.Limit <= 0 {
		query.Limit = cloudstorage.DefaultSyncPullLimit
	}
	query.Limit = min(query.Limit, cloudstorage.MaxSyncPullRecords)
	if query.Since < 0 {
		query.Since = 0
	}
	cleaned := query.Collections[:0]
	for _, collection := range query.Collections {
		collection = strings.TrimSpace(collection)
		if collection == "" {
			continue
		}
		if !cloudstorage.ValidCollection(collection) {
			return nil, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
				fmt.Sprintf("集合名 %q 格式无效", collection))
		}
		cleaned = append(cleaned, collection)
	}
	query.Collections = cleaned
	page, err := s.pg.ListCloudSyncChanges(ctx, query)
	if err != nil {
		return nil, err
	}
	if deviceID != "" {
		if _, err := s.pg.TouchCloudSyncDevice(ctx, query.AppID, query.UserID, deviceID, time.Now()); err != nil {
			s.log.Debug("同步设备心跳失败", zap.Error(err))
		}
	}
	return page, nil
}

// Records 浏览同步记录（查看别的设备同步了什么），不含墓碑。
func (s *CloudStorageService) Records(ctx context.Context, query cloudstorage.SyncRecordQuery) (*cloudstorage.SyncRecordPage, error) {
	if _, err := s.enabledConfig(ctx, query.AppID); err != nil {
		return nil, err
	}
	if query.Collection != "" && !cloudstorage.ValidCollection(query.Collection) {
		return nil, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "集合名格式无效")
	}
	if query.Domain != "" && !cloudstorage.ValidCollection(query.Domain+".x") {
		return nil, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "领域名格式无效")
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 30
	}
	if runes := []rune(strings.TrimSpace(query.Keyword)); len(runes) > 64 {
		query.Keyword = string(runes[:64])
	}
	return s.pg.ListCloudSyncRecords(ctx, query)
}

// Record 一条记录（含墓碑）。
func (s *CloudStorageService) Record(ctx context.Context, appID, userID int64, collection, id string) (*cloudstorage.SyncRecord, error) {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return nil, err
	}
	if !cloudstorage.ValidCollection(collection) || !cloudstorage.ValidRecordID(id) {
		return nil, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "集合名或记录 ID 格式无效")
	}
	record, err := s.pg.GetCloudSyncRecord(ctx, appID, userID, collection, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, apperrors.New(errCodeCloudItemNotFound, http.StatusNotFound, "记录不存在")
	}
	return record, nil
}

// ClearCollection 清空一个集合：全部改成墓碑，别的设备拉到后跟着删。
func (s *CloudStorageService) ClearCollection(ctx context.Context, appID, userID int64, collection, deviceID string) (int64, error) {
	_, plan, err := s.userConfig(ctx, appID, userID)
	if err != nil {
		return 0, err
	}
	if !cloudstorage.ValidCollection(collection) {
		return 0, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "集合名格式无效")
	}
	deleted, seq, err := s.pg.DeleteCloudSyncCollection(ctx, appID, userID, collection, deviceID, time.Now())
	if err != nil {
		return 0, mapCloudSyncError(err, plan)
	}
	if deleted > 0 && plan.Realtime {
		s.publish(ctx, appID, userID, cloudEventSync, map[string]any{
			"seq": seq, "deviceId": deviceID, "collections": []string{collection}, "count": deleted,
		})
	}
	return deleted, nil
}

// Overview 云服务首页：档位、用量、设备、各集合（含当前档位不能同步、但云端还留着的集合）。
func (s *CloudStorageService) Overview(ctx context.Context, appID, userID int64, currentDevice string) (*cloudstorage.SyncOverview, error) {
	cfg, plan, err := s.userConfig(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	state, err := s.pg.GetCloudSyncState(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	quota := cfg.QuotaBytes
	if state.QuotaOverride != nil && *state.QuotaOverride > 0 {
		quota = *state.QuotaOverride
		plan.Override = true
		plan.QuotaBytes = quota
	}
	devices, err := s.listDevices(ctx, appID, userID, currentDevice)
	if err != nil {
		return nil, err
	}
	usage, err := s.pg.CloudSyncCollectionUsage(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for i := range usage {
		usage[i].Allowed = plan.AllowsCollection(usage[i].Key)
		present[usage[i].Key] = true
	}
	// 目录里的集合都列出来（没有记录的计 0），客户端能看到每个领域都在同步。
	for _, info := range cloudstorage.SyncCollections {
		if present[info.Key] {
			continue
		}
		usage = append(usage, cloudstorage.CollectionUsage{
			CollectionInfo: info, ByDevice: map[string]int64{}, Allowed: plan.AllowsCollection(info.Key),
		})
	}
	sort.SliceStable(usage, func(i, j int) bool { return collectionOrder(usage[i].Key) < collectionOrder(usage[j].Key) })
	return &cloudstorage.SyncOverview{
		Plan: plan,
		Usage: cloudstorage.Usage{
			QuotaBytes: quota, UsedBytes: state.ItemBytes + state.SyncBytes, ItemBytes: state.ItemBytes,
			SyncBytes: state.SyncBytes, SyncRecords: state.SyncRecords, Devices: len(devices),
		},
		Devices:     devices,
		Collections: usage,
		Domains:     cloudstorage.SyncDomains,
		Seq:         state.Seq,
		ServerTime:  time.Now(),
	}, nil
}

// collectionOrder 目录顺序优先，目录外的排在后面按名字。
func collectionOrder(key string) string {
	for i, info := range cloudstorage.SyncCollections {
		if info.Key == key {
			return fmt.Sprintf("0%03d", i)
		}
	}
	return "1" + key
}

// ════════════════════════════════════════════════════════════
//  接力
// ════════════════════════════════════════════════════════════

// CloudHandoffInput 发起接力。
type CloudHandoffInput struct {
	FromDeviceID string
	ToDeviceID   string
	Collection   string
	RecordID     string
	Payload      json.RawMessage
}

// CreateHandoff 把一条记录发到另一台设备上继续（目标设备实时收到提示，离线的下次上线时领取）。
func (s *CloudStorageService) CreateHandoff(ctx context.Context, appID, userID int64, input CloudHandoffInput) (*cloudstorage.Handoff, error) {
	_, plan, err := s.userConfig(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if !plan.SyncEnabled {
		return nil, syncDisabledError(plan)
	}
	if !plan.Handoff {
		return nil, apperrors.New(errCodeCloudSyncHandoffPlan, http.StatusUnprocessableEntity,
			fmt.Sprintf("「%s」档位不支持设备接力", plan.Name))
	}
	if !cloudstorage.ValidDeviceID(input.FromDeviceID) || !cloudstorage.ValidDeviceID(input.ToDeviceID) {
		return nil, invalidDeviceError()
	}
	if input.FromDeviceID == input.ToDeviceID {
		return nil, apperrors.New(40000, http.StatusBadRequest, "不能接力到当前设备")
	}
	if !cloudstorage.ValidCollection(input.Collection) || !cloudstorage.ValidRecordID(input.RecordID) {
		return nil, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity, "集合名或记录 ID 格式无效")
	}
	if len(input.Payload) > 0 && (len(input.Payload) > cloudstorage.MaxSyncRecordBytes || !isJSONObject(input.Payload)) {
		return nil, apperrors.New(errCodeCloudSyncInvalidRecord, http.StatusUnprocessableEntity,
			fmt.Sprintf("接力内容需为不超过 %s 的 JSON 对象", cloudstorage.FormatBytes(cloudstorage.MaxSyncRecordBytes)))
	}
	now := time.Now()
	handoff, err := s.pg.CreateCloudSyncHandoff(ctx, appID, userID, cloudstorage.Handoff{
		FromDeviceID: input.FromDeviceID, ToDeviceID: input.ToDeviceID, Collection: input.Collection,
		RecordID: input.RecordID, Payload: input.Payload, CreatedAt: now, ExpiresAt: now.Add(cloudstorage.HandoffTTL),
	})
	if err != nil {
		return nil, mapCloudSyncError(err, plan)
	}
	s.publish(ctx, appID, userID, cloudEventHandoff, map[string]any{"handoff": handoff})
	return handoff, nil
}

// PendingHandoffs 发给这台设备、还没领取的接力。
func (s *CloudStorageService) PendingHandoffs(ctx context.Context, appID, userID int64, deviceID string) ([]cloudstorage.Handoff, error) {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return nil, err
	}
	if !cloudstorage.ValidDeviceID(deviceID) {
		return nil, invalidDeviceError()
	}
	return s.pg.ListPendingCloudSyncHandoffs(ctx, appID, userID, deviceID, time.Now())
}

// AckHandoff 领取（打开或忽略）一次接力。
func (s *CloudStorageService) AckHandoff(ctx context.Context, appID, userID, id int64, deviceID string) error {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return err
	}
	if err := s.pg.AckCloudSyncHandoff(ctx, appID, userID, id, deviceID, time.Now()); err != nil {
		return mapCloudSyncError(err, cloudstorage.PlanView{})
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  后台清理：墓碑、按档位的历史保留期
// ════════════════════════════════════════════════════════════

// purgeSync 一个应用的同步清理：过了墓碑保留期的墓碑、超出档位历史保留期的历史记录。
func (s *CloudStorageService) purgeSync(ctx context.Context, cfg cloudstorage.Config) (int64, error) {
	now := time.Now()
	total, err := s.pg.PurgeCloudSyncTombstones(ctx, cfg.AppID, now.Add(-cloudstorage.TombstoneRetention), cloudTombstonesPerRound)
	if err != nil {
		return 0, err
	}
	shortest := cfg.MinHistoryRetentionDays()
	if shortest <= 0 {
		return total, nil
	}
	collections := cloudstorage.HistoryCollections()
	users, err := s.pg.ListCloudSyncHistoryUsers(ctx, cfg.AppID, collections, now.AddDate(0, 0, -shortest), cloudHistoryUsersPerRound)
	if err != nil {
		return total, err
	}
	for _, userID := range users {
		_, plan, err := s.userPlan(ctx, cfg, userID)
		if err != nil {
			return total, err
		}
		if plan.HistoryRetentionDays <= 0 {
			continue
		}
		trimmed, err := s.pg.TrimCloudSyncHistory(ctx, cfg.AppID, userID, collections, now.AddDate(0, 0, -plan.HistoryRetentionDays))
		if err != nil {
			return total, err
		}
		total += trimmed
	}
	return total, nil
}

// ════════════════════════════════════════════════════════════
//  错误
// ════════════════════════════════════════════════════════════

func syncDisabledError(plan cloudstorage.PlanView) error {
	return apperrors.New(errCodeCloudSyncDisabledPlan, http.StatusUnprocessableEntity,
		fmt.Sprintf("「%s」档位没有多设备同步服务", plan.Name))
}

func invalidDeviceError() error {
	return apperrors.New(errCodeCloudSyncInvalidDevice, http.StatusUnprocessableEntity,
		"设备号缺失或格式无效：需为 1 到 128 位字母、数字或 . _ : -")
}

func mapCloudSyncError(err error, plan cloudstorage.PlanView) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrCloudSyncDeviceLimit):
		return apperrors.New(errCodeCloudSyncDeviceLimit, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("「%s」档位最多同步 %d 台设备，请先移除一台旧设备", plan.Name, plan.MaxDevices))
	case errors.Is(err, pgrepo.ErrCloudSyncRecordLimit):
		return apperrors.New(errCodeCloudSyncRecordLimit, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("「%s」档位最多同步 %d 条记录", plan.Name, plan.MaxSyncRecords))
	case errors.Is(err, pgrepo.ErrCloudSyncDeviceUnknown):
		return apperrors.New(errCodeCloudSyncDeviceNotFound, http.StatusNotFound, "设备未登记或已被移除，请重新开启同步")
	case errors.Is(err, pgrepo.ErrCloudSyncHandoffNotFound):
		return apperrors.New(errCodeCloudSyncHandoffNotFound, http.StatusNotFound, "接力请求不存在或已过期")
	}
	return mapCloudError(err)
}

// ════════════════════════════════════════════════════════════
//  管理端
// ════════════════════════════════════════════════════════════

// AdminSyncRecords 管理端查看某个用户的同步记录（含墓碑）。
func (s *CloudStorageService) AdminSyncRecords(ctx context.Context, query cloudstorage.SyncRecordQuery) (*cloudstorage.SyncRecordPage, error) {
	if err := s.requireAppUser(ctx, query.AppID, query.UserID); err != nil {
		return nil, err
	}
	query.IncludeDeleted = true
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 30
	}
	return s.pg.ListCloudSyncRecords(ctx, query)
}

// AdminRemoveDevice 管理端移除某个用户的一台同步设备。
func (s *CloudStorageService) AdminRemoveDevice(ctx context.Context, appID, userID int64, deviceID string) error {
	if err := s.requireAppUser(ctx, appID, userID); err != nil {
		return err
	}
	if err := s.pg.RemoveCloudSyncDevice(ctx, appID, userID, deviceID); err != nil {
		return mapCloudSyncError(err, cloudstorage.PlanView{})
	}
	s.publish(ctx, appID, userID, cloudEventDevices, map[string]any{"deviceId": deviceID, "change": "removed"})
	return nil
}
