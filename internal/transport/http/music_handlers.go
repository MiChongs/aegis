package httptransport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"aegis/internal/service"
	"aegis/pkg/clientip"
	"aegis/pkg/response"
	"aegis/services/music/contract"
)

// 音乐个性化（docs/music-personalization.md）。模型、推荐与分析都在独立的音乐微服务里，
// 主服务这里只有：事件校验与发布、推荐 / 画像 / 清除的转发、控制台管理接口的反向代理。

func (h *Handler) musicGatewayContext(c *gin.Context) (*gin.Context, bool) {
	if h.music == nil {
		response.Error(c, http.StatusServiceUnavailable, 50396, "音乐个性化服务尚未开通")
		return nil, false
	}
	return c, true
}

// AppMusicEvents POST /api/v1/apps/{appkey}/music/events
//
// 批量上报行为事件（≤100 条）。全部写进事件流才回 200；rejected 里的是非法事件（不要重传），
// 503（50397）时整批重传，重传的事件按 ID 去重。
func (h *Handler) AppMusicEvents(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, ok := h.musicGatewayContext(c); !ok {
		return
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if session.AppID != app.ID {
		response.Error(c, http.StatusUnauthorized, 40100, "令牌不属于该应用")
		return
	}
	var batch service.MusicEventBatch
	body := http.MaxBytesReader(c.Writer, c.Request.Body, contract.MaxBodyBytes)
	if err := json.NewDecoder(body).Decode(&batch); err != nil && err != io.EOF {
		response.Error(c, http.StatusBadRequest, 40000, "事件格式不正确")
		return
	}
	deviceID := strings.TrimSpace(session.DeviceID)
	if deviceID == "" {
		deviceID = strings.TrimSpace(c.GetHeader("X-Device-Id"))
	}
	ip := clientip.FromContext(c.Request.Context(), c.ClientIP())
	result, err := h.music.Report(c.Request.Context(), app, session, deviceID, ip, batch)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上报成功", result)
}

// AppMusicRecommendations GET /api/v1/apps/{appkey}/music/recommendations?scene=&limit=&cursor=&source=&songId=&personalized=
func (h *Handler) AppMusicRecommendations(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, ok := h.musicGatewayContext(c); !ok {
		return
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if session.AppID != app.ID {
		response.Error(c, http.StatusUnauthorized, 40100, "令牌不属于该应用")
		return
	}
	scene := strings.TrimSpace(c.DefaultQuery("scene", contract.SceneHome))
	personalized := true
	if v := strings.TrimSpace(c.Query("personalized")); v != "" {
		personalized, _ = strconv.ParseBool(v)
	}
	q := service.MusicRecommendQuery{
		Scene: scene, Limit: queryInt(c, "limit", 0), Cursor: strings.TrimSpace(c.Query("cursor")),
		Source: strings.TrimSpace(c.Query("source")), SongID: strings.TrimSpace(c.Query("songId")), Personalized: personalized,
		MixID: strings.TrimSpace(c.Query("mixId")), Artist: strings.TrimSpace(c.Query("artist")),
	}
	if c.Request.Method == http.MethodPost {
		body := struct {
			service.MusicRecommendQuery
			Personalized *bool `json:"personalized"`
		}{}
		if err := json.NewDecoder(io.LimitReader(c.Request.Body, 128<<10)).Decode(&body); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "请求体格式不正确")
			return
		}
		q = body.MusicRecommendQuery
		q.Personalized = body.Personalized == nil || *body.Personalized
		if q.Scene = strings.TrimSpace(q.Scene); q.Scene == "" {
			q.Scene = contract.SceneHome
		}
	}
	result, err := h.music.Recommend(c.Request.Context(), app, session, q)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取推荐成功", result)
}

// musicUserOp 第二期用户侧接口的公共部分：登录、应用、开关，然后把参数透传给 music-rec。
func (h *Handler) musicUserOp(c *gin.Context, op string, params map[string]any, okMsg string) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, ok := h.musicGatewayContext(c); !ok {
		return
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if session.AppID != app.ID {
		response.Error(c, http.StatusUnauthorized, 40100, "令牌不属于该应用")
		return
	}
	personalized := true
	if v := strings.TrimSpace(c.Query("personalized")); v != "" {
		personalized, _ = strconv.ParseBool(v)
	}
	result, err := h.music.UserOp(c.Request.Context(), app, session, op, params, personalized)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, okMsg, result)
}

// musicBody 读 JSON 请求体（≤ 64 KB）；格式不对时已回 400。
func musicBody(c *gin.Context) (map[string]any, bool) {
	body := map[string]any{}
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 64<<10)).Decode(&body); err != nil && err != io.EOF {
		response.Error(c, http.StatusBadRequest, 40000, "请求体格式不正确")
		return nil, false
	}
	return body, true
}

// AppMusicMixes GET /music/mixes：风格歌单列表。
func (h *Handler) AppMusicMixes(c *gin.Context) {
	h.musicUserOp(c, "mixes", map[string]any{}, "获取风格歌单成功")
}

// AppMusicArtists GET /music/artists/recommended?limit=12：推荐歌手。
func (h *Handler) AppMusicArtists(c *gin.Context) {
	h.musicUserOp(c, "artists", map[string]any{"limit": queryInt(c, "limit", 12)}, "获取推荐歌手成功")
}

// AppMusicOnboarding GET /music/onboarding：新用户偏好选择。
func (h *Handler) AppMusicOnboarding(c *gin.Context) {
	h.musicUserOp(c, "onboarding", map[string]any{}, "获取偏好选择成功")
}

