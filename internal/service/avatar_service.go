package service

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	storagedomain "aegis/internal/domain/storage"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

const (
	avatarStoragePrefix = "storage://"
	avatarMaxUploadSize = 10 << 20
)

var phoneIdentifierPattern = regexp.MustCompile(`^[+]?\d[\d\s\-()]{5,}$`)

type AvatarUploadInput struct {
	ConfigName    string
	FileName      string
	ContentType   string
	ContentLength int64
	Content       io.Reader
	UploadedBy    *int64
	UploaderType  string // "user" / "admin"
}

type AvatarUploadResult struct {
	AvatarURL string                      `json:"avatar"`
	Reference string                      `json:"reference"`
	Storage   *storagedomain.StoredObject `json:"storage"`
}

type AvatarService struct {
	log     *zap.Logger
	storage *StorageService
	user    *UserService
	admin   *AdminService
}

func NewAvatarService(log *zap.Logger, storage *StorageService, user *UserService, admin *AdminService) *AvatarService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AvatarService{log: log, storage: storage, user: user, admin: admin}
}

func (s *AvatarService) BuildWeAvatarURL(identifier string, algo string) string {
	normalized := normalizeAvatarIdentifier(identifier)
	if normalized == "" {
		return ""
	}
	return s.BuildWeAvatarURLByHash(hashAvatarIdentifier(normalized, algo))
}

func (s *AvatarService) BuildWeAvatarURLByHash(hash string) string {
	hash = sanitizeAvatarHash(hash)
	if hash == "" {
		return ""
	}
	return "https://weavatar.com/avatar/" + hash
}

func (s *AvatarService) ResolveUserAvatar(ctx context.Context, baseURL string, appID int64, rawAvatar string, identifiers ...string) string {
	return s.resolveAvatar(ctx, baseURL, appID, rawAvatar, identifiers...)
}

func (s *AvatarService) ResolveAdminAvatar(ctx context.Context, baseURL string, rawAvatar string, identifiers ...string) string {
	return s.resolveAvatar(ctx, baseURL, 0, rawAvatar, identifiers...)
}

func (s *AvatarService) UploadUserAvatar(ctx context.Context, baseURL string, session *authdomain.Session, input AvatarUploadInput) (*userdomain.Profile, *AvatarUploadResult, error) {
	if session == nil {
		return nil, nil, apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	stored, ref, err := s.uploadAvatarObject(ctx, session.AppID, avatarObjectKeyForUser(session.AppID, session.UserID, input.FileName), input)
	if err != nil {
		return nil, nil, err
	}
	current, err := s.user.GetProfile(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	updated, err := s.user.UpdateProfile(ctx, session, userdomain.ProfileUpdate{
		Nickname: current.Nickname,
		Email:    current.Email,
		Avatar:   ref,
	})
	if err != nil {
		return nil, nil, err
	}
	profile := updated.Profile
	if profile == nil {
		return nil, nil, apperrors.New(50000, http.StatusInternalServerError, "用户头像更新结果为空")
	}
	resolved := s.ResolveUserAvatar(ctx, baseURL, session.AppID, profile.Avatar, profile.Email, session.Account)
	profile.Avatar = resolved
	return profile, &AvatarUploadResult{AvatarURL: resolved, Reference: ref, Storage: stored}, nil
}

func (s *AvatarService) UploadAdminAvatar(ctx context.Context, baseURL string, access *admindomain.AccessContext, input AvatarUploadInput) (*admindomain.Profile, *AvatarUploadResult, error) {
	if access == nil {
		return nil, nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	stored, ref, err := s.uploadAvatarObject(ctx, 0, avatarObjectKeyForAdmin(access.AdminID, input.FileName), input)
	if err != nil {
		return nil, nil, err
	}
	current, err := s.admin.GetProfile(ctx, access.AdminID)
	if err != nil {
		return nil, nil, err
	}
	updated, err := s.admin.UpdateProfile(ctx, access.AdminID, admindomain.ProfileUpdate{
		DisplayName: current.Account.DisplayName,
		Email:       current.Account.Email,
		Avatar:      ref,
	})
	if err != nil {
		return nil, nil, err
	}
	resolved := s.ResolveAdminAvatar(ctx, baseURL, updated.Account.Avatar, updated.Account.Email, updated.Account.Account)
	updated.Account.Avatar = resolved
	return updated, &AvatarUploadResult{AvatarURL: resolved, Reference: ref, Storage: stored}, nil
}

func (s *AvatarService) resolveAvatar(ctx context.Context, baseURL string, appID int64, rawAvatar string, identifiers ...string) string {
	rawAvatar = strings.TrimSpace(rawAvatar)
	if rawAvatar != "" {
		if configID, objectKey, ok := parseAvatarStorageReference(rawAvatar); ok {
			if resolved := s.resolveStoredAvatar(ctx, baseURL, appID, configID, objectKey); resolved != "" {
				return resolved
			}
		} else {
			return rawAvatar
		}
	}
	for _, identifier := range identifiers {
		if weavatar := s.BuildWeAvatarURL(identifier, "sha256"); weavatar != "" {
			return weavatar
		}
	}
	return ""
}

func (s *AvatarService) resolveStoredAvatar(ctx context.Context, baseURL string, appID int64, configID int64, objectKey string) string {
	result, ticketID, err := s.storage.CreateObjectLinkByConfigID(ctx, appID, configID, storagedomain.LinkRequest{
		ObjectKey: objectKey,
		ExpiresIn: 30 * time.Minute,
	})
	if err != nil {
		s.log.Warn("resolve avatar object failed", zap.Int64("appid", appID), zap.Int64("config_id", configID), zap.String("object_key", objectKey), zap.Error(err))
		return ""
	}
	if result == nil {
		return ""
	}
	if ticketID != "" {
		return joinURL(baseURL, "/api/storage/proxy/"+url.PathEscape(ticketID))
	}
	return strings.TrimSpace(result.URL)
}

func (s *AvatarService) uploadAvatarObject(ctx context.Context, appID int64, objectKey string, input AvatarUploadInput) (*storagedomain.StoredObject, string, error) {
	contentType, ext, err := validateAvatarUpload(input.FileName, input.ContentType, input.ContentLength)
	if err != nil {
		return nil, "", err
	}
	stored, err := s.storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		AppID:         appID,
		ConfigName:    strings.TrimSpace(input.ConfigName),
		ObjectKey:     strings.TrimSuffix(objectKey, path.Ext(objectKey)) + ext,
		FileName:      strings.TrimSpace(input.FileName),
		ContentType:   contentType,
		ContentLength: input.ContentLength,
		CacheControl:  "public, max-age=31536000, immutable",
		Metadata: map[string]string{
			"module": "avatar",
		},
		Content:      input.Content,
		UploadedBy:   input.UploadedBy,
		UploaderType: input.UploaderType,
	})
	if err != nil {
		return nil, "", err
	}
	return stored, buildAvatarStorageReference(stored.ConfigID, stored.Key), nil
}

func buildAvatarStorageReference(configID int64, objectKey string) string {
	if configID <= 0 || strings.TrimSpace(objectKey) == "" {
		return ""
	}
	return fmt.Sprintf("%s%d/%s", avatarStoragePrefix, configID, url.PathEscape(strings.TrimSpace(objectKey)))
}

func parseAvatarStorageReference(raw string) (int64, string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(raw), avatarStoragePrefix) {
		return 0, "", false
	}
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), avatarStoragePrefix)
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	configID, err := parsePositiveInt64(parts[0])
	if err != nil {
		return 0, "", false
	}
	objectKey, err := url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(objectKey) == "" {
		return 0, "", false
	}
	return configID, objectKey, true
}

