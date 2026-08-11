package httptransport

import (
	"net/http"
	"strconv"

	devicedomain "aegis/internal/domain/device"
	auditmiddleware "aegis/internal/middleware"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 设备营销名称字典 CRUD
//
// 所有路由要求 **平台管理员（超级管理员）** 身份。
// 路由挂载在 /api/admin/device-marketing-names/* 下：
//   - GET    /                → 分页列表
//   - GET    /:id             → 详情
//   - POST   /                → 新增
//   - PUT    /:id             → 更新
//   - DELETE /:id             → 删除
//   - POST   /seed            → 重新 seed（幂等补齐缺失映射）
//   - POST   /lookup          → 根据 platform+identifier 查询营销名称（运行时调试用）

func (h *Handler) ListDeviceMarketingNames(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	var q DeviceMarketingQuery
	_ = c.ShouldBindQuery(&q)
	page, err := h.deviceMarketing.List(c.Request.Context(), devicedomain.Filter{
		Platform: q.Platform, Keyword: q.Keyword, Source: q.Source, Manufacturer: q.Manufacturer,
		Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", page)
}

// ListDeviceMarketingManufacturers 返回字典中已登记的厂商名列表，供前端筛选
func (h *Handler) ListDeviceMarketingManufacturers(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	platform := c.Query("platform")
	list, err := h.deviceMarketing.ListManufacturers(c.Request.Context(), platform)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"items": list})
}

func (h *Handler) GetDeviceMarketingName(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "id 无效")
		return
	}
	item, err := h.deviceMarketing.Get(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", item)
}

func (h *Handler) CreateDeviceMarketingName(c *gin.Context) {
	ctx, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	var req DeviceMarketingCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.deviceMarketing.Create(c.Request.Context(), devicedomain.CreateInput{
		Platform:            req.Platform,
		Identifier:          req.Identifier,
		MarketingName:       req.MarketingName,
		Manufacturer:        req.Manufacturer,
		ManufacturerIconURL: req.ManufacturerIconURL,
		DeviceImageURL:      req.DeviceImageURL,
		Notes:               req.Notes,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAuditWithAdmin(c, ctx.AdminID, ctx.Account,
		"device_marketing.create", "device_marketing_name", strconv.FormatInt(item.ID, 10),
		"新增设备营销名称："+req.Platform+"/"+req.Identifier+" → "+req.MarketingName, "success")
	response.Success(c, 200, "创建成功", item)
}

func (h *Handler) UpdateDeviceMarketingName(c *gin.Context) {
	ctx, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "id 无效")
		return
	}
	var req DeviceMarketingUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.deviceMarketing.Update(c.Request.Context(), id, devicedomain.UpdateInput{
		Platform:            req.Platform,
		Identifier:          req.Identifier,
		MarketingName:       req.MarketingName,
		Manufacturer:        req.Manufacturer,
		ManufacturerIconURL: req.ManufacturerIconURL,
		DeviceImageURL:      req.DeviceImageURL,
		Notes:               req.Notes,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAuditWithAdmin(c, ctx.AdminID, ctx.Account,
		"device_marketing.update", "device_marketing_name", strconv.FormatInt(id, 10),
		"更新设备营销名称", "success")
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) DeleteDeviceMarketingName(c *gin.Context) {
	ctx, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "id 无效")
		return
	}
	if err := h.deviceMarketing.Delete(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAuditWithAdmin(c, ctx.AdminID, ctx.Account,
		"device_marketing.delete", "device_marketing_name", strconv.FormatInt(id, 10),
		"删除设备营销名称", "success")
	auditmiddleware.MarkAuditRecorded(c)
	response.Success(c, 200, "删除成功", nil)
}

// ReseedDeviceMarketingNames 幂等重新种子（不覆盖现有数据，仅补齐缺失）
func (h *Handler) ReseedDeviceMarketingNames(c *gin.Context) {
	ctx, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	stats, err := h.deviceMarketing.Reseed(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAuditWithAdmin(c, ctx.AdminID, ctx.Account,
		"device_marketing.reseed", "device_marketing_name", "",
		"重新种子设备营销名称", "success")
	response.Success(c, 200, "ok", stats)
}

// LookupDeviceMarketingName 运行时查询：根据 platform+identifier 返回营销名称
// 未找到返回 404，调用方可回退到 UA 推断
func (h *Handler) LookupDeviceMarketingName(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	platform := c.Query("platform")
	identifier := c.Query("identifier")
	item, err := h.deviceMarketing.Lookup(c.Request.Context(), platform, identifier)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if item == nil {
		response.Error(c, http.StatusNotFound, 40404, "未找到映射")
		return
	}
	response.Success(c, 200, "ok", item)
}
