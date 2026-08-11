package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	systemdomain "aegis/internal/domain/system"
	auditmiddleware "aegis/internal/middleware"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AdminListIPBans 查询 IP 封禁列表
// GET /api/admin/system/firewall/bans
func (h *Handler) AdminListIPBans(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	var req IPBanListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	filter := firewalldomain.IPBanFilter{
		IP:       req.IP,
		Status:   req.Status,
		Source:   req.Source,
		Page:     req.Page,
		PageSize: req.PageSize,
	}
	page, err := h.ipBan.ListBans(c.Request.Context(), filter)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", page)
}

// AdminBanIP 手动封禁 IP
// POST /api/admin/system/firewall/bans
func (h *Handler) AdminBanIP(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	var req IPBanCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	ban, err := h.ipBan.BanIPWithOptions(c.Request.Context(), req.IP, req.Reason, req.Duration, session.AdminID, service.BanIPOptions{
		Mode:    req.Mode,
		DelayMs: req.DelayMs,
	})
	if err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.ip_ban", req.IP)
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("封禁 IP %s 失败", req.IP))
		auditmiddleware.SetAuditDetail(c, fmt.Sprintf("ip=%s reason=%q mode=%s duration=%ds delay_ms=%d", req.IP, req.Reason, req.Mode, req.Duration, req.DelayMs))
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.ip_ban", strconv.FormatInt(ban.ID, 10))
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("封禁 IP %s", ban.IP))
	auditmiddleware.SetAuditDetail(c, fmt.Sprintf("ip=%s reason=%q mode=%s duration=%ds delay_ms=%d country=%s isp=%s", ban.IP, ban.Reason, firewallResolveMode(ban.Mode, h.ipBan.DefaultMode()), ban.Duration, req.DelayMs, ban.Country, ban.ISP))
	auditmiddleware.SetAuditDiff(c, nil, ban)
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "封禁成功", ban)
}

// firewallResolveMode 把空模式回落为默认模式字面量，便于审计日志展示
func firewallResolveMode(mode, fallback string) string {
	if mode == "" {
		return fallback + " (默认)"
	}
	return mode
}

