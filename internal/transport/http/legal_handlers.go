package httptransport

import (
	"net/http"
	"strings"

	legaldomain "aegis/internal/domain/legal"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// 法律文本的两组入口：
//
//   - 公开读取（/api/legal/*）：**免登录**。登录页和注册页在用户还没有账号时
//     就要链到它，要求登录才能读条款是荒谬的。
//   - 管理端读写（/api/admin/system/legal/*）：限超级管理员。
//
// 语言由服务端协商，前端不做任何判断：`?locale=` > 用户显式选择 > Accept-Language。
// 让前端自己挑语言的结果是「浏览器语言是繁中、后端只有简中和英文」这种情形下，
// 每一端各挑各的，同一次访问里页面标题和正文可能不是同一种语言。

// LegalPreviewRequest 预览一段草稿。
type LegalPreviewRequest struct {
	Body string `json:"body"`
}

// LegalDocumentRequest 管理端写入一份法律文本。
type LegalDocumentRequest struct {
	Title       string `json:"title" binding:"required"`
	Body        string `json:"body" binding:"required"`
	Version     string `json:"version"`
	EffectiveAt string `json:"effectiveAt"` // YYYY-MM-DD 或 RFC3339，空串表示不设置
	Published   *bool  `json:"published"`
}

// legalPrefs 组装语言偏好，顺序即优先级。
func legalPrefs(c *gin.Context) []string {
	return []string{
		strings.TrimSpace(c.Query("locale")),
		strings.TrimSpace(c.GetHeader("X-Aegis-Locale")),
		strings.TrimSpace(c.GetHeader("Accept-Language")),
	}
}

// PublicLegalCatalog 列出全部法律文本及其可用语言。
func (h *Handler) PublicLegalCatalog(c *gin.Context) {
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	entries, err := h.legal.Catalog(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": entries})
}

// PublicLegalDocument 取一份法律文本（免登录）。
func (h *Handler) PublicLegalDocument(c *gin.Context) {
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	docType := legaldomain.DocType(strings.TrimSpace(c.Param("docType")))
	view, err := h.legal.Document(c.Request.Context(), docType, legalPrefs(c)...)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 语言协商的结果必须让缓存层看见，否则 CDN 会把中文版发给英文读者。
	c.Header("Vary", "Accept-Language, X-Aegis-Locale")
	c.Header("Content-Language", view.Locale)
	response.Success(c, 200, "获取成功", view)
}

// AdminListLegalDocuments 管理端列表：自定义与内置合并。
func (h *Handler) AdminListLegalDocuments(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40314, "仅超级管理员可管理法律文本")
		return
	}
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	items, err := h.legal.AdminList(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"items": items,
		// 内置文本引用联系邮箱，没配就会印出占位文字 —— 让管理员在这一页看见，
		// 而不是等用户翻到隐私政策最后一节才发现。
		"contactConfigured": h.legal.ContactConfigured(),
		// 准据语言：多语言法律文本必须指定其中一版为准，控制台要标出来是哪一版
		"authoritativeLocale": h.legal.AuthoritativeLocale(),
		// 内置语言目录，供「新增语言」选择器使用。前端不另抄一份 ——
		// 抄了就会出现「选择器里有这个语言、存进去发现没有内置底稿」。
		"builtinLocales": h.legal.BuiltinLocales(),
	})
}

// AdminPreviewLegalDocument 把一段草稿按当前部署的值渲染出来，供编辑器实时预览。
//
// 走服务端而不是前端自己替换：平台名与联系邮箱的取值规则（品牌配置优先、
// 未配置时印占位文字而不是生造地址）只在服务端实现一次，
// 前端另写一套必然和公开页渲染出的结果不一致 —— 而预览的全部意义就是「所见即所得」。
func (h *Handler) AdminPreviewLegalDocument(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40314, "仅超级管理员可管理法律文本")
		return
	}
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	var req LegalPreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	locale := strings.TrimSpace(c.Param("locale"))
	response.Success(c, 200, "渲染成功", gin.H{
		"body": h.legal.RenderTokens(c.Request.Context(), req.Body, locale),
	})
}

// AdminGetLegalDocument 取一份文本用于编辑；没有自定义版本时返回内置版本作底稿。
func (h *Handler) AdminGetLegalDocument(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40314, "仅超级管理员可管理法律文本")
		return
	}
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	item, err := h.legal.AdminGet(c.Request.Context(),
		legaldomain.DocType(strings.TrimSpace(c.Param("docType"))), strings.TrimSpace(c.Param("locale")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminSaveLegalDocument 写入一份文本。
func (h *Handler) AdminSaveLegalDocument(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40314, "仅超级管理员可管理法律文本")
		return
	}
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	var req LegalDocumentRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	effectiveAt, err := service.LegalEffectiveFromString(req.EffectiveAt)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 缺省发布：管理员写完一份条款的默认意图是让它生效，
	// 留一个默认不发布的开关只会造出「改了但线上没变」的困惑。
	published := true
	if req.Published != nil {
		published = *req.Published
	}

	adminID := session.AdminID
	docType := legaldomain.DocType(strings.TrimSpace(c.Param("docType")))
	item, err := h.legal.Save(c.Request.Context(), legaldomain.SaveInput{
		DocType:     docType,
		Locale:      strings.TrimSpace(c.Param("locale")),
		Title:       req.Title,
		Body:        req.Body,
		Version:     req.Version,
		EffectiveAt: effectiveAt,
		Published:   published,
		UpdatedBy:   &adminID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "legal-document", string(docType)+"/"+item.Locale,
		"管理员 "+session.DisplayName+" 更新了法律文本 "+item.Title)
	response.Success(c, 200, "保存成功", item)
}

// AdminDeleteLegalDocument 删除自定义版本，该语言随即回落到内置版本。
func (h *Handler) AdminDeleteLegalDocument(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40314, "仅超级管理员可管理法律文本")
		return
	}
	if h.legal == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "法律文本服务暂不可用")
		return
	}
	docType := legaldomain.DocType(strings.TrimSpace(c.Param("docType")))
	locale := strings.TrimSpace(c.Param("locale"))
	if err := h.legal.Delete(c.Request.Context(), docType, locale); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "legal-document", string(docType)+"/"+locale,
		"管理员 "+session.DisplayName+" 恢复了法律文本的内置版本")
	response.Success(c, 200, "已恢复内置版本", gin.H{"deleted": true})
}
