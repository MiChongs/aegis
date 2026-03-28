package httptransport

import (
	"net/http"
	"strings"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/user"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) AvatarRedirect(c *gin.Context) {
	if h.avatar == nil {
		response.Error(c, http.StatusServiceUnavailable, 50380, "头像服务暂不可用")
		return
	}
	target := h.avatar.BuildWeAvatarURLByHash(c.Param("hash"))
	if target == "" {
		response.Error(c, http.StatusBadRequest, 40090, "头像标识无效")
		return
	}
	if rawQuery := strings.TrimSpace(c.Request.URL.RawQuery); rawQuery != "" {
		target += "?" + rawQuery
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

func (h *Handler) UploadUserAvatar(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()

	uid := session.UserID
	profile, upload, err := h.avatar.UploadUserAvatar(c.Request.Context(), requestBaseURL(c.Request), session, service.AvatarUploadInput{
		ConfigName:    strings.TrimSpace(c.PostForm("config_name")),
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploadedBy:    &uid,
		UploaderType:  "user",
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "上传成功", gin.H{"profile": profile, "upload": upload})
}

func (h *Handler) AdminProfile(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	profile, err := h.admin.GetProfile(c.Request.Context(), access.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachAdminProfileAvatar(c, profile)
	response.Success(c, 200, "获取成功", profile)
}

func (h *Handler) UpdateAdminProfile(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req AdminProfileUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	profile, err := h.admin.UpdateProfile(c.Request.Context(), access.AdminID, admindomain.ProfileUpdate(req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachAdminProfileAvatar(c, profile)
	response.Success(c, 200, "更新成功", profile)
}

func (h *Handler) UploadAdminAvatar(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()

	adminID := access.AdminID
	profile, upload, err := h.avatar.UploadAdminAvatar(c.Request.Context(), requestBaseURL(c.Request), access, service.AvatarUploadInput{
		ConfigName:    strings.TrimSpace(c.PostForm("config_name")),
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploadedBy:    &adminID,
		UploaderType:  "admin",
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "上传成功", gin.H{"profile": profile, "upload": upload})
}

func (h *Handler) attachMyAvatar(c *gin.Context, session *authdomain.Session, view *user.MyView) {
	if h.avatar == nil || session == nil || view == nil {
		return
	}
	view.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request), session.AppID, view.Avatar, view.Email, session.Account)
}

func (h *Handler) attachUserProfileAvatar(c *gin.Context, session *authdomain.Session, profile *user.Profile) {
	if h.avatar == nil || profile == nil {
		return
	}
	appID := int64(0)
	account := ""
	if session != nil {
		appID = session.AppID
		account = session.Account
	}
	profile.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request), appID, profile.Avatar, profile.Email, account)
}

func (h *Handler) attachAdminProfileAvatar(c *gin.Context, profile *admindomain.Profile) {
	if h.avatar == nil || profile == nil {
		return
	}
	profile.Account.Avatar = h.avatar.ResolveAdminAvatar(c.Request.Context(), requestBaseURL(c.Request), profile.Account.Avatar, profile.Account.Email, profile.Account.Account)
}

func (h *Handler) attachAdminAccountAvatar(c *gin.Context, account *admindomain.Account) {
	if h.avatar == nil || account == nil {
		return
	}
	account.Avatar = h.avatar.ResolveAdminAvatar(c.Request.Context(), requestBaseURL(c.Request), account.Avatar, account.Email, account.Account)
}

func requestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