// AdminListIPBanModes 返回可用的封禁响应模式（供前端下拉渲染）
// GET /api/admin/system/firewall/bans/modes
func (h *Handler) AdminListIPBanModes(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	defaults := "forbidden"
	if h.ipBan != nil {
		defaults = h.ipBan.DefaultMode()
	}
	modes := []IPBanModeEntry{
		{Value: firewalldomain.BanModeForbidden, Label: "拦截 (HTTP 403)", Description: "返回 HTTP 403 + 错误信封，对合法运维最友好"},
		{Value: firewalldomain.BanModeSilentDrop, Label: "完全不响应", Description: "劫持 TCP 连接直接关闭，客户端感知为超时"},
		{Value: firewalldomain.BanModeConnReset, Label: "连接重置", Description: "同「完全不响应」，语义上更强"},
		{Value: firewalldomain.BanModeTarpit, Label: "拖延响应 (Tarpit)", Description: "等待 N 秒后再返回 403，拖慢攻击者"},
		{Value: firewalldomain.BanModeStealth404, Label: "伪装 404", Description: "让扫描器以为资源不存在"},
		{Value: firewalldomain.BanModeStealth503, Label: "伪装 503", Description: "让攻击者以为服务暂时不可用"},
		{Value: firewalldomain.BanModeTeapot, Label: "418 彩蛋", Description: "返回 HTTP 418 I'm a teapot，明示拦截"},
		{Value: firewalldomain.BanModeGone, Label: "410 已永久移除", Description: "语义「资源永久消失」，让爬虫从索引删除"},
		{Value: firewalldomain.BanModeRateChoke, Label: "严格限速", Description: "延迟 2s 后返回 429，Retry-After 5 分钟"},
		{Value: firewalldomain.BanModeRedirect, Label: "302 重定向", Description: "重定向到蜜罐或首页，URL 可在防火墙配置中修改"},
		{Value: firewalldomain.BanModeHoneypot, Label: "蜜罐（伪成功）", Description: "返回 200 + 伪造 JSON 响应，诱导攻击者误判"},
		{Value: firewalldomain.BanModeFakeEmpty, Label: "200 空响应", Description: "返回 200 + 空 body，让爬虫以为是空资源"},
		{Value: firewalldomain.BanModeRandomError, Label: "随机 5xx", Description: "随机返回 500/502/503/504，扰乱扫描指纹"},
		{Value: firewalldomain.BanModeSlowResponse, Label: "逐字节慢响应", Description: "每 500ms 输出 1 字节，拖住攻击者连接数小时"},
		{Value: firewalldomain.BanModeBandwidthChoke, Label: "带宽抑制", Description: "以 1 字节/100ms 输出 403 响应体"},
		{Value: firewalldomain.BanModeRandomDelay, Label: "随机延迟", Description: "随机 1-15 秒延迟后返回 403，破坏时序分析"},

		// ── 增强恶心模式 ──
		{Value: firewalldomain.BanModeZipBomb, Label: "🧨 Gzip 压缩炸弹", Description: "~10KB 的响应解压为 10MB 零，撑爆不设防的扫描器"},
		{Value: firewalldomain.BanModeChunkedInfinite, Label: "♾️ 无限 chunked 流", Description: "每秒 1KB 持续输出，拖满攻击者 I/O 线程直到超时"},
		{Value: firewalldomain.BanModeInfiniteRedirect, Label: "🔁 无限重定向", Description: "每次 302 到随机 /_trap/... 子路径，攻击者陷入循环跳转"},
		{Value: firewalldomain.BanModeMirrorRequest, Label: "🪞 请求回显", Description: "把攻击者请求原样回显，扫描器误判为 XSS/SSRF 反射点浪费时间"},
		{Value: firewalldomain.BanModeFakeLogin, Label: "🎣 伪装登录页", Description: "返回以假乱真的管理员登录表单，诱导爬虫/爆破器投喂凭据"},
		{Value: firewalldomain.BanModeRandomGarbage, Label: "🗑️ 随机垃圾数据", Description: "返回 512KB 完全随机二进制 + 伪造 Content-Type"},
		{Value: firewalldomain.BanModeCursedHeaders, Label: "🧿 诅咒响应头", Description: "返回 50+ 矛盾/伪造响应头，彻底破坏指纹识别"},
		{Value: firewalldomain.BanModeJSONBomb, Label: "💣 JSON 武器级炸弹", Description: "gzip 压缩传输 → 解压后 500MB+：50 万层嵌套 + 20 万 unicode-escape 键 + 百万次 \\uXXXX 转义 + 10 万位 BigInt。Python/Ruby/Node/PHP parser 直接 OOM"},
		{Value: firewalldomain.BanModeCookieBomb, Label: "🍪 Cookie 膨胀", Description: "下发 80+ 大 Cookie，让攻击者后续每请求携带 ~100KB 头"},
		{Value: firewalldomain.BanModeReverseSlowloris, Label: "🐢 反向 Slowloris", Description: "劫持连接后按字节 1 秒滴一个慢吐响应头，拖住连接 5 分钟"},
	}
	response.Success(c, 200, "ok", IPBanModesResponse{Default: defaults, Modes: modes})
}

// AdminUnbanIP 手动解封 IP
// DELETE /api/admin/system/firewall/bans/:banId
func (h *Handler) AdminUnbanIP(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	banID, err := strconv.ParseInt(c.Param("banId"), 10, 64)
	if err != nil || banID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的封禁记录 ID")
		return
	}
	// 解封前先读取原始记录，用于审计日志 diff
	before, _ := h.ipBan.GetBan(c.Request.Context(), banID)
	if err := h.ipBan.UnbanIP(c.Request.Context(), banID, session.AdminID); err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.ip_ban", strconv.FormatInt(banID, 10))
		if before != nil {
			auditmiddleware.SetAuditSummary(c, fmt.Sprintf("解封 IP %s 失败", before.IP))
		} else {
			auditmiddleware.SetAuditSummary(c, fmt.Sprintf("解封 IP 记录 #%d 失败", banID))
		}
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	if before != nil {
		auditmiddleware.SetAuditResource(c, "firewall.ip_ban", strconv.FormatInt(banID, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("解封 IP %s", before.IP))
		auditmiddleware.SetAuditDetail(c, fmt.Sprintf("ip=%s reason=%q source=%s country=%s", before.IP, before.Reason, before.Source, before.Country))
		auditmiddleware.SetAuditDiff(c, before, nil)
	} else {
		auditmiddleware.SetAuditResource(c, "firewall.ip_ban", strconv.FormatInt(banID, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("解封 IP 记录 #%d", banID))
	}
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "解封成功", map[string]any{
		"banId":     banID,
		"revokedAt": time.Now().UTC(),
	})
}
