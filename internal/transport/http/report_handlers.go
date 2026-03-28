package httptransport

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// parseReportTimeRange 解析报表时间范围参数
func parseReportTimeRange(c *gin.Context) (time.Time, time.Time, bool) {
	var q ReportQueryParams
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少必要的时间参数 start/end")
		return time.Time{}, time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, q.Start)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "start 时间格式错误，需要 RFC3339")
		return time.Time{}, time.Time{}, false
	}
	end, err := time.Parse(time.RFC3339, q.End)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "end 时间格式错误，需要 RFC3339")
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// ReportRegistration 注册趋势报表
func (h *Handler) ReportRegistration(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.RegistrationReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportLogin 登录趋势报表
func (h *Handler) ReportLogin(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.LoginReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportRetention 留存分析报表
func (h *Handler) ReportRetention(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.RetentionReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportActive 活跃用户报表
func (h *Handler) ReportActive(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.ActiveReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportDevice 设备分布报表
func (h *Handler) ReportDevice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.DeviceReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportRegion 地域分布报表
func (h *Handler) ReportRegion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.RegionReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportChannel 渠道来源报表
func (h *Handler) ReportChannel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.ChannelReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportPayment 支付报表
func (h *Handler) ReportPayment(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.PaymentReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportNotification 通知报表
func (h *Handler) ReportNotification(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.NotificationReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportRisk 风控报表
func (h *Handler) ReportRisk(c *gin.Context) {
	// 风控数据为全局维度（firewall_logs 无 appid），仍需管理员身份
	_, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.RiskReport(c.Request.Context(), start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportActivity 抽奖活动报表
func (h *Handler) ReportActivity(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	var q ReportQueryParams
	_ = c.ShouldBindQuery(&q)
	result, err := h.report.ActivityReport(c.Request.Context(), appID, start, end, q.ActivityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportFunnel 用户转化漏斗
func (h *Handler) ReportFunnel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	start, end, ok := parseReportTimeRange(c)
	if !ok {
		return
	}
	result, err := h.report.FunnelReport(c.Request.Context(), appID, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportExport 报表数据导出（CSV）
func (h *Handler) ReportExport(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q ExportQueryParams
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少必要参数 type/start/end")
		return
	}
	start, err := time.Parse(time.RFC3339, q.Start)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "start 时间格式错误")
		return
	}
	end, err := time.Parse(time.RFC3339, q.End)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "end 时间格式错误")
		return
	}

	ctx := c.Request.Context()
	filename := fmt.Sprintf("report_%s_%d.csv", q.Type, appID)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	switch q.Type {
	case "registration":
		data, err := h.report.RegistrationReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"日期", "注册数"})
		for _, p := range data.Series {
			_ = writer.Write([]string{p.Date, strconv.FormatInt(p.Count, 10)})
		}

	case "login":
		data, err := h.report.LoginReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"日期", "成功", "失败"})
		for _, p := range data.Series {
			success := fmt.Sprintf("%v", p.Extra["success"])
			failure := fmt.Sprintf("%v", p.Extra["failure"])
			_ = writer.Write([]string{p.Date, success, failure})
		}

	case "active":
		data, err := h.report.ActiveReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"日期", "DAU"})
		for _, p := range data.DAU {
			_ = writer.Write([]string{p.Date, strconv.FormatInt(p.Count, 10)})
		}

	case "payment":
		data, err := h.report.PaymentReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"日期", "订单数", "金额(分)"})
		for _, p := range data.Series {
			orders := fmt.Sprintf("%v", p.Extra["orders"])
			amount := fmt.Sprintf("%v", p.Extra["amount"])
			_ = writer.Write([]string{p.Date, orders, amount})
		}

	case "notification":
		data, err := h.report.NotificationReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"日期", "发送数"})
		for _, p := range data.Series {
			_ = writer.Write([]string{p.Date, strconv.FormatInt(p.Count, 10)})
		}

	case "risk":
		data, err := h.report.RiskReport(ctx, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"日期", "拦截数"})
		for _, p := range data.Series {
			_ = writer.Write([]string{p.Date, strconv.FormatInt(p.Count, 10)})
		}

	case "funnel":
		data, err := h.report.FunnelReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"步骤", "人数", "转化率(%)"})
		for _, s := range data.Steps {
			_ = writer.Write([]string{s.Step, strconv.FormatInt(s.Count, 10), fmt.Sprintf("%.2f", s.Rate)})
		}

	case "channel":
		data, err := h.report.ChannelReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"渠道", "数量", "占比(%)"})
		for _, p := range data.Channels {
			_ = writer.Write([]string{p.Label, strconv.FormatInt(p.Count, 10), fmt.Sprintf("%.2f", p.Percentage)})
		}

	case "region":
		data, err := h.report.RegionReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"IP", "数量", "占比(%)"})
		for _, p := range data.TopIPs {
			_ = writer.Write([]string{p.Label, strconv.FormatInt(p.Count, 10), fmt.Sprintf("%.2f", p.Percentage)})
		}

	case "device":
		data, err := h.report.DeviceReport(ctx, appID, start, end)
		if err != nil {
			h.writeError(c, err)
			return
		}
		_ = writer.Write([]string{"类型", "名称", "数量", "占比(%)"})
		for _, p := range data.OS {
			_ = writer.Write([]string{"OS", p.Label, strconv.FormatInt(p.Count, 10), fmt.Sprintf("%.2f", p.Percentage)})
		}
		for _, p := range data.Browser {
			_ = writer.Write([]string{"Browser", p.Label, strconv.FormatInt(p.Count, 10), fmt.Sprintf("%.2f", p.Percentage)})
		}
		for _, p := range data.Platform {
			_ = writer.Write([]string{"Platform", p.Label, strconv.FormatInt(p.Count, 10), fmt.Sprintf("%.2f", p.Percentage)})
		}

	default:
		response.Error(c, http.StatusBadRequest, 40000, "不支持的报表类型: "+q.Type)
	}
}
