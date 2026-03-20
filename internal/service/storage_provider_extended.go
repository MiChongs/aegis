package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"path"
	"strconv"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	redislib "github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type dropboxProvider struct {
	httpClient *http.Client
	redis      *redislib.Client
	keyPrefix  string
}

func newDropboxProvider(httpClient *http.Client, redis *redislib.Client, keyPrefix string) storageProvider {
	return &dropboxProvider{httpClient: httpClient, redis: redis, keyPrefix: keyPrefix}
}

func (p *dropboxProvider) Name() string { return storagedomain.ProviderDropbox }

func (p *dropboxProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	resp, err := p.apiJSON(ctx, cfg, "https://api.dropboxapi.com/2/users/get_current_account", nil)
	if err != nil {
		return nil, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("dropbox health status=%d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return map[string]any{"account_id": payload["account_id"], "name": payload["name"]}, nil
}

func (p *dropboxProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	token, err := p.resolveToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arg := map[string]any{
		"path":       "/" + strings.Trim(strings.TrimSpace(input.ObjectKey), "/"),
		"mode":       "overwrite",
		"autorename": false,
		"mute":       true,
	}
	raw, _ := json.Marshal(arg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://content.dropboxapi.com/2/files/upload", input.Content)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Dropbox-API-Arg", string(raw))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("dropbox upload status=%d", resp.StatusCode)
	}
	var payload struct {
		PathDisplay string `json:"path_display"`
		ID          string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return &storagedomain.StoredObject{
		Bucket:      "dropbox",
		Key:         strings.TrimPrefix(payload.PathDisplay, "/"),
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		ETag:        payload.ID,
		URL:         composeDropboxWebURL(cfg, input.ObjectKey),
	}, nil
}

func (p *dropboxProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	token, err := p.resolveToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arg := map[string]any{"path": "/" + strings.Trim(strings.TrimSpace(objectKey), "/")}
	raw, _ := json.Marshal(arg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://content.dropboxapi.com/2/files/download", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Dropbox-API-Arg", string(raw))
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		closeSilently(resp.Body)
		return nil, fmt.Errorf("dropbox open status=%d", resp.StatusCode)
	}
	reader := &storagedomain.ObjectReader{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		FileName:    path.Base(objectKey),
	}
	if length, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil {
		reader.Size = length
	}
	return reader, nil
}

func (p *dropboxProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	token, err := p.resolveToken(ctx, cfg)
	if err != nil {
		return "", err
	}
	payload := map[string]any{"path": "/" + strings.Trim(strings.TrimSpace(objectKey), "/")}
	resp, err := p.apiJSONWithToken(ctx, token, "https://api.dropboxapi.com/2/sharing/create_shared_link_with_settings", payload)
	if err != nil {
		return "", err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return composeDropboxWebURL(cfg, objectKey), nil
	}
	var result struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if strings.TrimSpace(result.URL) == "" {
		return composeDropboxWebURL(cfg, objectKey), nil
	}
	return result.URL, nil
}

func (p *dropboxProvider) apiJSON(ctx context.Context, cfg *storagedomain.Config, targetURL string, payload any) (*http.Response, error) {
	token, err := p.resolveToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return p.apiJSONWithToken(ctx, token, targetURL, payload)
}

func (p *dropboxProvider) apiJSONWithToken(ctx context.Context, token string, targetURL string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return p.httpClient.Do(req)
}

func (p *dropboxProvider) resolveToken(ctx context.Context, cfg *storagedomain.Config) (string, error) {
	raw, err := decodeDropboxConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(raw.RefreshToken) != "" && strings.TrimSpace(raw.AppKey) != "" && strings.TrimSpace(raw.AppSecret) != "" {
		return p.refreshToken(ctx, cfg, raw)
	}
	if strings.TrimSpace(raw.AccessToken) != "" {
		return raw.AccessToken, nil
	}
	return "", apperrors.New(40113, http.StatusBadRequest, "Dropbox 令牌不可用")
}

func (p *dropboxProvider) refreshToken(ctx context.Context, cfg *storagedomain.Config, raw *storagedomain.DropboxConfig) (string, error) {
	if p.redis != nil {
		cacheKey := fmt.Sprintf("%s:storage:dropbox:token:%d", p.keyPrefix, cfg.ID)
		if cached, err := p.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
			var token oneDriveTokenCache
			if json.Unmarshal(cached, &token) == nil && token.ExpiresAt > time.Now().Unix()+30 && strings.TrimSpace(token.AccessToken) != "" {
				return token.AccessToken, nil
			}
		}
	}
	form := neturl.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", raw.RefreshToken)
	form.Set("client_id", raw.AppKey)
	form.Set("client_secret", raw.AppSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.dropboxapi.com/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("dropbox refresh status=%d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", apperrors.New(40114, http.StatusBadGateway, "Dropbox 刷新令牌失败")
	}
	if p.redis != nil {
		cache := oneDriveTokenCache{AccessToken: payload.AccessToken, ExpiresAt: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).Unix()}
		if rawBytes, err := json.Marshal(cache); err == nil {
			ttl := time.Duration(payload.ExpiresIn-60) * time.Second
			if ttl < time.Minute {
				ttl = time.Minute
			}
			_ = p.redis.Set(ctx, fmt.Sprintf("%s:storage:dropbox:token:%d", p.keyPrefix, cfg.ID), rawBytes, ttl).Err()
		}
	}
	return payload.AccessToken, nil
}

func composeDropboxWebURL(cfg *storagedomain.Config, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	return "https://www.dropbox.com/home/" + encodeObjectPath(objectKey)
}

type googleDriveProvider struct {
	redis     *redislib.Client
	keyPrefix string
}

func newGoogleDriveProvider(redis *redislib.Client, keyPrefix string) storageProvider {
	return &googleDriveProvider{redis: redis, keyPrefix: keyPrefix}
}

func (p *googleDriveProvider) Name() string { return storagedomain.ProviderGoogleDrive }

func (p *googleDriveProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	svc, _, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	about, err := svc.About.Get().Fields("user(displayName,emailAddress),storageQuota,rootFolderId").Do()
	if err != nil {
		return nil, err
	}
	return map[string]any{"drive_id": raw.DriveID, "user": about.User, "storage_quota": about.StorageQuota}, nil
}

func (p *googleDriveProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	svc, _, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	parentID, err := p.ensureParentFolders(ctx, svc, raw, path.Dir(strings.Trim(strings.TrimSpace(input.ObjectKey), "/")))
	if err != nil {
		return nil, err
	}
	fileName := path.Base(strings.Trim(strings.TrimSpace(input.ObjectKey), "/"))
	file := &drive.File{Name: fileName}
	if parentID != "" {
		file.Parents = []string{parentID}
	}
	call := svc.Files.Create(file).Media(input.Content)
	if raw.DriveID != "" {
		call = call.SupportsAllDrives(true)
	}
	saved, err := call.Fields("id,name,mimeType,size,webViewLink,webContentLink").Do()
	if err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      firstNonEmpty(raw.DriveID, "google_drive"),
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        saved.Size,
		ContentType: saved.MimeType,
		ETag:        saved.Id,
		URL:         firstNonEmpty(saved.WebContentLink, saved.WebViewLink, composeGoogleDriveWebURL(cfg, saved.Id)),
	}, nil
}

func (p *googleDriveProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	svc, _, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	file, err := p.resolveFileByPath(ctx, svc, raw, strings.Trim(strings.TrimSpace(objectKey), "/"))
	if err != nil {
		return nil, err
	}
	call := svc.Files.Get(file.Id)
	if raw.DriveID != "" {
		call = call.SupportsAllDrives(true)
	}
	resp, err := call.Download()
	if err != nil {
		return nil, err
	}
	reader := &storagedomain.ObjectReader{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		FileName:    file.Name,
		ETag:        file.Id,
	}
	if length, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil {
		reader.Size = length
	}
	return reader, nil
}

func (p *googleDriveProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	svc, _, raw, err := p.client(ctx, cfg)
	if err != nil {
		return "", err
	}
	file, err := p.resolveFileByPath(ctx, svc, raw, strings.Trim(strings.TrimSpace(objectKey), "/"))
	if err != nil {
		return "", err
	}
	perm := &drive.Permission{Type: "anyone", Role: "reader"}
	create := svc.Permissions.Create(file.Id, perm)
	if raw.DriveID != "" {
		create = create.SupportsAllDrives(true)
	}
	_, _ = create.Do()
	get := svc.Files.Get(file.Id).Fields("webViewLink,webContentLink")
	if raw.DriveID != "" {
		get = get.SupportsAllDrives(true)
	}
	detail, err := get.Do()
	if err != nil {
		return "", err
	}
	return firstNonEmpty(detail.WebContentLink, detail.WebViewLink, composeGoogleDriveWebURL(cfg, file.Id)), nil
}

func (p *googleDriveProvider) client(ctx context.Context, cfg *storagedomain.Config) (*drive.Service, *oauth2.Token, *storagedomain.GoogleDriveConfig, error) {
	raw, err := decodeGoogleDriveConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, nil, err
	}
	token, err := p.resolveToken(ctx, cfg.ID, raw)
	if err != nil {
		return nil, nil, nil, err
	}
	svc, err := drive.NewService(ctx, option.WithTokenSource(oauth2.StaticTokenSource(token)))
	if err != nil {
		return nil, nil, nil, err
	}
	return svc, token, raw, nil
}

