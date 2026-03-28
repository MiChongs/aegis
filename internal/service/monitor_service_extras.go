package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// GetRuntimeInfo 返回当前系统运行时完整信息（供管理员 API 调用）
func (s *MonitorService) GetRuntimeInfo() MonitorRuntime {
	return s.runtimeSnapshot(time.Now().UTC())
}

func (s *MonitorService) runtimeSnapshot(checkedAt time.Time) MonitorRuntime {
	startedAt := s.startedAt
	if startedAt.IsZero() {
		startedAt = checkedAt
	}
	uptime := checkedAt.Sub(startedAt)
	if uptime < 0 {
		uptime = 0
	}

	rt := MonitorRuntime{
		AppName:       strings.TrimSpace(s.cfg.AppName),
		Environment:   strings.TrimSpace(s.cfg.AppEnv),
		Port:          s.cfg.HTTPPort,
		CheckedAt:     checkedAt,
		StartedAt:     startedAt,
		UptimeSeconds: int64(uptime / time.Second),
		Timezone:      "Asia/Shanghai",
	}

	// Go 运行时
	rt.GoVersion = runtime.Version()
	rt.GoOS = runtime.GOOS
	rt.GoArch = runtime.GOARCH
	rt.Goroutines = runtime.NumGoroutine()
	rt.CGOCalls = runtime.NumCgoCall()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	rt.MemAlloc = memStats.Alloc
	rt.MemTotalAlloc = memStats.TotalAlloc
	rt.MemSys = memStats.Sys
	rt.NumGC = memStats.NumGC
	rt.LastGCTime = int64(memStats.LastGC)

	// 进程
	rt.PID = os.Getpid()

	// 系统信息（gopsutil，错误时静默跳过）
	if info, err := host.Info(); err == nil {
		rt.Hostname = info.Hostname
		rt.OS = info.OS
		rt.Platform = info.Platform
		rt.PlatformVer = info.PlatformVersion
		rt.KernelArch = info.KernelArch
		rt.KernelVer = info.KernelVersion
	}

	// CPU
	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		rt.CPUModel = cpus[0].ModelName
		rt.CPUCores, _ = cpu.Counts(false) // 物理核心
		rt.CPUThreads, _ = cpu.Counts(true) // 逻辑线程
	}
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		rt.CPUUsage = pcts[0]
	}

	// 系统内存
	if vm, err := mem.VirtualMemory(); err == nil {
		rt.MemTotal = vm.Total
		rt.MemUsed = vm.Used
		rt.MemFree = vm.Free
		rt.MemUsedPct = vm.UsedPercent
	}

	// 磁盘（根目录 / 或 C:\）
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = "C:\\"
	}
	if du, err := disk.Usage(diskPath); err == nil {
		rt.DiskTotal = du.Total
		rt.DiskUsed = du.Used
		rt.DiskFree = du.Free
		rt.DiskUsedPct = du.UsedPercent
	}

	// 进程内存
	if proc, err := process.NewProcess(int32(rt.PID)); err == nil {
		if mi, err := proc.MemoryInfo(); err == nil {
			rt.ProcessMem = mi.RSS
		}
	}

	return rt
}

