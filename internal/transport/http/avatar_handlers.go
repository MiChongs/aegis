package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	avatardomain "aegis/internal/domain/avatar"
	"aegis/internal/domain/user"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// AvatarRedirect 老的 /api/avatar/:hash 入口，302 到第三方头像服务。
// 保留它是因为已经有客户端在用这个地址；新链路一律走 /api/avatars/:token。
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

// AvatarImage 永久头像地址。
//
// 这条路由是整个改动的落点：它**不需要登录**、**不带有效期**、
// 也不认地址里的 v —— 永远返回该主体当前的头像。因此任何地方存下来的
// 这个地址（浏览器 localStorage、移动端本地库、邮件正文、CDN）都不会失效。
//
// 缓存策略分两档，判据是请求里的 v 与当前版本是否一致：
//
//	一致  → immutable 一年。那个地址确实指向不变的内容，可以放心长期缓存
//	不一致 → 五分钟 + stale-while-revalidate。这是一份旧地址，
//	         内容会跟着当前头像走，不能让它被长期钉住
func (h *Handler) AvatarImage(c *gin.Context) {
	if h.avatar == nil {
		response.Error(c, http.StatusServiceUnavailable, 50380, "头像服务暂不可用")
		return
	}
	size, _ := strconv.Atoi(strings.TrimSpace(firstNonEmptyQuery(c, "s", "size")))
	image, err := h.avatar.OpenAvatar(c.Request.Context(), c.Param("token"), size, c.GetHeader("If-None-Match"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	if image.Redirect != "" {
		// 外部头像同样按"地址可能是旧的"处理：短缓存，让换绑之后能收敛。
		c.Header("Cache-Control", "public, max-age=300")
		c.Redirect(http.StatusFound, image.Redirect)
		return
	}

	requested := strings.TrimSpace(c.Query("v"))
	if requested != "" && requested == image.Version {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "public, max-age=300, stale-while-revalidate=86400")
	}
	c.Header("ETag", image.ETag)
	// 头像是给 <img> 用的，浏览器不会把它当脚本上下文；但这条路由在平台
	// 自己的域上，nosniff 仍然要给 —— 存储桶里的内容终究是上传方决定的。
	c.Header("X-Content-Type-Options", "nosniff")
	if image.NotModified {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, image.ContentType, image.Data)
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
		Options:       avatarUploadOptions(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "上传成功", gin.H{"profile": profile, "upload": upload})
}

// RemoveUserAvatar 移除自定义头像，回到服务端生成的默认头像。
func (h *Handler) RemoveUserAvatar(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	profile, view, err := h.avatar.RemoveUserAvatar(c.Request.Context(), requestBaseURL(c.Request), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已移除头像", gin.H{"profile": profile, "avatar": view})
}

// ListUserAvatarHistory 头像历史，供「换回上一张」。
func (h *Handler) ListUserAvatarHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	items, err := h.avatar.ListUserAvatarHistory(c.Request.Context(), requestBaseURL(c.Request), session, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items})
}

// RestoreUserAvatar 把历史里的某一张重新设为当前头像。
func (h *Handler) RestoreUserAvatar(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req AvatarRestoreRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	profile, view, err := h.avatar.RestoreUserAvatar(c.Request.Context(), requestBaseURL(c.Request), session, req.AssetID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已恢复头像", gin.H{"profile": profile, "avatar": view})
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
		Options:       avatarUploadOptions(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "上传成功", gin.H{"profile": profile, "upload": upload})
}

// RemoveAdminAvatar 管理员移除自定义头像。
func (h *Handler) RemoveAdminAvatar(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	profile, view, err := h.avatar.RemoveAdminAvatar(c.Request.Context(), requestBaseURL(c.Request), access.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已移除头像", gin.H{"profile": profile, "avatar": view})
}

// avatarUploadOptions 解析可选的裁剪参数。
//
// 裁剪框走表单字段而不是另开一个接口：客户端已经在同一次 multipart 里
// 传了图，把「裁哪一块」拆成第二次请求意味着服务端要先把整张原图存下来等着。
func avatarUploadOptions(c *gin.Context) avatardomain.UploadOptions {
	x, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("crop_x")))
	y, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("crop_y")))
	width, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("crop_width")))
	height, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("crop_height")))
	if width <= 0 || height <= 0 {
		return avatardomain.UploadOptions{}
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return avatardomain.UploadOptions{Crop: &avatardomain.CropRect{X: x, Y: y, Width: width, Height: height}}
}

func firstNonEmptyQuery(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	return ""
}

func (h *Handler) attachMyAvatar(c *gin.Context, session *authdomain.Session, view *user.MyView) {
	if h.avatar == nil || session == nil || view == nil {
		return
	}
	view.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
		session.AppID, session.UserID, view.Avatar, view.Email, session.Account)
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
	userID := profile.UserID
	if userID <= 0 && session != nil {
		userID = session.UserID
	}
	profile.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
		appID, userID, profile.Avatar, profile.Email, account)
}

func (h *Handler) attachAdminProfileAvatar(c *gin.Context, profile *admindomain.Profile) {
	if h.avatar == nil || profile == nil {
		return
	}
	h.attachAdminAccountAvatar(c, &profile.Account)
}

func (h *Handler) attachAdminAccountAvatar(c *gin.Context, account *admindomain.Account) {
	if h.avatar == nil || account == nil {
		return
	}
	account.Avatar = h.avatar.ResolveAdminAvatar(c.Request.Context(), requestBaseURL(c.Request),
		account.ID, account.Avatar, account.Email, account.Account)
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
