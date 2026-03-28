package httptransport

import (
	appdomain "aegis/internal/domain/app"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

type PasswordLoginRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Account  string `json:"account" form:"account" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
	MarkCode string `json:"markcode" form:"markcode"`
}

type PasswordRegisterRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Account  string `json:"account" form:"account" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
	Nickname string `json:"nickname" form:"nickname"`
	MarkCode string `json:"markcode" form:"markcode"`
}

type OAuthAuthURLRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Provider string `json:"provider" form:"provider" binding:"required"`
	MarkCode string `json:"markcode" form:"markcode"`
}

type OAuthMobileLoginRequest struct {
	AppID          int64          `json:"appid" form:"appid" binding:"required"`
	Provider       string         `json:"provider" form:"provider" binding:"required"`
	ProviderUserID string         `json:"providerUserId" form:"providerUserId" binding:"required"`
	UnionID        string         `json:"unionId" form:"unionId"`
	Nickname       string         `json:"nickname" form:"nickname"`
	Avatar         string         `json:"avatar" form:"avatar"`
	Email          string         `json:"email" form:"email"`
	AccessToken    string         `json:"accessToken" form:"accessToken"`
	RefreshToken   string         `json:"refreshToken" form:"refreshToken"`
	MarkCode       string         `json:"markcode" form:"markcode"`
	RawProfile     map[string]any `json:"rawProfile"`
}

type RefreshRequest struct {
	Token        string `json:"token" form:"token"`
	RefreshToken string `json:"refreshToken" form:"refreshToken"`
	MarkCode     string `json:"markcode" form:"markcode"`
}

type VerifyPasswordRequest struct {
	Password string `json:"password" form:"password" binding:"required"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" form:"currentPassword"`
	NewPassword     string `json:"newPassword" form:"newPassword" binding:"required"`
}

type LegacyModifyPasswordRequest struct {
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	OldPassword string `json:"oldPassword" form:"oldPassword"`
	NewPassword string `json:"newPassword" form:"newPassword" binding:"required"`
}

type LegacyModifyUsernameRequest struct {
	AppID int64  `json:"appid" form:"appid" binding:"required"`
	Name  string `json:"name" form:"name" binding:"required"`
}

type LegacyVerifyVIPRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type LegacyDeviceRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type LegacyLogoutDeviceRequest struct {
	AppID int64  `json:"appid" form:"appid" binding:"required"`
	Token string `json:"token" form:"token" binding:"required"`
}

type LegacyDevicesByPasswordRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Account  string `json:"account" form:"account" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
}

type LegacyLogoutDeviceByPasswordRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Account  string `json:"account" form:"account" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
	Token    string `json:"token" form:"token" binding:"required"`
}

type SignInRequest struct {
	Location string `json:"location" form:"location"`
	Source   string `json:"source" form:"source"`
}

type PaginationQuery struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

type RankingQuery struct {
	Type  string `form:"type"`
	Page  int    `form:"page"`
	Limit int    `form:"limit"`
}

