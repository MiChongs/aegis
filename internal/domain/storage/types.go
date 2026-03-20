package storage

import (
	"io"
	"time"
)

const (
	ScopeGlobal = "global"
	ScopeApp    = "app"

	AccessPublic  = "public"
	AccessPrivate = "private"

	ProviderS3          = "s3"
	ProviderAliyunOSS   = "aliyun_oss"
	ProviderTencentCOS  = "tencent_cos"
	ProviderQiniuKodo   = "qiniu_kodo"
	ProviderWebDAV      = "webdav"
	ProviderOneDrive    = "onedrive"
	ProviderDropbox     = "dropbox"
	ProviderGoogleDrive = "google_drive"
	ProviderAzureBlob   = "azure_blob"
)

type Config struct {
	ID            int64          `json:"id"`
	Scope         string         `json:"scope"`
	AppID         *int64         `json:"appid,omitempty"`
	Provider      string         `json:"provider"`
	ConfigName    string         `json:"config_name"`
	AccessMode    string         `json:"access_mode"`
	Enabled       bool           `json:"enabled"`
	IsDefault     bool           `json:"is_default"`
	ProxyDownload bool           `json:"proxy_download"`
	BaseURL       string         `json:"base_url,omitempty"`
	RootPath      string         `json:"root_path,omitempty"`
	Description   string         `json:"description,omitempty"`
	ConfigData    map[string]any `json:"config_data"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type ConfigMutation struct {
	ID            int64
	Scope         string
	AppID         *int64
	Provider      *string
	ConfigName    *string
	AccessMode    *string
	Enabled       *bool
	IsDefault     *bool
	ProxyDownload *bool
	BaseURL       *string
	RootPath      *string
	Description   *string
	ConfigData    map[string]any
}

type ListQuery struct {
	Scope    string
	AppID    *int64
	Provider string
}

type ResolveOptions struct {
	AppID      int64
	ConfigName string
	Provider   string
}

type TestResult struct {
	Success  bool           `json:"success"`
	Provider string         `json:"provider"`
	Scope    string         `json:"scope"`
	AppID    *int64         `json:"appid,omitempty"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type UploadInput struct {
	AppID         int64
	ConfigName    string
	ObjectKey     string
	FileName      string
	ContentType   string
	CacheControl  string
	ContentLength int64
	Metadata      map[string]string
	Content       io.Reader
}

type StoredObject struct {
	ConfigID      int64          `json:"config_id"`
	Provider      string         `json:"provider"`
	Bucket        string         `json:"bucket,omitempty"`
	Key           string         `json:"key"`
	FileName      string         `json:"file_name,omitempty"`
	Size          int64          `json:"size"`
	ContentType   string         `json:"content_type,omitempty"`
	ETag          string         `json:"etag,omitempty"`
	URL           string         `json:"url,omitempty"`
	AccessMode    string         `json:"access_mode"`
	ProxyRequired bool           `json:"proxy_required"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type LinkRequest struct {
	AppID      int64
	ConfigName string
	ObjectKey  string
	Download   bool
	FileName   string
	ExpiresIn  time.Duration
}

type LinkResult struct {
	ConfigID      int64     `json:"config_id"`
	Provider      string    `json:"provider"`
	Key           string    `json:"key"`
	URL           string    `json:"url"`
	AccessMode    string    `json:"access_mode"`
	ProxyRequired bool      `json:"proxy_required"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ProxyTicket struct {
	AppID      int64     `json:"appid"`
	ConfigID   int64     `json:"config_id"`
	ObjectKey  string    `json:"object_key"`
	Download   bool      `json:"download"`
	FileName   string    `json:"file_name,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	IssuedAt   time.Time `json:"issued_at"`
	Provider   string    `json:"provider"`
	AccessMode string    `json:"access_mode"`
}

type ObjectReader struct {
	Body         io.ReadCloser
	Size         int64
	ContentType  string
	FileName     string
	ETag         string
	CacheControl string
	LastModified *time.Time
}

type S3Config struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
	UseSSL          bool   `json:"use_ssl"`
	ForcePathStyle  bool   `json:"force_path_style"`
}

type AliyunOSSConfig struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

type TencentCOSConfig struct {
	Endpoint  string `json:"endpoint"`
	BucketURL string `json:"bucket_url"`
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
}

type QiniuKodoConfig struct {
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Domain    string `json:"domain"`
	UseHTTPS  bool   `json:"use_https"`
}

type WebDAVConfig struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type OneDriveConfig struct {
	DriveID      string `json:"drive_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	TenantID     string `json:"tenant_id,omitempty"`
}

type DropboxConfig struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AppKey       string `json:"app_key,omitempty"`
	AppSecret    string `json:"app_secret,omitempty"`
}

type GoogleDriveConfig struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	DriveID      string `json:"drive_id,omitempty"`
	FolderID     string `json:"folder_id,omitempty"`
}

type AzureBlobConfig struct {
	AccountURL       string `json:"account_url,omitempty"`
	ConnectionString string `json:"connection_string,omitempty"`
	AccountName      string `json:"account_name,omitempty"`
	AccountKey       string `json:"account_key,omitempty"`
	Container        string `json:"container"`
}