func (p *googleDriveProvider) resolveToken(ctx context.Context, configID int64, raw *storagedomain.GoogleDriveConfig) (*oauth2.Token, error) {
	cacheKey := fmt.Sprintf("%s:storage:gdrive:token:%d", p.keyPrefix, configID)
	if p.redis != nil {
		if cached, err := p.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
			var token oauth2.Token
			if json.Unmarshal(cached, &token) == nil && token.Valid() {
				return &token, nil
			}
		}
	}
	token := &oauth2.Token{AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken, TokenType: "Bearer"}
	if strings.TrimSpace(raw.RefreshToken) != "" && strings.TrimSpace(raw.ClientID) != "" && strings.TrimSpace(raw.ClientSecret) != "" {
		cfg := &oauth2.Config{
			ClientID:     raw.ClientID,
			ClientSecret: raw.ClientSecret,
			Scopes:       []string{drive.DriveScope},
			Endpoint:     google.Endpoint,
		}
		resolved, err := cfg.TokenSource(ctx, token).Token()
		if err != nil {
			return nil, err
		}
		token = resolved
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil, apperrors.New(40115, http.StatusBadRequest, "Google Drive 令牌不可用")
	}
	if p.redis != nil {
		if rawBytes, err := json.Marshal(token); err == nil {
			ttl := time.Hour
			if !token.Expiry.IsZero() {
				ttl = time.Until(token.Expiry.Add(-time.Minute))
				if ttl < time.Minute {
					ttl = time.Minute
				}
			}
			_ = p.redis.Set(ctx, cacheKey, rawBytes, ttl).Err()
		}
	}
	return token, nil
}