func normalizeAvatarIdentifier(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "@") {
		return value
	}
	if !phoneIdentifierPattern.MatchString(value) {
		return ""
	}
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	value = replacer.Replace(value)
	if len(strings.TrimLeft(value, "+")) < 6 {
		return ""
	}
	return value
}

func hashAvatarIdentifier(identifier string, algo string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(algo)) {
	case "", "sha256":
		sum := sha256.Sum256([]byte(identifier))
		return hex.EncodeToString(sum[:])
	case "md5":
		sum := md5.Sum([]byte(identifier))
		return hex.EncodeToString(sum[:])
	default:
		sum := sha256.Sum256([]byte(identifier))
		return hex.EncodeToString(sum[:])
	}
}

func sanitizeAvatarHash(hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != 32 && len(hash) != 64 {
		return ""
	}
	for _, ch := range hash {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	return hash
}

func validateAvatarUpload(fileName string, contentType string, size int64) (string, string, error) {
	if size <= 0 {
		return "", "", apperrors.New(40087, http.StatusBadRequest, "头像文件不能为空")
	}
	if size > avatarMaxUploadSize {
		return "", "", apperrors.New(40088, http.StatusBadRequest, "头像文件不能超过 10MB")
	}
	ext := strings.ToLower(strings.TrimSpace(path.Ext(fileName)))
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if mapped := avatarContentTypeByExt(ext); mapped != "" {
		return mapped, ext, nil
	}
	if mappedExt := avatarExtByContentType(contentType); mappedExt != "" {
		return contentType, mappedExt, nil
	}
	return "", "", apperrors.New(40089, http.StatusBadRequest, "头像仅支持 JPG、PNG、GIF、WEBP")
}

func avatarContentTypeByExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

func avatarExtByContentType(contentType string) string {
	switch contentType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func avatarObjectKeyForUser(appID int64, userID int64, fileName string) string {
	now := time.Now().UTC()
	return path.Join("avatars", "apps", fmt.Sprintf("%d", appID), "users", fmt.Sprintf("%d", userID), now.Format("2006"), now.Format("01"), now.Format("02030405")+"_avatar"+strings.ToLower(path.Ext(strings.TrimSpace(fileName))))
}

func avatarObjectKeyForAdmin(adminID int64, fileName string) string {
	now := time.Now().UTC()
	return path.Join("avatars", "admins", fmt.Sprintf("%d", adminID), now.Format("2006"), now.Format("01"), now.Format("02030405")+"_avatar"+strings.ToLower(path.Ext(strings.TrimSpace(fileName))))
}

func joinURL(baseURL string, relative string) string {
	relative = strings.TrimSpace(relative)
	if relative == "" {
		return ""
	}
	if strings.HasPrefix(relative, "http://") || strings.HasPrefix(relative, "https://") {
		return relative
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		if strings.HasPrefix(relative, "/") {
			return relative
		}
		return "/" + relative
	}
	if strings.HasPrefix(relative, "/") {
		return baseURL + relative
	}
	return baseURL + "/" + relative
}

func parsePositiveInt64(value string) (int64, error) {
	var result int64
	for _, ch := range strings.TrimSpace(value) {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid integer")
		}
		result = result*10 + int64(ch-'0')
	}
	if result <= 0 {
		return 0, fmt.Errorf("invalid integer")
	}
	return result, nil
}
