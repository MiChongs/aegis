package httptransport

import (
	emaildomain "aegis/internal/domain/email"
	paymentdomain "aegis/internal/domain/payment"
	workflowdomain "aegis/internal/domain/workflow"
	"aegis/pkg/response"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminEmailConfigList(c *gin.Context) {
	var req AdminEmailConfigListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.email.ListConfigs(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminEmailConfigDetail(c *gin.Context) {
	var req AdminEmailConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.email.Detail(c.Request.Context(), req.AppID, req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminEmailConfigCreate(c *gin.Context) { h.adminEmailConfigSave(c, 0) }

func (h *Handler) AdminEmailConfigUpdate(c *gin.Context) {
	var req AdminEmailConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminEmailConfigSaveWithReq(c, req, req.ConfigID)
}

func (h *Handler) adminEmailConfigSave(c *gin.Context, id int64) {
	var req AdminEmailConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminEmailConfigSaveWithReq(c, req, id)
}

func (h *Handler) adminEmailConfigSaveWithReq(c *gin.Context, req AdminEmailConfigSaveRequest, id int64) {
	smtp := req.SMTP
	if smtp == nil {
		smtp = &emaildomain.SMTPConfig{
			Host:               req.SMTPHost,
			Port:               req.SMTPPort,
			Username:           req.SMTPUser,
			Password:           req.SMTPPassword,
			FromAddress:        req.SMTPFrom,
			FromName:           req.SMTPFromName,
			ReplyTo:            req.SMTPReplyTo,
			MaxConnections:     5,
			MaxMessagesPerConn: 100,
		}
		if req.SMTPTLS != nil {
			smtp.UseTLS = *req.SMTPTLS
		}
		if req.SMTPInsecure != nil {
			smtp.InsecureSkipVerify = *req.SMTPInsecure
		}
	}
	item, err := h.email.Save(c.Request.Context(), emaildomain.ConfigMutation{
		ID:          id,
		AppID:       req.AppID,
		Name:        maybeString(req.Name),
		Provider:    maybeString(req.Provider),
		Enabled:     req.Enabled,
		IsDefault:   req.IsDefault,
		Description: maybeString(req.Description),
		SMTP:        smtp,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) AdminEmailConfigDelete(c *gin.Context) {
	var req AdminEmailConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.email.Delete(c.Request.Context(), req.AppID, req.ConfigID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminEmailConfigTest(c *gin.Context) {
	var req AdminEmailConfigTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.TestConfig(c.Request.Context(), req.AppID, req.ConfigID, req.TestEmail)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试成功", result)
}

func (h *Handler) SendEmailCode(c *gin.Context) {
	var req EmailCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.SendVerificationCode(c.Request.Context(), req.AppID, req.Email, req.Purpose, normalizeEmailExpire(req.ExpireMinutes), req.ConfigName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

func (h *Handler) VerifyEmailCode(c *gin.Context) {
	var req EmailVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	valid, err := h.email.VerifyCode(c.Request.Context(), req.AppID, req.Email, req.Code, req.Purpose)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证完成", gin.H{"valid": valid})
}

func (h *Handler) SendPasswordResetEmail(c *gin.Context) {
	var req EmailResetRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.SendPasswordResetEmail(c.Request.Context(), req.AppID, req.Email, req.ResetURL, req.ConfigName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

func (h *Handler) VerifyResetToken(c *gin.Context) {
	var req EmailVerifyResetRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	valid, err := h.email.VerifyResetToken(c.Request.Context(), req.AppID, req.Email, req.Token)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证完成", gin.H{"valid": valid})
}

func (h *Handler) AdminPaymentConfigList(c *gin.Context) {
	var req AdminPaymentConfigListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.payment.ListConfigs(c.Request.Context(), req.AppID, req.PaymentMethod, req.EnabledOnly)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminPaymentConfigDetail(c *gin.Context) {
	var req AdminPaymentConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.payment.Detail(c.Request.Context(), req.AppID, req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminPaymentConfigCreate(c *gin.Context) { h.adminPaymentSave(c, 0) }

func (h *Handler) AdminPaymentConfigUpdate(c *gin.Context) {
	var req AdminPaymentConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminPaymentSaveWithReq(c, req, req.ConfigID)
}

func (h *Handler) adminPaymentSave(c *gin.Context, id int64) {
	var req AdminPaymentConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminPaymentSaveWithReq(c, req, id)
}

func (h *Handler) adminPaymentSaveWithReq(c *gin.Context, req AdminPaymentConfigSaveRequest, id int64) {
	item, err := h.payment.Save(c.Request.Context(), paymentdomain.ConfigMutation{
		ID:            id,
		AppID:         req.AppID,
		PaymentMethod: maybeString(req.PaymentMethod),
		ConfigName:    maybeString(req.ConfigName),
		ConfigData:    req.ConfigData,
		Enabled:       req.Enabled,
		IsDefault:     req.IsDefault,
		Description:   maybeString(req.Description),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) AdminPaymentConfigDelete(c *gin.Context) {
	var req AdminPaymentConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.payment.Delete(c.Request.Context(), req.AppID, req.ConfigID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminPaymentConfigTest(c *gin.Context) {
	var req AdminPaymentConfigTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.payment.TestConfig(c.Request.Context(), req.AppID, req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试完成", result)
}

func (h *Handler) AdminPaymentEpayInit(c *gin.Context) {
	var req AdminPaymentInitEpayRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var cfg paymentdomain.EpayConfig
	if err := decodeJSON(req.EpayConfig, &cfg); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "易支付配置格式错误")
		return
	}
	item, err := h.payment.InitDefaultEpayConfig(c.Request.Context(), req.AppID, cfg)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "初始化成功", item)
}

func (h *Handler) CreatePaymentOrder(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req CreatePaymentOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	payload, order, err := h.payment.CreateOrder(c.Request.Context(), session, req.Subject, req.Body, req.Amount, req.Type, req.ConfigName, req.NotifyURL, req.ReturnURL, req.Metadata, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", gin.H{"payment": payload, "order": order})
}

func (h *Handler) PaymentOrders(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserPaymentOrdersQuery
	_ = bind(c, &query)
	result, err := h.payment.ListUserOrders(c.Request.Context(), session, paymentdomain.OrderListQuery{
		Status: query.Status,
		Page:   query.Page,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) PaymentOrderDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	order, err := h.payment.GetUserOrder(c.Request.Context(), session, c.Param("orderNo"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", order)
}

func (h *Handler) ExportPaymentBill(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	export, err := h.payment.CreateUserOrderBillExport(c.Request.Context(), session, c.Param("orderNo"), time.Duration(req.ExpireMinutes)*time.Minute)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", export)
}

func (h *Handler) DownloadPaymentBill(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	data, filename, err := h.payment.DownloadUserOrderBillExport(c.Request.Context(), session, c.Param("billId"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/pdf", data)
}

func (h *Handler) QueryEpayOrder(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, err := h.payment.GetUserOrder(c.Request.Context(), session, c.Param("orderNo")); err != nil {
		h.writeError(c, err)
		return
	}
	result, err := h.payment.QueryEpayRemoteOrder(c.Request.Context(), c.Param("orderNo"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) EpayCallback(c *gin.Context) {
	_ = c.Request.ParseForm()
	data := map[string]string{}
	for key, values := range c.Request.Form {
		if len(values) > 0 {
			data[key] = values[0]
		}
	}
	result, err := h.payment.HandleEpayCallback(c.Request.Context(), data, c.Request.Method, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result.Paid {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "fail")
}

func (h *Handler) PaymentCallback(c *gin.Context) {
	method := c.Param("method")
	_ = c.Request.ParseForm()
	data := map[string]string{}
	for key, values := range c.Request.Form {
		if len(values) > 0 {
			data[key] = values[0]
		}
	}
	result, err := h.payment.HandleCallback(c.Request.Context(), method, data, c.Request.Method, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result.Paid {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "fail")
}

func (h *Handler) PaymentMethods(c *gin.Context) {
	methods := h.payment.AvailableMethods()
	response.Success(c, 200, "获取成功", methods)
}

func (h *Handler) WorkflowList(c *gin.Context) {
	var req WorkflowListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.List(c.Request.Context(), req.AppID, workflowdomain.ListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), Status: req.Status, Category: req.Category, Keyword: req.Keyword})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowCreate(c *gin.Context) { h.workflowSave(c, 0) }

func (h *Handler) WorkflowUpdate(c *gin.Context) {
	var req WorkflowSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.workflowSaveWithReq(c, req, req.WorkflowID)
}

func (h *Handler) workflowSave(c *gin.Context, id int64) {
	var req WorkflowSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.workflowSaveWithReq(c, req, id)
}

func (h *Handler) workflowSaveWithReq(c *gin.Context, req WorkflowSaveRequest, id int64) {
	definition, err := toWorkflowDefinition(req.Definition)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "工作流定义格式错误")
		return
	}
	item, err := h.workflow.Save(c.Request.Context(), workflowdomain.WorkflowMutation{
		ID:            id,
		AppID:         req.AppID,
		Name:          maybeString(req.Name),
		Description:   maybeString(req.Description),
		Category:      maybeString(req.Category),
		Status:        maybeString(req.Status),
		Definition:    definition,
		TriggerConfig: req.TriggerConfig,
		UIConfig:      req.UIConfig,
		Permissions:   req.Permissions,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) WorkflowDetail(c *gin.Context) {
	var req WorkflowDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.Detail(c.Request.Context(), req.AppID, req.WorkflowID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) WorkflowDelete(c *gin.Context) {
	var req WorkflowDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.workflow.Delete(c.Request.Context(), req.AppID, req.WorkflowID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) WorkflowStart(c *gin.Context) {
	var req WorkflowStartRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	workflowID := req.WorkflowID
	if workflowID == 0 {
		workflowID = req.WorkflowID2
	}
	item, err := h.workflow.Start(c.Request.Context(), req.AppID, workflowID, nil, req.InputData, req.InstanceName, req.Priority)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "启动成功", item)
}

func (h *Handler) WorkflowInstances(c *gin.Context) {
	var req WorkflowInstancesRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	workflowID := req.WorkflowID
	if workflowID == 0 {
		workflowID = req.WorkflowID2
	}
	result, err := h.workflow.Instances(c.Request.Context(), req.AppID, workflowdomain.InstanceQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), WorkflowID: workflowID, Status: req.Status})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowInstanceDetail(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.InstanceDetail(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) WorkflowInstancePause(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.PauseInstance(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "暂停成功", item)
}

func (h *Handler) WorkflowInstanceResume(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.ResumeInstance(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "恢复成功", item)
}

func (h *Handler) WorkflowInstanceCancel(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.CancelInstance(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "取消成功", item)
}

func (h *Handler) WorkflowTasksTodo(c *gin.Context) {
	var req WorkflowTaskQueryRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.UserTasks(c.Request.Context(), req.AppID, workflowdomain.TaskQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), UserID: req.UserID, Status: req.Status, Priority: req.Priority})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowTaskDetail(c *gin.Context) {
	var req WorkflowTaskDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.TaskDetail(c.Request.Context(), req.AppID, req.TaskID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) WorkflowTaskComplete(c *gin.Context) {
	var req WorkflowTaskCompleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	taskID := req.TaskID
	if taskID == 0 {
		taskID = req.TaskID2
	}
	item, err := h.workflow.CompleteTask(c.Request.Context(), req.AppID, taskID, req.Output, req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "处理成功", item)
}

func (h *Handler) WorkflowTaskAssign(c *gin.Context) {
	var req WorkflowTaskAssignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.AssignTask(c.Request.Context(), req.AppID, req.TaskID, req.AssignedTo, req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "分配成功", item)
}

func (h *Handler) WorkflowTaskHistory(c *gin.Context) {
	var req WorkflowTaskDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.workflow.TaskHistory(c.Request.Context(), req.AppID, req.TaskID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) WorkflowTemplates(c *gin.Context) {
	var req WorkflowTemplatesRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.Templates(c.Request.Context(), req.AppID, req.Category, normalizePage(req.Page), normalizeLimit(req.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowCreateFromTemplate(c *gin.Context) {
	var req WorkflowCreateFromTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.CreateFromTemplate(c.Request.Context(), req.AppID, req.TemplateID, req.Name, req.Description, 0)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", item)
}

func (h *Handler) WorkflowSaveAsTemplate(c *gin.Context) {
	var req WorkflowSaveAsTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.SaveAsTemplate(c.Request.Context(), req.AppID, req.WorkflowID, req.TemplateName, req.TemplateDescription, req.Category, req.IsPublic)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) WorkflowValidate(c *gin.Context) {
	var req WorkflowValidateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	definition, err := toWorkflowDefinition(req.Definition)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "工作流定义格式错误")
		return
	}
	if err := h.workflow.ValidateDefinition(*definition); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "校验通过", gin.H{"valid": true})
}

func (h *Handler) WorkflowNodeTypes(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.workflow.NodeTypes())
}

func (h *Handler) WorkflowStatistics(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.Statistics(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowLogs(c *gin.Context) {
	var req WorkflowLogsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.workflow.Logs(c.Request.Context(), req.AppID, req.WorkflowID, req.InstanceID, req.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) WorkflowEngineStatus(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.workflow.EngineStatus())
}

func normalizeEmailExpire(value int) int {
	if value <= 0 {
		return 5
	}
	if value > 60 {
		return 60
	}
	return value
}

func toWorkflowDefinition(input map[string]any) (*workflowdomain.Definition, error) {
	if input == nil {
		return &workflowdomain.Definition{}, nil
	}
	var item workflowdomain.Definition
	if err := decodeJSON(input, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func decodeJSON(input any, target any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