type LegacyRankingRequest struct {
	AppID    int64  `json:"appid" form:"appid"`
	Type     string `json:"type" form:"type"`
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"pageSize" form:"pageSize"`
}

type NotificationQuery struct {
	Status string `form:"status"`
	Type   string `form:"type"`
	Level  string `form:"level"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

type NotificationReadRequest struct {
	NotificationID int64 `json:"notificationId" form:"notificationId" binding:"required"`
}

type NotificationReadBatchRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

type NotificationClearRequest struct {
	Status string `json:"status" form:"status"`
	Type   string `json:"type" form:"type"`
	Level  string `json:"level" form:"level"`
}

type AppIDQuery struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type UpdateProfileRequest struct {
	Nickname string                   `json:"nickname" form:"nickname"`
	Avatar   string                   `json:"avatar" form:"avatar"`
	Email    string                   `json:"email" form:"email"`
	Phone    string                   `json:"phone" form:"phone"`
	Birthday string                   `json:"birthday" form:"birthday"`
	Bio      string                   `json:"bio" form:"bio"`
	Contacts []userdomain.ContactInfo `json:"contacts"`
}

type ConfirmProfileChangeRequest struct {
	Field string `json:"field" form:"field" binding:"required"`
	Code  string `json:"code" form:"code" binding:"required"`
}

type UpdateSettingsRequest struct {
	Category string         `json:"category" form:"category" binding:"required"`
	Settings map[string]any `json:"settings" binding:"required"`
}

type ResetSettingsRequest struct {
	Category string `json:"category" form:"category" binding:"required"`
}

type UserSessionRevokeAllRequest struct {
	IncludeCurrent bool `json:"includeCurrent"`
}

type UserLoginAuditQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

type UserSessionAuditQuery struct {
	EventType string `form:"eventType"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

type AdminAppUpsertRequest struct {
	Name                   *string        `json:"name"`
	Status                 *bool          `json:"status"`
	DisabledReason         *string        `json:"disabledReason"`
	RegisterStatus         *bool          `json:"registerStatus"`
	DisabledRegisterReason *string        `json:"disabledRegisterReason"`
	LoginStatus            *bool          `json:"loginStatus"`
	DisabledLoginReason    *string        `json:"disabledLoginReason"`
	Settings               map[string]any `json:"settings"`
}

type AdminAppCreateRequest struct {
	Name *string `json:"name" binding:"required"`
	AdminAppUpsertRequest
}

type LegacyAppCreateRequest struct {
	ID   int64  `json:"id" form:"id" binding:"required"`
	Name string `json:"name" form:"name" binding:"required"`
	Key  string `json:"key" form:"key"`
}

type LegacyAppConfigRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type LegacyAppUpdateConfigRequest struct {
	AppID                   int64          `json:"appid" form:"appid" binding:"required"`
	Name                    string         `json:"name" form:"name"`
	Key                     string         `json:"key" form:"key"`
	Status                  *bool          `json:"status"`
	DisabledReason          string         `json:"disabledReason" form:"disabledReason"`
	RegisterStatus          *bool          `json:"registerStatus"`
	DisabledRegisterReason  string         `json:"disabledRegisterReason" form:"disabledRegisterReason"`
	LoginStatus             *bool          `json:"loginStatus"`
	DisableLoginReason      string         `json:"disableLoginReason" form:"disableLoginReason"`
	LoginCheckDevice        *bool          `json:"loginCheckDevice"`
	LoginCheckUser          *bool          `json:"loginCheckUser"`
	LoginCheckIP            *bool          `json:"loginCheckIp"`
	LoginCheckDeviceTimeOut *int           `json:"loginCheckDeviceTimeOut"`
	MultiDeviceLogin        *bool          `json:"multiDeviceLogin"`
	MultiDeviceLoginNum     *int           `json:"multiDeviceLoginNum"`
	RegisterCaptcha         *bool          `json:"registerCaptcha"`
	RegisterCaptchaTimeOut  *int           `json:"registerCaptchaTimeOut"`
	RegisterCheckIP         *bool          `json:"registerCheckIp"`
	Settings                map[string]any `json:"settings"`
}

type AdminAppPolicyRequest struct {
	LoginCheckDevice        bool `json:"loginCheckDevice"`
	LoginCheckUser          bool `json:"loginCheckUser"`
	LoginCheckIP            bool `json:"loginCheckIp"`
	LoginCheckDeviceTimeout int  `json:"loginCheckDeviceTimeOut"`
	MultiDeviceLogin        bool `json:"multiDeviceLogin"`
	MultiDeviceLimit        int  `json:"multiDeviceLimit"`
	RegisterCaptcha         bool `json:"registerCaptcha"`
	RegisterCaptchaTimeout  int  `json:"registerCaptchaTimeOut"`
	RegisterCheckIP         bool `json:"registerCheckIp"`
}

type AdminBannerUpsertRequest struct {
	Header    *string    `json:"header"`
	Title     *string    `json:"title"`
	Content   *string    `json:"content"`
	URL       *string    `json:"url"`
	Type      *string    `json:"type"`
	Position  *int       `json:"position"`
	Status    *bool      `json:"status"`
	StartTime *time.Time `json:"startTime"`
	EndTime   *time.Time `json:"endTime"`
}

type AdminNoticeUpsertRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
}

type AdminBatchIDsRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

type AdminUserListQuery struct {
	AppID       int64  `json:"appid" form:"appid"`
	Keyword     string `form:"keyword"`
	Account     string `form:"account"`
	Nickname    string `form:"nickname"`
	Email       string `form:"email"`
	Phone       string `form:"phone"`
	RegisterIP  string `form:"registerIp"`
	UserID      *int64 `form:"userId"`
	Enabled     *bool  `form:"enabled"`
	CreatedFrom string `form:"createdFrom"`
	CreatedTo   string `form:"createdTo"`
	Page        int    `form:"page"`
	Limit       int    `form:"limit"`
}

type AdminUserStatusRequest struct {
	Enabled              *bool      `json:"enabled"`
	DisabledEndTime      *time.Time `json:"disabledEndTime"`
	ClearDisabledEndTime bool       `json:"clearDisabledEndTime"`
	DisabledReason       *string    `json:"disabledReason"`
}

type AdminUserBatchStatusRequest struct {
	UserIDs []int64 `json:"userIds" binding:"required"`
	AdminUserStatusRequest
}

type AdminUpdateUserProfileRequest struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
}

type AdminResetUserPasswordRequest struct {
	NewPassword string `json:"newPassword" binding:"required"`
}

type AdminAppTrendQuery struct {
	Days int `form:"days"`
}

type AdminLoginAuditQuery struct {
	Keyword string `form:"keyword"`
	Status  string `form:"status"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

type AdminSessionAuditQuery struct {
	Keyword   string `form:"keyword"`
	EventType string `form:"eventType"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

type AdminRegionStatsQuery struct {
	Type  string `form:"type"`
	Limit int    `form:"limit"`
}

type AdminBulkNotificationRequest struct {
	UserIDs  []int64        `json:"userIds"`
	Keyword  string         `json:"keyword"`
	Enabled  *bool          `json:"enabled"`
	Limit    int            `json:"limit"`
	Type     string         `json:"type" binding:"required"`
	Title    string         `json:"title" binding:"required"`
	Content  string         `json:"content" binding:"required"`
	Level    string         `json:"level"`
	Metadata map[string]any `json:"metadata"`
}

type AdminNotificationListQuery struct {
	AppID   int64  `json:"appid" form:"appid"`
	Keyword string `form:"keyword"`
	Type    string `form:"type"`
	Level   string `form:"level"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

type LegacyAppNotificationCreateRequest struct {
	AppID    int64          `json:"appid" form:"appid" binding:"required"`
	UserIDs  []int64        `json:"user_ids"`
	Keyword  string         `json:"keyword" form:"keyword"`
	Enabled  *bool          `json:"enabled"`
	Limit    int            `json:"limit" form:"limit"`
	Type     string         `json:"type" form:"type"`
	Title    string         `json:"title" form:"title" binding:"required"`
	Content  string         `json:"content" form:"content" binding:"required"`
	Level    string         `json:"level" form:"level"`
	Metadata map[string]any `json:"metadata"`
}

type AdminNotificationDeleteRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

type AdminNotificationDeleteFilterRequest struct {
	Keyword string `json:"keyword"`
	Type    string `json:"type"`
	Level   string `json:"level"`
	Limit   int    `json:"limit"`
}

type AdminSettingsStatsQuery struct {
	AppID int64 `form:"appid" binding:"required"`
}

type AdminUserSettingsQuery struct {
	AppID  int64 `form:"appid" binding:"required"`
	UserID int64 `form:"userId" binding:"required"`
}

type AdminBatchInitializeSettingsRequest struct {
	AppID      int64    `json:"appid" form:"appid" binding:"required"`
	BatchSize  int      `json:"batchSize" form:"batchSize"`
	Categories []string `json:"categories" form:"categories"`
}

type AdminInitializeUserSettingsRequest struct {
	AppID      int64    `json:"appid" form:"appid" binding:"required"`
	UserID     int64    `json:"userId" form:"userId" binding:"required"`
	Categories []string `json:"categories" form:"categories"`
}

type AdminSettingsIntegrityQuery struct {
	AppID      int64 `form:"appid" binding:"required"`
	AutoRepair bool  `form:"autoRepair"`
}

type AdminSettingsCleanupQuery struct {
	AppID  int64 `form:"appid" binding:"required"`
	DryRun *bool `form:"dryRun"`
}

type PasswordPolicyAppIDRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type PasswordPolicySetRequest struct {
	AppID  int64                    `json:"appid" form:"appid" binding:"required"`
	Policy appdomain.PasswordPolicy `json:"policy" binding:"required"`
}

type PasswordPolicyTestRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
}

type AppPointsStatsRequest struct {
	AppID     int64 `json:"appid" form:"appid" binding:"required"`
	TimeRange int   `json:"time_range" form:"time_range"`
}

type AppAdjustIntegralRequest struct {
	UserID int64  `json:"user_id" form:"user_id" binding:"required"`
	AppID  int64  `json:"appid" form:"appid" binding:"required"`
	Amount int64  `json:"amount" form:"amount" binding:"required"`
	Reason string `json:"reason" form:"reason"`
}

type AppAdjustExperienceRequest struct {
	UserID int64  `json:"user_id" form:"user_id" binding:"required"`
	AppID  int64  `json:"appid" form:"appid" binding:"required"`
	Amount int64  `json:"amount" form:"amount" binding:"required"`
	Reason string `json:"reason" form:"reason"`
}

type AppBatchAdjustIntegralRequest struct {
	UserIDs       []int64 `json:"user_ids" form:"user_ids" binding:"required"`
	AppID         int64   `json:"appid" form:"appid" binding:"required"`
	Amount        int64   `json:"amount" form:"amount" binding:"required"`
	OperationType string  `json:"operation_type" form:"operation_type"`
	Reason        string  `json:"reason" form:"reason"`
}

type SiteCreateRequest struct {
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	Name        string `json:"name" form:"name" binding:"required"`
	URL         string `json:"url" form:"url" binding:"required"`
	Description string `json:"description" form:"description" binding:"required"`
	Type        string `json:"type" form:"type" binding:"required"`
	Header      string `json:"image" form:"image"`
	Category    string `json:"category" form:"category"`
}

type SiteUpdateRequest struct {
	ID          int64  `json:"id" form:"id" binding:"required"`
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	Name        string `json:"name" form:"name"`
	URL         string `json:"url" form:"url"`
	Description string `json:"description" form:"description"`
	Type        string `json:"type" form:"type"`
	Header      string `json:"image" form:"image"`
	Category    string `json:"category" form:"category"`
}

type SiteDeleteRequest struct {
	ID    int64 `json:"id" form:"id" binding:"required"`
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type SiteDetailQuery struct {
	ID    int64 `json:"id" form:"id" binding:"required"`
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type SiteListQuery struct {
	AppID     int64  `json:"appid" form:"appid" binding:"required"`
	Page      int    `json:"page" form:"page"`
	PageSize  int    `json:"pageSize" form:"pageSize"`
	Limit     int    `json:"limit" form:"limit"`
	Keyword   string `json:"keyword" form:"keyword"`
	SortBy    string `json:"sortBy" form:"sortBy"`
	SortOrder string `json:"sortOrder" form:"sortOrder"`
	Category  string `json:"category" form:"category"`
	Status    string `json:"status" form:"status"`
}

type RoleApplyRequest struct {
	AppID         int64  `json:"appid" form:"appid" binding:"required"`
	RequestedRole string `json:"requestedRole" form:"requestedRole" binding:"required"`
	Reason        string `json:"reason" form:"reason" binding:"required"`
	Priority      string `json:"priority" form:"priority"`
	ValidDays     int    `json:"validDays" form:"validDays"`
}

type RoleApplicationsQuery struct {
	AppID         int64  `json:"appid" form:"appid" binding:"required"`
	Page          int    `json:"page" form:"page"`
	Limit         int    `json:"limit" form:"limit"`
	Status        string `json:"status" form:"status"`
	RequestedRole string `json:"requestedRole" form:"requestedRole"`
	Priority      string `json:"priority" form:"priority"`
	Keyword       string `json:"keyword" form:"keyword"`
	SortBy        string `json:"sortBy" form:"sortBy"`
	SortOrder     string `json:"sortOrder" form:"sortOrder"`
}

type RoleAppIDQuery struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type RoleResubmitRequest struct {
	AppID  int64  `json:"appid" form:"appid" binding:"required"`
	Reason string `json:"reason" form:"reason"`
}

type VersionCheckQuery struct {
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	VersionCode int64  `json:"versionCode" form:"versionCode" binding:"required"`
	Platform    string `json:"platform" form:"platform"`
}

type AdminAppVersionListRequest struct {
	AppID     int64  `json:"appid" form:"appid" binding:"required"`
	Page      int    `json:"page" form:"page"`
	Limit     int    `json:"limit" form:"limit"`
	Status    string `json:"status" form:"status"`
	Platform  string `json:"platform" form:"platform"`
	ChannelID int64  `json:"channel_id" form:"channel_id"`
}

type AdminAppVersionDetailRequest struct {
	AppID     int64 `json:"appid" form:"appid" binding:"required"`
	VersionID int64 `json:"version_id" form:"version_id" binding:"required"`
}

type AdminAppVersionSaveRequest struct {
	AppID        int64          `json:"appid" form:"appid" binding:"required"`
	VersionID    int64          `json:"version_id" form:"version_id"`
	ChannelID    *int64         `json:"channel_id"`
	Version      string         `json:"version" form:"version"`
	VersionCode  int64          `json:"version_code" form:"version_code"`
	Description  string         `json:"description" form:"description"`
	ReleaseNotes string         `json:"release_notes" form:"release_notes"`
	DownloadURL  string         `json:"download_url" form:"download_url"`
	FileSize     int64          `json:"file_size" form:"file_size"`
	FileHash     string         `json:"file_hash" form:"file_hash"`
	ForceUpdate  *bool          `json:"force_update"`
	UpdateType   string         `json:"update_type" form:"update_type"`
	Platform     string         `json:"platform" form:"platform"`
	MinOSVersion string         `json:"min_os_version" form:"min_os_version"`
	Status       string         `json:"status" form:"status"`
	Metadata     map[string]any `json:"metadata"`
}

type AdminVersionChannelDetailRequest struct {
	AppID     int64 `json:"appid" form:"appid" binding:"required"`
	ChannelID int64 `json:"channel_id" form:"channel_id" binding:"required"`
}

type AdminVersionChannelSaveRequest struct {
	AppID          int64                   `json:"appid" form:"appid" binding:"required"`
	ChannelID      int64                   `json:"channel_id" form:"channel_id"`
	Name           string                  `json:"name" form:"name"`
	Code           string                  `json:"code" form:"code"`
	Description    string                  `json:"description" form:"description"`
	IsDefault      *bool                   `json:"is_default"`
	Status         *bool                   `json:"status"`
	Priority       *int                    `json:"priority"`
	Color          string                  `json:"color" form:"color"`
	Level          string                  `json:"level" form:"level"`
	RolloutPct     *int                    `json:"rollout_pct"`
	Platforms      []string                `json:"platforms"`
	MinVersionCode *int64                  `json:"min_version_code"`
	MaxVersionCode *int64                  `json:"max_version_code"`
	Rules          []appdomain.ChannelRule `json:"rules"`
	TargetAudience map[string]any          `json:"targetAudience"`
}

type AdminVersionChannelUsersRequest struct {
	AppID     int64   `json:"appid" form:"appid" binding:"required"`
	ChannelID int64   `json:"channel_id" form:"channel_id" binding:"required"`
	UserIDs   []int64 `json:"user_ids"`
	Page      int     `json:"page" form:"page"`
	Limit     int     `json:"limit" form:"limit"`
}

type AdminVersionPreviewMatchRequest struct {
	AppID          int64          `json:"appid" form:"appid" binding:"required"`
	TargetAudience map[string]any `json:"targetAudience"`
	ChannelID      int64          `json:"channel_id" form:"channel_id"`
}

type AdminRoleApplicationDetailRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
	ID    int64 `json:"id" form:"id" binding:"required"`
}

type AdminRoleApplicationReviewRequest struct {
	AppID        int64  `json:"appid" form:"appid" binding:"required"`
	ID           int64  `json:"id" form:"id" binding:"required"`
	Action       string `json:"action" form:"action" binding:"required"`
	ReviewReason string `json:"reviewReason" form:"reviewReason"`
}

type AdminRoleApplicationBatchReviewRequest struct {
	AppID        int64   `json:"appid" form:"appid" binding:"required"`
	IDs          []int64 `json:"ids" binding:"required"`
	Action       string  `json:"action" form:"action" binding:"required"`
	ReviewReason string  `json:"reviewReason" form:"reviewReason"`
}

type AdminSiteDetailRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
	ID    int64 `json:"id" form:"id" binding:"required"`
}

type AdminSiteAuditRequest struct {
	AppID  int64  `json:"appid" form:"appid" binding:"required"`
	SiteID int64  `json:"siteId" form:"siteId" binding:"required"`
	Status string `json:"status" form:"status" binding:"required"`
	Reason string `json:"reason" form:"reason"`
}

type AdminSiteBatchAuditRequest struct {
	AppID   int64   `json:"appid" form:"appid" binding:"required"`
	SiteIDs []int64 `json:"siteIds" binding:"required"`
	Status  string  `json:"status" form:"status" binding:"required"`
	Reason  string  `json:"reason" form:"reason"`
}

type AdminSiteTogglePinRequest struct {
	AppID    int64 `json:"appid" form:"appid" binding:"required"`
	ID       int64 `json:"id" form:"id" binding:"required"`
	IsPinned bool  `json:"is_pinned" form:"is_pinned"`
}

type AdminSiteUserRequest struct {
	AppID  int64 `json:"appid" form:"appid" binding:"required"`
	UserID int64 `json:"userId" form:"userId" binding:"required"`
}

func bind(c *gin.Context, target any) error {
	rawBody, hasRawBody := snapshotRequestBody(c)
	if err := c.ShouldBind(target); err != nil {
		if fallbackErr := bindRawJSONFallback(c, target, rawBody, hasRawBody); fallbackErr == nil {
			return nil
		}
		return normalizeBindError(err)
	}
	return nil
}

func snapshotRequestBody(c *gin.Context) ([]byte, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, false
	}
	if strings.Contains(strings.ToLower(c.ContentType()), "multipart/form-data") {
		return nil, false
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, true
}

func bindRawJSONFallback(c *gin.Context, target any, rawBody []byte, hasRawBody bool) error {
	if !hasRawBody || len(bytes.TrimSpace(rawBody)) == 0 || !shouldFallbackToJSONBinding(c, rawBody) {
		return errors.New("skip raw json fallback")
	}
	return binding.JSON.BindBody(rawBody, target)
}

func shouldFallbackToJSONBinding(c *gin.Context, rawBody []byte) bool {
	if c == nil || c.Request == nil {
		return false
	}
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
		return false
	}

	trimmed := bytes.TrimSpace(rawBody)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return false
	}

	contentType := strings.ToLower(strings.TrimSpace(c.ContentType()))
	if contentType == "" {
		return true
	}
	if strings.Contains(contentType, "json") || strings.Contains(contentType, "text/plain") {
		return true
	}
	return false
}

