package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	walletdomain "aegis/internal/domain/wallet"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 钱包流水凭证与管理端交易查询。
//
// 用户端三条与订单凭证同构（直接取 PDF / 导出可分享凭据 / 寄到绑定邮箱），
// 路径只是把「订单号」换成「流水号」。刻意保持同构是为了让客户端能用同一段
// 代码渲染两种资金记录的凭证入口。

// ── DTO ──

// AdminWalletTransactionsQuery 管理端全应用流水筛选。
type AdminWalletTransactionsQuery struct {
	UserID    int64  `form:"userId" json:"userId"`
	Type      string `form:"type" json:"type"`
	Direction string `form:"direction" json:"direction"`
	Keyword   string `form:"keyword" json:"keyword"`
	// Start / End RFC3339 时间窗
	Start string `form:"start" json:"start"`
	End   string `form:"end" json:"end"`
	Page  int    `form:"page" json:"page"`
	Limit int    `form:"limit" json:"limit"`
}

// AdminWalletStatsQuery 资金面板的时间窗
type AdminWalletStatsQuery struct {
	Start string `form:"start" json:"start"`
	End   string `form:"end" json:"end"`
}

// AdminWalletReceiptRequest 管理端代开钱包流水凭证。
type AdminWalletReceiptRequest struct {
	TransactionNo string `json:"transactionNo" form:"transactionNo" binding:"required"`
	PaymentBillExportRequest
}

// AdminWalletReceiptEmailRequest 管理端把钱包流水凭证寄到指定邮箱。
type AdminWalletReceiptEmailRequest struct {
	TransactionNo string `json:"transactionNo" binding:"required"`
	To            string `json:"to" binding:"required"`
	PaymentBillExportRequest
}

// ── 用户端 ──

// MyWalletTransactionDetail GET /api/wallet/transactions/:transactionNo
func (h *Handler) MyWalletTransactionDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	view, err := h.wallet.GetMyTransactionView(c.Request.Context(), session,
		c.Param("transactionNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", view)
}

// DownloadWalletReceipt GET /api/wallet/transactions/:transactionNo/receipt
//
// 直接返回 PDF，省掉「先创建再下载」两步。挂着关联订单的流水会拿到
// **订单那份**凭证 —— 同一笔钱只有一份凭证，编号也只有一个。
func (h *Handler) DownloadWalletReceipt(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	data, filename, err := h.payment.RenderUserWalletReceipt(c.Request.Context(), session,
		c.Param("transactionNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writePDF(c, filename, data)
}

// ExportWalletBill POST|GET /api/wallet/transactions/:transactionNo/bill
// 生成凭证并返回一次性下载凭据（可分享的链接）。
func (h *Handler) ExportWalletBill(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	export, err := h.payment.CreateUserWalletReceipt(c.Request.Context(), session,
		c.Param("transactionNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", export)
}

// EmailWalletReceipt POST /api/wallet/transactions/:transactionNo/receipt/email
// 收件地址恒为账号绑定邮箱，不接受请求指定（理由见 EmailPaymentReceipt）。
func (h *Handler) EmailWalletReceipt(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	result, err := h.payment.EmailUserWalletReceipt(c.Request.Context(), session,
		c.Param("transactionNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

// ── 管理端 ──

// AdminAppWalletTransactions GET /api/admin/apps/:appkey/wallet/transactions
//
// 按**应用**而不是按用户查。此前只有 /users/:userId/wallet/transactions，
// 于是「这个应用今天的资金往来」只能一个用户一个用户点过去。
func (h *Handler) AdminAppWalletTransactions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminWalletTransactionsQuery
	_ = c.ShouldBindQuery(&query)
	result, err := h.wallet.AdminListAppTransactions(c.Request.Context(), appID, walletdomain.AdminListQuery{
		UserID:    query.UserID,
		Type:      strings.TrimSpace(query.Type),
		Direction: strings.TrimSpace(query.Direction),
		Keyword:   strings.TrimSpace(query.Keyword),
		Start:     parseTimeWindow(query.Start),
		End:       parseTimeWindow(query.End),
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取应用钱包流水成功", result)
}

// AdminAppWalletStats GET /api/admin/apps/:appkey/wallet/stats
func (h *Handler) AdminAppWalletStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminWalletStatsQuery
	_ = c.ShouldBindQuery(&query)
	stats, err := h.wallet.AdminStats(c.Request.Context(), appID,
		parseTimeWindow(query.Start), parseTimeWindow(query.End))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取资金统计成功", stats)
}

// AdminAppWalletReceipt POST /api/admin/apps/:appkey/wallet/receipt
// 管理端代开钱包流水凭证（客服 / 对账留档），**不落盘** ——
// 一次性下载没有分享场景，落盘只会多留一份资金明细在磁盘上。
func (h *Handler) AdminAppWalletReceipt(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminWalletReceiptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, filename, err := h.payment.RenderAppWalletReceipt(c.Request.Context(), appID,
		req.TransactionNo, receiptOptions(c, req.PaymentBillExportRequest))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writePDF(c, filename, data)
}

// AdminAppWalletReceiptEmail POST /api/admin/apps/:appkey/wallet/receipt/email
// 收件地址由操作者指定，因此这条路径走管理端鉴权与审计中间件。
func (h *Handler) AdminAppWalletReceiptEmail(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminWalletReceiptEmailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.payment.EmailAppWalletReceipt(c.Request.Context(), appID,
		req.TransactionNo, req.To, receiptOptions(c, req.PaymentBillExportRequest))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

// AdminAppCommerceOverview GET /api/admin/apps/:appkey/commerce/overview
//
// 交易中心首屏的一次性取数：订单、退款、钱包三口径 + 凭证能力自述。
// 合成一个接口而不是让前端并发拉四个：这四段数据在页面上是**一起**呈现的，
// 分开拉会出现「订单已刷新、退款还是上一次的时间窗」这种自相矛盾的画面。
func (h *Handler) AdminAppCommerceOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminWalletStatsQuery
	_ = c.ShouldBindQuery(&query)
	start, end := parseTimeWindow(query.Start), parseTimeWindow(query.End)

	overview := gin.H{}
	if stats, err := h.payment.AdminOrderStats(c.Request.Context(), appID, start, end); err == nil {
		overview["orders"] = stats
	} else {
		h.writeError(c, err)
		return
	}
	if stats, err := h.wallet.AdminStats(c.Request.Context(), appID, start, end); err == nil {
		overview["wallet"] = stats
	} else {
		h.writeError(c, err)
		return
	}
	if trend, err := h.payment.AdminTransactionTrend(c.Request.Context(), appID, start, end); err == nil {
		overview["trend"] = trend
	} else {
		h.writeError(c, err)
		return
	}
	overview["receipt"] = h.payment.ReceiptCapability()
	response.Success(c, 200, "获取交易概览成功", overview)
}

// parseTimeWindow 解析时间窗端点。
//
// 解析失败一律当「不限」而不是报错：时间窗是筛选条件，
// 为一个格式不对的查询参数返回 400，会让面板在用户还没选完日期时就红一片。
func parseTimeWindow(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	// 也接受 Unix 秒：图表控件传时间戳比传字符串更常见
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		parsed := time.Unix(seconds, 0).UTC()
		return &parsed
	}
	return nil
}
