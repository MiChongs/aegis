package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	systemdomain "aegis/internal/domain/system"
	auditmiddleware "aegis/internal/middleware"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// IP 封禁批量管理（docs/firewall.md「封禁」）。全部仅超级管理员。

func (h *Handler) requireBanAdmin(c *gin.Context) (int64, bool) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return 0, false
	}
	return session.AdminID, true
}

// AdminIPBanStats 封禁概况
// GET /api/admin/system/firewall/bans/stats
func (h *Handler) AdminIPBanStats(c *gin.Context) {
	if _, ok := h.requireBanAdmin(c); !ok {
		return
	}
	stats, err := h.ipBan.Stats(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

// AdminCountIPBans 按筛选计数（批量解封前确认）
// GET /api/admin/system/firewall/bans/count
func (h *Handler) AdminCountIPBans(c *gin.Context) {
	if _, ok := h.requireBanAdmin(c); !ok {
		return
	}
	var req IPBanListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	total, err := h.ipBan.CountBans(c.Request.Context(), req.filter())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"total": total})
}

// AdminInspectIP 某个 IP 现在会不会被拦、被哪一层拦，封禁历史与近期拦截次数
// GET /api/admin/system/firewall/bans/inspect?ip=
func (h *Handler) AdminInspectIP(c *gin.Context) {
	if _, ok := h.requireBanAdmin(c); !ok {
		return
	}
	result, err := h.ipBan.Inspect(c.Request.Context(), c.Query("ip"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminBatchBanIPs 批量封禁
// POST /api/admin/system/firewall/bans/batch
func (h *Handler) AdminBatchBanIPs(c *gin.Context) {
	adminID, ok := h.requireBanAdmin(c)
	if !ok {
		return
	}
	var req IPBanBatchCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.ip_ban", "")
	result, err := h.ipBan.BanIPs(c.Request.Context(), service.BanIPsInput{
		IPs: req.IPs, Reason: req.Reason, Duration: req.Duration, Mode: req.Mode, DelayMs: req.DelayMs,
	}, adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("批量封禁 IP：新增 %d 个，已在封禁 %d 个，失败 %d 个", result.Succeeded, result.Skipped, result.Failed))
	auditmiddleware.SetAuditDetail(c, fmt.Sprintf("reason=%q mode=%s duration=%ds ips=%s", req.Reason, req.Mode, req.Duration, summarizeIPs(req.IPs)))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "批量封禁完成", result)
}

// AdminBatchUnbanIPs 批量解封（选中的 ids，或按筛选解封全部匹配的生效中封禁）
// POST /api/admin/system/firewall/bans/unban
func (h *Handler) AdminBatchUnbanIPs(c *gin.Context) {
	adminID, ok := h.requireBanAdmin(c)
	if !ok {
		return
	}
	var req IPBanBatchUnbanRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.ip_ban", "")
	var err error
	if len(req.IDs) > 0 {
		out, e := h.ipBan.UnbanByIDs(c.Request.Context(), req.IDs, adminID, req.Note)
		err = e
		if e == nil {
			auditmiddleware.SetAuditDetail(c, fmt.Sprintf("note=%q ids=%v", req.Note, truncateIDs(req.IDs)))
			auditmiddleware.SetAuditSummary(c, fmt.Sprintf("批量解封 IP：选中 %d 条，解封 %d 条", len(req.IDs), out.Succeeded))
			auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
			response.Success(c, 200, "解封完成", out)
			return
		}
	} else {
		if req.Filter == nil {
			response.Error(c, http.StatusBadRequest, 40000, "需要 ids 或 filter")
			return
		}
		out, e := h.ipBan.UnbanByFilter(c.Request.Context(), req.Filter.filter(), req.Expected, adminID, req.Note)
		err = e
		if e == nil {
			f := req.Filter
			auditmiddleware.SetAuditDetail(c, fmt.Sprintf("note=%q source=%s country=%s mode=%s keyword=%q expiringWithin=%d permanent=%s",
				req.Note, f.Source, f.Country, f.Mode, f.Keyword, f.ExpiringWithin, f.Permanent))
			auditmiddleware.SetAuditSummary(c, fmt.Sprintf("按条件批量解封 IP：匹配 %d 条，解封 %d 条", out.Total, out.Succeeded))
			auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityCritical)
			response.Success(c, 200, "解封完成", out)
			return
		}
	}
	auditmiddleware.SetAuditFailure(c, err)
	h.writeError(c, err)
}

// AdminUpdateIPBan 修改一条生效中的封禁
// PATCH /api/admin/system/firewall/bans/:banId
func (h *Handler) AdminUpdateIPBan(c *gin.Context) {
	adminID, ok := h.requireBanAdmin(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("banId"), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的封禁记录 ID")
		return
	}
	var req IPBanUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.ip_ban", strconv.FormatInt(id, 10))
	before, _ := h.ipBan.GetBan(c.Request.Context(), id)
	ban, err := h.ipBan.UpdateBan(c.Request.Context(), id, req.patch(), adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("修改 IP 封禁 %s", ban.IP))
	auditmiddleware.SetAuditDiff(c, before, ban)
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已修改", ban)
}

// AdminBatchUpdateIPBans 批量修改响应模式 / 时长
// POST /api/admin/system/firewall/bans/update
func (h *Handler) AdminBatchUpdateIPBans(c *gin.Context) {
	adminID, ok := h.requireBanAdmin(c)
	if !ok {
		return
	}
	var req IPBanUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.Mode == nil && req.Reason == nil && req.Duration == nil && req.DelayMs == nil {
		response.Error(c, http.StatusBadRequest, 40000, "没有要修改的字段")
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.ip_ban", "")
	result, err := h.ipBan.UpdateBans(c.Request.Context(), req.IDs, req.patch(), adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("批量修改 IP 封禁：成功 %d 条，失败 %d 条", result.Succeeded, result.Failed))
	auditmiddleware.SetAuditDetail(c, fmt.Sprintf("ids=%v", truncateIDs(req.IDs)))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已修改", result)
}

// AdminBatchGeoBans 地域规则批量启用 / 停用 / 删除
// POST /api/admin/system/firewall/geo-bans/batch
func (h *Handler) AdminBatchGeoBans(c *gin.Context) {
	if _, ok := h.requireBanAdmin(c); !ok {
		return
	}
	if h.geoBan == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地域封禁服务未就绪")
		return
	}
	var req GeoBanBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 500 {
		response.Error(c, http.StatusBadRequest, 40000, "需要选中 1-500 条规则")
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_ban", "")
	done, err := h.geoBan.BatchApply(c.Request.Context(), req.IDs, req.Action)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	verb := map[string]string{"enable": "启用", "disable": "停用", "delete": "删除"}[req.Action]
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("批量%s地域封禁规则 %d 条", verb, done))
	auditmiddleware.SetAuditDetail(c, fmt.Sprintf("ids=%v", truncateIDs(req.IDs)))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已完成", gin.H{"processed": done, "total": len(req.IDs)})
}

func summarizeIPs(ips []string) string {
	if len(ips) <= 20 {
		return strings.Join(ips, ",")
	}
	return strings.Join(ips[:20], ",") + fmt.Sprintf(" 等 %d 个", len(ips))
}

func truncateIDs(ids []int64) []int64 {
	if len(ids) > 50 {
		return ids[:50]
	}
	return ids
}