func (p *googleDriveProvider) ensureParentFolders(ctx context.Context, svc *drive.Service, raw *storagedomain.GoogleDriveConfig, folderPath string) (string, error) {
	folderPath = strings.Trim(strings.TrimSpace(folderPath), "/")
	parent := strings.TrimSpace(raw.FolderID)
	if folderPath == "" || folderPath == "." {
		return parent, nil
	}
	for _, part := range strings.Split(folderPath, "/") {
		if part == "" {
			continue
		}
		found, err := p.findDriveFile(ctx, svc, raw, part, parent, "application/vnd.google-apps.folder")
		if err != nil {
			return "", err
		}
		if found == nil {
			file := &drive.File{Name: part, MimeType: "application/vnd.google-apps.folder"}
			if parent != "" {
				file.Parents = []string{parent}
			}
			create := svc.Files.Create(file).Fields("id")
			if raw.DriveID != "" {
				create = create.SupportsAllDrives(true)
			}
			created, err := create.Do()
			if err != nil {
				return "", err
			}
			parent = created.Id
			continue
		}
		parent = found.Id
	}
	return parent, nil
}

func (p *googleDriveProvider) resolveFileByPath(ctx context.Context, svc *drive.Service, raw *storagedomain.GoogleDriveConfig, objectKey string) (*drive.File, error) {
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	if objectKey == "" {
		return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	parts := strings.Split(objectKey, "/")
	parent := strings.TrimSpace(raw.FolderID)
	for index, part := range parts {
		mimeType := ""
		if index < len(parts)-1 {
			mimeType = "application/vnd.google-apps.folder"
		}
		found, err := p.findDriveFile(ctx, svc, raw, part, parent, mimeType)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
		}
		parent = found.Id
		if index == len(parts)-1 {
			return found, nil
		}
	}
	return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
}