func (s *MonitorService) buildSystemEndpoints() []MonitorEndpoint {
	wsStatus := monitorStatusUnavailable
	if s.realtime != nil {
		wsStatus = monitorStatusAvailable
	}
	endpoints := []MonitorEndpoint{
		{Key: "healthz", Name: "存活探针", Method: http.MethodGet, Path: "/healthz", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回当前进程存活状态。"},
		{Key: "readyz", Name: "就绪探针", Method: http.MethodGet, Path: "/readyz", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回核心依赖就绪状态。", DependsOn: []string{"postgres", "redis"}},
		{Key: "monitor_system", Name: "系统监测", Method: http.MethodGet, Path: "/api/system/monitor", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回系统级监测概览。"},
		{Key: "monitor_apps", Name: "应用监测清单", Method: http.MethodGet, Path: "/api/system/monitor/apps", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回全部应用的监测摘要。"},
		{Key: "monitor_components", Name: "系统组件清单", Method: http.MethodGet, Path: "/api/system/monitor/components", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回系统级组件与模块清单。"},
		{Key: "monitor_app", Name: "应用监测详情", Method: http.MethodGet, Path: "/api/system/monitor/apps/:appid", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回单应用监测详情。"},
		{Key: "monitor_app_components", Name: "应用组件清单", Method: http.MethodGet, Path: "/api/system/monitor/apps/:appid/components", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回单应用组件与入口清单。"},
		{Key: "openapi", Name: "OpenAPI 文档", Method: http.MethodGet, Path: "/openapi.json", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回 OpenAPI JSON。"},
		{Key: "docs", Name: "在线文档", Method: http.MethodGet, Path: "/docs", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回 API 在线文档。"},
		{Key: "public_apps", Name: "应用公开信息", Method: http.MethodGet, Path: "/api/app/public", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "返回应用公开清单。"},
		{Key: "avatar", Name: "头像入口", Method: http.MethodGet, Path: "/api/avatar/:hash", Scope: "public", Protected: false, Status: monitorStatusAvailable, Summary: "可访问", Description: "头像查询与回退入口。", DependsOn: []string{"avatar", "storage"}},
		{Key: "websocket", Name: "WebSocket 网关", Method: http.MethodGet, Path: "/api/ws", Scope: "public", Protected: true, Status: wsStatus, Summary: moduleSummary("WebSocket", wsStatus), Description: "实时事件与在线状态入口。", DependsOn: []string{"realtime", "redis", "nats"}},
	}
	return endpoints
}

func (s *MonitorService) buildControlPlaneModules(ctx context.Context, infra map[string]string, checkedAt time.Time) []MonitorComponent {
	return []MonitorComponent{
		s.httpAPIComponent(checkedAt),
		s.monitorAPIComponent(checkedAt),
		s.docsAPIComponent(checkedAt),
		s.corsComponent(checkedAt),
		s.firewallComponent(infra, checkedAt),
		s.systemSettingsComponent(ctx, infra, checkedAt),
	}
}

func (s *MonitorService) httpAPIComponent(checkedAt time.Time) MonitorComponent {
	status := evaluateStatus(s.auth != nil && s.app != nil && s.user != nil, nil, nil)
	return MonitorComponent{Key: "http_api", Name: "HTTP API", Status: status, Severity: severityFromStatus(status), Available: status == monitorStatusAvailable, Summary: moduleSummary("HTTP API", status), Detail: "HTTP 路由与核心服务装配完成。", CheckedAt: checkedAt, Meta: map[string]any{"port": s.cfg.HTTPPort, "environment": s.cfg.AppEnv}}
}

func (s *MonitorService) monitorAPIComponent(checkedAt time.Time) MonitorComponent {
	status := evaluateStatus(true, nil, nil)
	return MonitorComponent{Key: "monitor", Name: "监测服务", Status: status, Severity: severityFromStatus(status), Available: true, Summary: "正常", Detail: "系统与应用监测能力已装配。", CheckedAt: checkedAt, Meta: map[string]any{"endpointCount": len(s.buildSystemEndpoints())}}
}

func (s *MonitorService) docsAPIComponent(checkedAt time.Time) MonitorComponent {
	return MonitorComponent{Key: "docs", Name: "接口文档", Status: monitorStatusAvailable, Severity: severityFromStatus(monitorStatusAvailable), Available: true, Summary: "正常", Detail: "OpenAPI 与在线文档已开放。", CheckedAt: checkedAt, Meta: map[string]any{"paths": []string{"/docs", "/openapi.json"}}}
}

func (s *MonitorService) corsComponent(checkedAt time.Time) MonitorComponent {
	status := monitorStatusAvailable
	summary := "已启用"
	detail := "跨域策略已生效。"
	if !s.cfg.CORS.Enabled {
		status = monitorStatusDegraded
		summary = "未启用"
		detail = "跨域策略未启用，跨域访问将依赖同源部署。"
	}
	return MonitorComponent{Key: "cors", Name: "跨域策略", Status: status, Severity: severityFromStatus(status), Available: status == monitorStatusAvailable, Summary: summary, Detail: detail, CheckedAt: checkedAt, Meta: map[string]any{"allowAllOrigins": s.cfg.CORS.AllowAllOrigins, "allowCredentials": s.cfg.CORS.AllowCredentials, "originCount": len(s.cfg.CORS.AllowOrigins), "methodCount": len(s.cfg.CORS.AllowMethods), "headerCount": len(s.cfg.CORS.AllowHeaders), "exposeHeaderCount": len(s.cfg.CORS.ExposeHeaders), "maxAgeSeconds": int64(s.cfg.CORS.MaxAge / time.Second)}}
}

func (s *MonitorService) firewallComponent(infra map[string]string, checkedAt time.Time) MonitorComponent {
	component := MonitorComponent{Key: "firewall", Name: "防火墙", Status: monitorStatusUnavailable, Severity: severityFromStatus(monitorStatusUnavailable), Available: false, Summary: "不可用", Detail: "防火墙运行时未装配。", CheckedAt: checkedAt, DependsOn: []string{"redis"}}
	if s.firewall == nil {
		return component
	}
	cfg := s.firewall.CurrentConfig()
	reloadVersion, reloadedAt := s.firewall.ReloadMeta()
	component.Meta = map[string]any{"enabled": cfg.Enabled, "corazaEnabled": cfg.CorazaEnabled, "corazaParanoia": cfg.CorazaParanoia, "reloadVersion": reloadVersion, "reloadedAt": reloadedAt, "globalRate": cfg.GlobalRate, "authRate": cfg.AuthRate, "adminRate": cfg.AdminRate}
	if !cfg.Enabled {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Summary = "未启用"
		component.Detail = "防火墙运行时已装配，但当前未启用。"
		return component
	}
	if infra["redis"] == monitorStatusUnavailable {
		component.Detail = "限流依赖 Redis 不可用。"
		return component
	}
	component.Status = monitorStatusAvailable
	component.Severity = severityFromStatus(component.Status)
	component.Available = true
	if cfg.CorazaEnabled {
		component.Summary = "Coraza 已启用"
		component.Detail = "限流与 Coraza 规则引擎均已启用。"
	} else {
		component.Summary = "基础防护已启用"
		component.Detail = "限流与基础拦截已启用，Coraza 当前未启用。"
	}
	return component
}

func (s *MonitorService) systemSettingsComponent(ctx context.Context, infra map[string]string, checkedAt time.Time) MonitorComponent {
	component := MonitorComponent{Key: "platform_settings", Name: "系统设置", Status: monitorStatusUnavailable, Severity: severityFromStatus(monitorStatusUnavailable), Available: false, Summary: "不可用", Detail: "系统设置服务未装配。", CheckedAt: checkedAt, DependsOn: []string{"postgres", "firewall"}}
	if s.system == nil {
		return component
	}
	if infra["postgres"] == monitorStatusUnavailable {
		component.Detail = "系统设置依赖 PostgreSQL。"
		return component
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	view, err := s.system.GetSettings(timeoutCtx)
	if err != nil {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Summary = "读取异常"
		component.Detail = "系统设置服务已装配，但当前无法读取设置。"
		component.Meta = map[string]any{"error": err.Error()}
		return component
	}
	component.Status = monitorStatusAvailable
	component.Severity = severityFromStatus(component.Status)
	component.Available = true
	component.Summary = "正常"
	component.Detail = "系统设置已加载，可进行热重载。"
	component.Meta = map[string]any{"firewallSource": view.Firewall.Source, "reloadVersion": view.Firewall.ReloadVersion, "reloadedAt": view.Firewall.ReloadedAt, "updatedAt": view.Firewall.UpdatedAt}
	return component
}

func (s *MonitorService) listAppBriefs(ctx context.Context, checkedAt time.Time) ([]MonitorAppBrief, error) {
	if s.app == nil {
		return nil, fmt.Errorf("app service unavailable")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	items, err := s.app.ListApps(timeoutCtx)
	if err != nil {
		return nil, err
	}
	summaries := make([]MonitorAppBrief, 0, len(items))
	for _, item := range items {
		components := s.buildAppBriefComponents(&item, checkedAt)
		status, score, availabilityRate, counts := summarizeComponents(components)
		summaries = append(summaries, MonitorAppBrief{ID: item.ID, Name: item.Name, Status: status, Score: score, AvailabilityRate: availabilityRate, Summary: buildAppSummary(&item, counts), CheckedAt: checkedAt, Counts: counts})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })
	return summaries, nil
}

func (s *MonitorService) ListAppBriefs(ctx context.Context) ([]MonitorAppBrief, error) {
	return s.listAppBriefs(ctx, time.Now().UTC())
}

func (s *MonitorService) buildAppBriefComponents(appItem *appdomain.App, checkedAt time.Time) []MonitorComponent {
	if appItem == nil {
		return []MonitorComponent{{Key: "app_core", Name: "应用主体", Status: monitorStatusUnavailable, Severity: severityFromStatus(monitorStatusUnavailable), Available: false, Summary: "应用不存在", Detail: "未查询到对应应用。", CheckedAt: checkedAt}}
	}
	transportPolicy := appdomain.TransportEncryptionPolicy{}
	if s.app != nil {
		transportPolicy = s.app.ResolveTransportEncryption(appItem)
	}
	return []MonitorComponent{
		{Key: "app_core", Name: "应用主体", Status: appAvailabilityStatus(appItem), Severity: severityFromStatus(appAvailabilityStatus(appItem)), Available: appAvailabilityStatus(appItem) == monitorStatusAvailable, Summary: appCoreSummary(appItem), Detail: appCoreDetail(appItem), CheckedAt: checkedAt},
		s.composeAppGateModule("auth_login", "登录入口", checkedAt, s.auth != nil, nil, []string{"postgres", "redis"}, appItem.Status, appItem.LoginStatus, appItem.DisabledReason, appItem.DisabledLoginReason),
		s.composeAppGateModule("registration", "注册入口", checkedAt, s.auth != nil, nil, []string{"postgres", "redis"}, appItem.Status, appItem.RegisterStatus, appItem.DisabledReason, appItem.DisabledRegisterReason),
		s.transportMonitorComponent(appItem, checkedAt, transportPolicy),
	}
}

func (s *MonitorService) buildAppEntrypoints(appItem *appdomain.App, infra map[string]string, checkedAt time.Time, cfg appModuleConfigCounts) []MonitorComponent {
	return []MonitorComponent{
		s.composeAppModule("public_banner", "轮播入口", checkedAt, s.app != nil, []string{infra["postgres"]}, []string{"postgres"}, appItem.Status, appItem.DisabledReason, "公开轮播与横幅内容入口。"),
		s.composeAppModule("public_notice", "公告入口", checkedAt, s.app != nil, []string{infra["postgres"]}, []string{"postgres"}, appItem.Status, appItem.DisabledReason, "公开公告与通知入口。"),
		s.composeAppModule("avatar_entry", "头像入口", checkedAt, s.avatar != nil, []string{moduleStatusFromConfig(cfg.storageEnabled > 0)}, []string{"storage"}, appItem.Status, appItem.DisabledReason, "头像上传、回退与静态访问入口。"),
		s.composeAppModule("websocket_entry", "WebSocket", checkedAt, s.realtime != nil, []string{infra["redis"]}, []string{"redis", "nats"}, appItem.Status, appItem.DisabledReason, "在线状态与实时事件入口。"),
	}
}

func (s *MonitorService) buildAppExtensionModules(appItem *appdomain.App, infra map[string]string, checkedAt time.Time, cfg appModuleConfigCounts) []MonitorComponent {
	return []MonitorComponent{
		s.securityPolicyComponent(appItem, checkedAt, cfg),
		s.transportMonitorComponent(appItem, checkedAt, cfg.transportPolicy),
		s.contentComponent(appItem, checkedAt, cfg),
		s.composeAppModule("site_directory", "站点收录", checkedAt, s.site != nil, []string{infra["postgres"]}, []string{"postgres"}, appItem.Status, appItem.DisabledReason, "用户站点收录、审核与展示。"),
		s.composeAppModule("version_delivery", "版本发布", checkedAt, s.version != nil, []string{infra["postgres"]}, []string{"postgres"}, appItem.Status, appItem.DisabledReason, "版本渠道、发布与升级分发。"),
		s.composeAppModule("role_request", "角色申请", checkedAt, s.roleApp != nil, []string{infra["postgres"]}, []string{"postgres"}, appItem.Status, appItem.DisabledReason, "角色申请、审批与回流。"),
	}
}

func (s *MonitorService) securityPolicyComponent(appItem *appdomain.App, checkedAt time.Time, cfg appModuleConfigCounts) MonitorComponent {
	component := MonitorComponent{Key: "security_policy", Name: "安全策略", Status: monitorStatusUnavailable, Severity: severityFromStatus(monitorStatusUnavailable), Available: false, Summary: "不可用", Detail: "安全策略未装配。", CheckedAt: checkedAt, DependsOn: []string{"postgres"}}
	if appItem == nil {
		component.Summary = "应用不存在"
		component.Detail = "未查询到对应应用。"
		return component
	}
	if !appItem.Status {
		component.Summary = "应用已停用"
		component.Detail = pickReason(appItem.DisabledReason, "应用主体已停用。")
		return component
	}
	if s.app == nil {
		return component
	}
	component.Meta = map[string]any{}
	if cfg.policy != nil {
		component.Meta["policy"] = cfg.policy
	}
	if cfg.passwordPolicy != nil {
		component.Meta["passwordPolicy"] = cfg.passwordPolicy
	}
	if cfg.authSources != nil {
		component.Meta["authSources"] = cfg.authSources
	}
	if cfg.policyLoadError || cfg.passwordLoadError || cfg.authSourcesError {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Summary = "策略读取异常"
		component.Detail = "安全策略已装配，但当前无法完整读取。"
		return component
	}
	component.Status = monitorStatusAvailable
	component.Severity = severityFromStatus(component.Status)
	component.Available = true
	component.Summary = "已加载"
	if cfg.passwordPolicy != nil {
		component.Detail = fmt.Sprintf("密码最小长度 %d，最低强度 %d 分。", cfg.passwordPolicy.Policy.MinLength, cfg.passwordPolicy.Policy.MinScore)
	} else {
		component.Detail = "登录校验、密码策略与认证来源状态已加载。"
	}
	return component
}

func (s *MonitorService) transportMonitorComponent(appItem *appdomain.App, checkedAt time.Time, policy appdomain.TransportEncryptionPolicy) MonitorComponent {
	component := MonitorComponent{Key: "transport_encryption", Name: "传输加密", Status: monitorStatusUnavailable, Severity: severityFromStatus(monitorStatusUnavailable), Available: false, Summary: "不可用", Detail: "传输加密策略未装配。", CheckedAt: checkedAt, DependsOn: []string{"app"}, Meta: map[string]any{"enabled": policy.Enabled, "strict": policy.Strict, "responseEncryption": policy.ResponseEncryption}}
	if appItem == nil {
		component.Summary = "应用不存在"
		component.Detail = "未查询到对应应用。"
		return component
	}
	if !appItem.Status {
		component.Summary = "应用已停用"
		component.Detail = pickReason(appItem.DisabledReason, "应用主体已停用。")
		return component
	}
	if !policy.Enabled {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Summary = "未启用"
		component.Detail = "当前应用未启用请求传输加密。"
		return component
	}
	if strings.TrimSpace(policy.Secret) == "" {
		component.Status = monitorStatusUnavailable
		component.Summary = "配置缺失"
		component.Detail = "已启用传输加密，但缺少有效密钥。"
		return component
	}
	component.Status = monitorStatusAvailable
	component.Severity = severityFromStatus(component.Status)
	component.Available = true
	component.Summary = "已启用"
	component.Detail = "请求传输加密已启用。"
	if policy.ResponseEncryption {
		component.Detail = "请求与响应加密已启用。"
	}
	return component
}

func (s *MonitorService) contentComponent(appItem *appdomain.App, checkedAt time.Time, cfg appModuleConfigCounts) MonitorComponent {
	component := MonitorComponent{Key: "content_delivery", Name: "内容投放", Status: monitorStatusUnavailable, Severity: severityFromStatus(monitorStatusUnavailable), Available: false, Summary: "不可用", Detail: "内容投放状态不可用。", CheckedAt: checkedAt, DependsOn: []string{"postgres"}}
	if appItem == nil {
		component.Summary = "应用不存在"
		component.Detail = "未查询到对应应用。"
		return component
	}
	if !appItem.Status {
		component.Summary = "应用已停用"
		component.Detail = pickReason(appItem.DisabledReason, "应用主体已停用。")
		return component
	}
	if cfg.appStats == nil {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Summary = "统计暂不可用"
		component.Detail = "当前无法读取轮播与公告统计。"
		return component
	}
	component.Meta = map[string]any{"bannerCount": cfg.appStats.BannerCount, "noticeCount": cfg.appStats.NoticeCount}
	if cfg.appStats.BannerCount == 0 && cfg.appStats.NoticeCount == 0 {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Summary = "内容为空"
		component.Detail = "当前应用未配置轮播或公告内容。"
		return component
	}
	component.Status = monitorStatusAvailable
	component.Severity = severityFromStatus(component.Status)
	component.Available = true
	component.Summary = "已配置"
	component.Detail = fmt.Sprintf("轮播 %d 条，公告 %d 条。", cfg.appStats.BannerCount, cfg.appStats.NoticeCount)
	return component
}

func (s *MonitorService) safeAppPolicy(ctx context.Context, appID int64) (*appdomain.Policy, error) {
	if s.app == nil {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	return s.app.GetPolicy(timeoutCtx, appID)
}

func (s *MonitorService) safePasswordPolicy(ctx context.Context, appID int64) (*appdomain.PasswordPolicyView, error) {
	if s.app == nil {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	return s.app.GetPasswordPolicy(timeoutCtx, appID)
}

func (s *MonitorService) safeAppAuthSources(ctx context.Context, appID int64) (*appdomain.AuthSourceStats, error) {
	if s.app == nil {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	return s.app.GetAuthSourceStats(timeoutCtx, appID)
}

func (s *MonitorService) LivenessReport() map[string]any {
	checkedAt := time.Now().UTC()
	return map[string]any{"status": "healthy", "checkedAt": checkedAt, "runtime": s.runtimeSnapshot(checkedAt)}
}

func (s *MonitorService) ReadinessReport(ctx context.Context) (map[string]any, bool) {
	checkedAt := time.Now().UTC()
	infra := s.checkInfrastructure(ctx, checkedAt)
	critical := make([]MonitorComponent, 0, 2)
	ready := true
	for _, item := range infra {
		if item.Key == "postgres" || item.Key == "redis" {
			critical = append(critical, item)
			if item.Status != monitorStatusAvailable {
				ready = false
			}
		}
	}
	if s.auth == nil || s.admin == nil || s.app == nil {
		ready = false
	}
	status := "ready"
	summary := "核心依赖已就绪。"
	if !ready {
		status = "not_ready"
		summary = "核心依赖未就绪。"
	}
	return map[string]any{"status": status, "checkedAt": checkedAt, "summary": summary, "runtime": s.runtimeSnapshot(checkedAt), "components": critical}, ready
}