// AppMusicPreferences GET /music/preferences。
func (h *Handler) AppMusicPreferences(c *gin.Context) {
	h.musicUserOp(c, "prefs_get", map[string]any{}, "获取音乐偏好成功")
}

// AppMusicSavePreferences PUT /music/preferences（缺省字段保持原值）。
func (h *Handler) AppMusicSavePreferences(c *gin.Context) {
	body, ok := musicBody(c)
	if !ok {
		return
	}
	h.musicUserOp(c, "prefs_put", body, "音乐偏好已保存")
}

// AppMusicFeedback GET /music/feedback?kind=track|artist|suppressed&page=&pageSize=：我的不喜欢。
func (h *Handler) AppMusicFeedback(c *gin.Context) {
	h.musicUserOp(c, "feedback_list", map[string]any{"kind": strings.TrimSpace(c.Query("kind")),
		"page": queryInt(c, "page", 1), "pageSize": queryInt(c, "pageSize", 50)}, "获取不感兴趣的内容成功")
}

// AppMusicUndoFeedback DELETE /music/feedback {kind, workKey | artist}：撤销。
func (h *Handler) AppMusicUndoFeedback(c *gin.Context) {
	body, ok := musicBody(c)
	if !ok {
		return
	}
	h.musicUserOp(c, "feedback_undo", body, "已撤销")
}

// AppMusicReport GET /music/report?period=week|month|year&offset=0：听歌报告。
func (h *Handler) AppMusicReport(c *gin.Context) {
	h.musicUserOp(c, "report", map[string]any{"period": strings.TrimSpace(c.DefaultQuery("period", "week")),
		"offset": queryInt(c, "offset", 0)}, "获取听歌报告成功")
}

// AppMusicTrendingSearches GET /music/search/trending?limit=10：热搜。
func (h *Handler) AppMusicTrendingSearches(c *gin.Context) {
	h.musicUserOp(c, "trending", map[string]any{"limit": queryInt(c, "limit", 10)}, "获取热搜成功")
}

// AppMusicRerank POST /music/search/rerank {query, items}：搜索结果按口味轻度重排。
func (h *Handler) AppMusicRerank(c *gin.Context) {
	body, ok := musicBody(c)
	if !ok {
		return
	}
	h.musicUserOp(c, "rerank", body, "重排成功")
}

// AppMusicExport GET /music/export?before=&limit=500：导出我的听歌记录。
func (h *Handler) AppMusicExport(c *gin.Context) {
	before, _ := strconv.ParseInt(strings.TrimSpace(c.Query("before")), 10, 64)
	h.musicUserOp(c, "export", map[string]any{"before": before, "limit": queryInt(c, "limit", 500)}, "导出听歌记录成功")
}

// AppMusicProfile GET /api/v1/apps/{appkey}/music/profile?days=30
func (h *Handler) AppMusicProfile(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, ok := h.musicGatewayContext(c); !ok {
		return
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if session.AppID != app.ID {
		response.Error(c, http.StatusUnauthorized, 40100, "令牌不属于该应用")
		return
	}
	profile, err := h.music.Profile(c.Request.Context(), app, session, queryInt(c, "days", 30))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取听歌画像成功", profile)
}

// AppMusicClearProfile DELETE /api/v1/apps/{appkey}/music/profile
//
// 清除自己的全部听歌数据（事件、实时状态、模型里的口味）。
func (h *Handler) AppMusicClearProfile(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, ok := h.musicGatewayContext(c); !ok {
		return
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if session.AppID != app.ID {
		response.Error(c, http.StatusUnauthorized, 40100, "令牌不属于该应用")
		return
	}
	if err := h.music.ClearProfile(c.Request.Context(), app, session); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "听歌数据已清除", gin.H{"deleted": true})
}

var musicProxyTransport = &http.Transport{
	Proxy:                 nil,
	MaxIdleConns:          32,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
}

// AdminMusicProxy /api/admin/apps/:appkey/music/*path → music-rec /v1/apps/<应用 ID>/*path
//
// 主服务负责管理员登录、应用范围与权限（app:music:read / app:music:manage）与审计；
// 请求体、查询参数、响应信封都不改。
func (h *Handler) AdminMusicProxy(c *gin.Context) {
	if h.music == nil || !h.music.Enabled() {
		response.Error(c, http.StatusServiceUnavailable, 50396, "音乐个性化服务尚未开通（未配置 MUSIC_RPC_URL）")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	target, err := url.Parse(h.music.AdminBaseURL())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50397, "音乐服务地址无效")
		return
	}
	operator := "admin"
	if session, ok := adminAccessSession(c); ok && session != nil {
		name := strings.TrimSpace(session.DisplayName)
		if name == "" {
			name = session.Account
		}
		operator = name
	}
	path := "/v1/apps/" + strconv.FormatInt(appID, 10) + "/" + strings.TrimPrefix(c.Param("path"), "/")
	proxy := &httputil.ReverseProxy{
		Transport: musicProxyTransport,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = path
			r.Out.URL.RawPath = ""
			r.Out.URL.RawQuery = c.Request.URL.RawQuery
			r.Out.Host = target.Host
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Cookie")
			r.Out.Header.Set("X-Music-Token", h.music.InternalToken())
			r.Out.Header.Set("X-Music-Operator", url.PathEscape(operator))
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			response.Error(c, http.StatusBadGateway, 50397, "音乐服务暂时不可用")
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