func normalizeBindError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, io.EOF) {
		return errors.New("请求体不能为空")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("请求参数格式错误")
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return errors.New("请求参数格式错误")
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return errors.New("请求参数类型错误")
	}

	var numErr *strconv.NumError
	if errors.As(err, &numErr) {
		return errors.New("请求参数格式错误")
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		for _, item := range validationErrs {
			if item.Tag() == "required" {
				return errors.New("缺少必要的请求参数")
			}
		}
		return errors.New("请求参数校验失败")
	}

	var invalidValidationErr *validator.InvalidValidationError
	if errors.As(err, &invalidValidationErr) {
		return errors.New("请求参数校验失败")
	}

	if strings.Contains(strings.ToLower(err.Error()), "cannot parse") {
		return errors.New("请求参数格式错误")
	}

	return errors.New("请求参数错误")
}

func (h *Handler) writeError(c *gin.Context, err error) {
	if appErr, ok := errors.AsType[*apperrors.AppError](err); ok {
		response.Error(c, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	// 记录真实错误到 gin logger（控制台可见）
	_ = c.Error(err)
	// 直接输出摘要信息（绕过 sanitizeMessage 对 500 的脱敏）
	summary := err.Error()
	if len(summary) > 200 {
		summary = summary[:200]
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusInternalServerError, map[string]any{
		"code":    50000,
		"message": summary,
	})
}

func middlewareBearer(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}

func pathInt64(c *gin.Context, name string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
}

func parseOptionalDateTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, errors.New("invalid datetime")
}

// resolveAppID 从路径参数 :appkey 解析 appkey，查库获取 appid
func resolveAppID(c *gin.Context, appService *service.AppService) (int64, bool) {
	appKey := strings.TrimSpace(c.Param("appkey"))
	if appKey == "" {
		response.Error(c, http.StatusBadRequest, 40000, "应用标识不能为空")
		return 0, false
	}
	app, err := appService.GetAppByKey(c.Request.Context(), appKey)
	if err != nil || app == nil {
		response.Error(c, http.StatusNotFound, 40404, "应用不存在")
		return 0, false
	}
	return app.ID, true
}
