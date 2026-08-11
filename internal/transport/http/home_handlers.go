package httptransport

import (
	"net/http"
	"strings"

	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// Homepage 渲染服务默认首页（Claude / Anthropic 风格）。
func (h *Handler) Homepage(c *gin.Context) {
	if wantsJSONStrict(c) {
		response.OK(c, gin.H{
			"name":        "Aegis",
			"description": "Multi-tenant identity platform",
			"version":     resolveVersion(h),
			"endpoints": gin.H{
				"health":  "/healthz",
				"ready":   "/readyz",
				"status":  "/status",
				"console": "/console",
				"docs":    "/docs",
				"login":   "/login",
				"api":     "/api",
			},
		})
		return
	}
	response.RenderHomepage(c, response.HomePageData{
		Version: resolveVersion(h),
	})
}

// StatusPage 渲染服务完整状态页（/status）。
//
// 优先使用 MonitorService 返回的实时数据；失败则退化为静态"服务不可观测"页。
// JSON 客户端会得到与 /api/system/monitor 对齐的结构化响应。
func (h *Handler) StatusPage(c *gin.Context) {
	if h.monitor == nil {
		// 监控服务未注入：也应该给出一个状态页
		if wantsJSONStrict(c) {
			response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
			return
		}
		response.RenderStatusPage(c, response.StatusPageData{
			Status:           "unavailable",
			Score:            0,
			AvailabilityRate: 0,
			Summary:          "监控服务未就绪，无法采集实时状态。",
			Counts:           response.StatusCounts{},
		})
		return
	}

	overview, err := h.monitor.SystemOverview(c.Request.Context())
	if err != nil {
		if wantsJSONStrict(c) {
			response.Internal(c, "系统状态采集失败")
			return
		}
		response.RenderStatusPage(c, response.StatusPageData{
			Status:  "degraded",
			Score:   0,
			Summary: "采集实时状态时发生错误。",
		})
		return
	}

	if wantsJSONStrict(c) {
		response.OK(c, gin.H{
			"status":           overview.Status,
			"score":            overview.Score,
			"availabilityRate": overview.AvailabilityRate,
			"summary":          overview.Summary,
			"checkedAt":        overview.CheckedAt,
		})
		return
	}

	response.RenderStatusPage(c, response.StatusPageData{
		Status:           overview.Status,
		Score:            overview.Score,
		AvailabilityRate: overview.AvailabilityRate,
		Summary:          overview.Summary,
		CheckedAt:        overview.CheckedAt,
	})
}

// EnvPage 渲染客户端环境查看页（/cons-env）。
//
// 服务端侧已知信息（IP、TLS、请求头、GeoIP 若可用）直接渲染；浏览器/设备/屏幕/
// 硬件/功能支持等字段由页面内联 JS 通过 navigator / screen API 填充。
func (h *Handler) EnvPage(c *gin.Context) {
	data := response.ExtractEnvPageData(c)

	// 若启用 LocationService 则补齐 GeoIP 字段
	if h.location != nil && data.ClientIP != "" {
		loc := h.location.Resolve(c.Request.Context(), data.ClientIP)
		data.Country = loc.Country
		data.Region = loc.Region
		data.City = loc.City
		data.Timezone = loc.Timezone
		data.ISP = loc.ISP
		if loc.Network.ASN != "" {
			data.ASN = loc.Network.ASN
			if data.ISP == "" {
				data.ISP = loc.Network.Organization
			}
		}
	}

	if wantsJSONStrict(c) {
		response.OK(c, gin.H{
			"clientIp":       data.ClientIP,
			"userAgent":      data.UserAgent,
			"method":         data.Method,
			"host":           data.Host,
			"protocol":       data.Protocol,
			"tlsVersion":     data.TLSVersion,
			"tlsCipher":      data.TLSCipher,
			"referer":        data.Referer,
			"forwardedFor":   data.ForwardedFor,
			"realIp":         data.RealIP,
			"acceptLang":     data.AcceptLang,
			"accept":         data.Accept,
			"acceptEncoding": data.AcceptEncoding,
			"country":        data.Country,
			"region":         data.Region,
			"city":           data.City,
			"asn":            data.ASN,
			"isp":            data.ISP,
			"timezone":       data.Timezone,
			"headers":        data.Headers,
			"checkedAt":      data.CheckedAt,
		})
		return
	}

	response.RenderEnvPage(c, data)
}

// AnnouncementsPage 渲染系统公告页（/announcements）。
//
// 数据来自 AnnouncementService.ListActive —— 只展示对公众生效的已发布公告。
// JSON 客户端会得到与 /api/system/announcements/active 对齐的结构化响应。
func (h *Handler) AnnouncementsPage(c *gin.Context) {
	if h.announcement == nil {
		if wantsJSONStrict(c) {
			response.Error(c, 503, 50310, "公告服务暂不可用")
			return
		}
		response.RenderAnnouncementsPage(c, response.AnnouncementsPageData{})
		return
	}

	items, err := h.announcement.ListActive(c.Request.Context())
	if err != nil {
		if wantsJSONStrict(c) {
			response.Internal(c, "加载公告失败: "+err.Error())
			return
		}
		// 页面模式：失败也渲染空态页，避免用户看到 500 技术细节
		response.RenderAnnouncementsPage(c, response.AnnouncementsPageData{Summary: "公告暂时无法加载"})
		return
	}

	if wantsJSONStrict(c) {
		response.OK(c, items)
		return
	}

	out := make([]response.AnnouncementItem, 0, len(items))
	for _, it := range items {
		out = append(out, response.AnnouncementItem{
			ID:          it.ID,
			Type:        it.Type,
			Title:       it.Title,
			Content:     it.Content,
			Level:       it.Level,
			Pinned:      it.Pinned,
			AdminName:   it.AdminName,
			PublishedAt: it.PublishedAt,
			ExpiresAt:   it.ExpiresAt,
			CreatedAt:   it.CreatedAt,
			UpdatedAt:   it.UpdatedAt,
		})
	}
	response.RenderAnnouncementsPage(c, response.AnnouncementsPageData{Items: out})
}

// adaptMonitorOverview 把 service.MonitorOverview 映射到 response.StatusPageData。
func adaptMonitorOverview(o *service.MonitorOverview) response.StatusPageData {
	data := response.StatusPageData{
		Status:           o.Status,
		Score:            o.Score,
		AvailabilityRate: o.AvailabilityRate,
		Summary:          o.Summary,
		CheckedAt:        o.CheckedAt,
		Counts: response.StatusCounts{
			Total:       o.Counts.Total,
			Available:   o.Counts.Available,
			Degraded:    o.Counts.Degraded,
			Unavailable: o.Counts.Unavailable,
		},
		Highlights:     o.Highlights,
		Infrastructure: adaptComponents(o.Infrastructure),
		Modules:        adaptComponents(o.Modules),
		Runtime: response.StatusRuntime{
			AppName:       o.Runtime.AppName,
			Environment:   o.Runtime.Environment,
			CheckedAt:     o.Runtime.CheckedAt,
			StartedAt:     o.Runtime.StartedAt,
			UptimeSeconds: o.Runtime.UptimeSeconds,
			Timezone:      o.Runtime.Timezone,
			GoVersion:     o.Runtime.GoVersion,
			GoOS:          o.Runtime.GoOS,
			GoArch:        o.Runtime.GoArch,
			Goroutines:    o.Runtime.Goroutines,
			Hostname:      o.Runtime.Hostname,
			OS:            o.Runtime.OS,
			Platform:      o.Runtime.Platform,
			KernelArch:    o.Runtime.KernelArch,
			CPUModel:      o.Runtime.CPUModel,
			CPUCores:      o.Runtime.CPUCores,
			CPUThreads:    o.Runtime.CPUThreads,
			CPUUsage:      o.Runtime.CPUUsage,
			MemTotal:      o.Runtime.MemTotal,
			MemUsed:       o.Runtime.MemUsed,
			MemUsedPct:    o.Runtime.MemUsedPct,
			DiskTotal:     o.Runtime.DiskTotal,
			DiskUsed:      o.Runtime.DiskUsed,
			DiskUsedPct:   o.Runtime.DiskUsedPct,
			PID:           o.Runtime.PID,
			ProcessMem:    o.Runtime.ProcessMem,
			MemAlloc:      o.Runtime.MemAlloc,
			MemSys:        o.Runtime.MemSys,
			NumGC:         o.Runtime.NumGC,
		},
	}
	return data
}

func adaptComponents(cs []service.MonitorComponent) []response.StatusComponent {
	out := make([]response.StatusComponent, 0, len(cs))
	for _, c := range cs {
		out = append(out, response.StatusComponent{
			Key:       c.Key,
			Name:      c.Name,
			Status:    c.Status,
			Severity:  c.Severity,
			Available: c.Available,
			Summary:   c.Summary,
			Detail:    c.Detail,
			CheckedAt: c.CheckedAt,
			DependsOn: c.DependsOn,
		})
	}
	return out
}

// resolveVersion 尝试从 Handler 取版本；当前 versionService 不暴露"当前平台版本"，返回空 → 模板降级为 "dev"。
func resolveVersion(h *Handler) string {
	_ = h
	return ""
}

// wantsJSONStrict 严格判定：只有明确声明 application/json 的客户端才返回 JSON。
// 浏览器默认不会进到这个分支。
func wantsJSONStrict(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if strings.EqualFold(c.Query("format"), "json") {
		return true
	}
	accept := c.GetHeader("Accept")
	if accept == "" {
		return false
	}
	// 只有显式 JSON 且不含 html 时，才走 JSON
	lower := strings.ToLower(accept)
	return strings.Contains(lower, "application/json") && !strings.Contains(lower, "text/html")
}