func (p *googleDriveProvider) findDriveFile(ctx context.Context, svc *drive.Service, raw *storagedomain.GoogleDriveConfig, name string, parent string, mimeType string) (*drive.File, error) {
	queryParts := []string{"trashed = false", fmt.Sprintf("name = '%s'", strings.ReplaceAll(name, "'", "\\'"))}
	if parent != "" {
		queryParts = append(queryParts, fmt.Sprintf("'%s' in parents", parent))
	}
	if mimeType != "" {
		queryParts = append(queryParts, fmt.Sprintf("mimeType = '%s'", mimeType))
	}
	call := svc.Files.List().Q(strings.Join(queryParts, " and ")).Fields("files(id,name,mimeType,size,webViewLink,webContentLink)").PageSize(1)
	if raw.DriveID != "" {
		call = call.SupportsAllDrives(true).IncludeItemsFromAllDrives(true).Corpora("drive").DriveId(raw.DriveID)
	}
	result, err := call.Do()
	if err != nil {
		return nil, err
	}
	if len(result.Files) == 0 {
		return nil, nil
	}
	return result.Files[0], nil
}

func composeGoogleDriveWebURL(cfg *storagedomain.Config, fileID string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + "/" + neturl.PathEscape(strings.TrimSpace(fileID))
	}
	return "https://drive.google.com/file/d/" + neturl.PathEscape(strings.TrimSpace(fileID)) + "/view"
}

type azureBlobProvider struct{}

func newAzureBlobProvider() storageProvider { return &azureBlobProvider{} }

func (p *azureBlobProvider) Name() string { return storagedomain.ProviderAzureBlob }

func (p *azureBlobProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	pager := client.ServiceClient().NewListContainersPager(nil)
	count := 0
	if pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		count = len(page.ContainerItems)
	}
	return map[string]any{"container": raw.Container, "account_url": firstNonEmpty(raw.AccountURL, raw.AccountName), "visible_containers": count}, nil
}

func (p *azureBlobProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.UploadStream(ctx, raw.Container, strings.Trim(strings.TrimSpace(input.ObjectKey), "/"), input.Content, &azblob.UploadStreamOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: stringPointer(input.ContentType), BlobCacheControl: stringPointer(input.CacheControl)},
	})
	if err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      raw.Container,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		URL:         composeAzureBlobURL(cfg, raw, input.ObjectKey),
	}, nil
}

func (p *azureBlobProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.DownloadStream(ctx, raw.Container, strings.Trim(strings.TrimSpace(objectKey), "/"), nil)
	if err != nil {
		return nil, err
	}
	reader := &storagedomain.ObjectReader{
		Body:         resp.NewRetryReader(ctx, nil),
		ContentType:  stringPointerValue(resp.ContentType),
		CacheControl: stringPointerValue(resp.CacheControl),
	}
	if resp.ETag != nil {
		reader.ETag = string(*resp.ETag)
	}
	if resp.ContentLength != nil {
		reader.Size = *resp.ContentLength
	}
	return reader, nil
}

func (p *azureBlobProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeAzureBlobConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	return composeAzureBlobURL(cfg, raw, objectKey), nil
}

func (p *azureBlobProvider) client(cfg *storagedomain.Config) (*azblob.Client, *storagedomain.AzureBlobConfig, error) {
	raw, err := decodeAzureBlobConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(raw.ConnectionString) != "" {
		client, err := azblob.NewClientFromConnectionString(raw.ConnectionString, nil)
		return client, raw, err
	}
	cred, err := azblob.NewSharedKeyCredential(raw.AccountName, raw.AccountKey)
	if err != nil {
		return nil, nil, err
	}
	client, err := azblob.NewClientWithSharedKeyCredential(strings.TrimRight(strings.TrimSpace(raw.AccountURL), "/"), cred, nil)
	return client, raw, err
}

func composeAzureBlobURL(cfg *storagedomain.Config, raw *storagedomain.AzureBlobConfig, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	base := strings.TrimRight(strings.TrimSpace(raw.AccountURL), "/")
	if base == "" && strings.TrimSpace(raw.AccountName) != "" {
		base = "https://" + raw.AccountName + ".blob.core.windows.net"
	}
	return strings.TrimRight(base, "/") + "/" + neturl.PathEscape(strings.TrimSpace(raw.Container)) + "/" + encodeObjectPath(objectKey)
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
