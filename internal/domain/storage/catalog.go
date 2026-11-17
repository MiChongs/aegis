package storage

// 存储提供商的**自描述**目录：与邮件服务商、支付渠道同一套做法。
//
// 一份目录同时驱动控制台的「添加存储」卡片与动态配置表单、服务端的密钥抹除与
// 「留空即不修改」。新增一家提供商只需在 Go 侧补实现 + 一条目录，控制台零改动。

// 字段类型。
const (
	FieldText   = "text"
	FieldSecret = "secret" // 出网抹除、提交留空表示不修改
	FieldURL    = "url"
	FieldSwitch = "switch"
	FieldSelect = "select"
)

// 提供商分组。
const (
	CategoryS3       = "s3"
	CategoryChina    = "china"
	CategoryDrive    = "drive"
	CategorySelfHost = "self_host"
)

// CategoryNames 分组标识 → 中文名。
var CategoryNames = map[string]string{
	CategoryS3:       "S3 兼容对象存储",
	CategoryChina:    "国内云厂商",
	CategoryDrive:    "网盘",
	CategorySelfHost: "自建",
}

// FieldOption 下拉选项。
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConfigField 一个配置项。
type ConfigField struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        string        `json:"type"`
	Required    bool          `json:"required,omitempty"`
	Secret      bool          `json:"secret,omitempty"`
	Placeholder string        `json:"placeholder,omitempty"`
	Help        string        `json:"help,omitempty"`
	Default     any           `json:"default,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
	Advanced    bool          `json:"advanced,omitempty"`
}

// ProviderMeta 一家存储提供商的完整描述。
type ProviderMeta struct {
	Provider     string             `json:"provider"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Category     string             `json:"category"`
	CategoryName string             `json:"categoryName"`
	Icon         string             `json:"icon,omitempty"` // Simple Icons slug，未收录为空
	BrandColor   string             `json:"brandColor,omitempty"`
	DocURL       string             `json:"docUrl,omitempty"`
	Featured     bool               `json:"featured,omitempty"`
	GlobalOnly   bool               `json:"globalOnly,omitempty"`
	Capabilities ConfigCapabilities `json:"capabilities"`
	// PublicBaseURLRequired 公开直链必须配公开访问域名（S3 端点本身不对外，如 R2）。
	PublicBaseURLRequired bool          `json:"publicBaseUrlRequired,omitempty"`
	BaseURLLabel          string        `json:"baseUrlLabel,omitempty"`
	BaseURLPlaceholder    string        `json:"baseUrlPlaceholder,omitempty"`
	Fields                []ConfigField `json:"fields"`
	Notes                 []string      `json:"notes,omitempty"`
}

// SecretKeys 声明为密钥的字段键。
func (m ProviderMeta) SecretKeys() []string {
	keys := make([]string, 0, 4)
	for _, field := range m.Fields {
		if field.Secret {
			keys = append(keys, field.Key)
		}
	}
	return keys
}

func text(key, label, placeholder string, required bool) ConfigField {
	return ConfigField{Key: key, Label: label, Type: FieldText, Placeholder: placeholder, Required: required}
}

func secret(key, label, placeholder string, required bool) ConfigField {
	return ConfigField{Key: key, Label: label, Type: FieldSecret, Secret: true, Placeholder: placeholder, Required: required}
}

func advanced(field ConfigField) ConfigField {
	field.Advanced = true
	return field
}

func help(field ConfigField, message string) ConfigField {
	field.Help = message
	return field
}

func toggle(key, label string, def bool) ConfigField {
	return ConfigField{Key: key, Label: label, Type: FieldSwitch, Default: def}
}

// Catalog 全部提供商，按控制台展示顺序排列。
func Catalog() []ProviderMeta {
	items := []ProviderMeta{
		{
			Provider: ProviderR2, Name: "Cloudflare R2", Category: CategoryS3, Icon: "cloudflare", BrandColor: "#F38020",
			Description: "S3 兼容，出网流量免费", DocURL: "https://developers.cloudflare.com/r2/", Featured: true,
			Capabilities:          ConfigCapabilities{DirectUpload: true, Delete: true, CORS: true, BucketUsage: true},
			PublicBaseURLRequired: true, BaseURLLabel: "公开访问域名", BaseURLPlaceholder: "https://files.example.com",
			Fields: []ConfigField{
				help(text("account_id", "账户 ID", "32 位十六进制", true), "Cloudflare 控制台 R2 概览页"),
				text("bucket", "存储桶", "aegis", true),
				text("access_key_id", "Access Key ID", "", true),
				secret("secret_access_key", "Secret Access Key", "", true),
				{Key: "jurisdiction", Label: "管辖区", Type: FieldSelect, Default: "default", Options: []FieldOption{
					{Value: "default", Label: "默认"}, {Value: "eu", Label: "欧盟"}, {Value: "fedramp", Label: "FedRAMP"},
				}},
				advanced(help(secret("api_token", "API 令牌", "可选", false), "需 Account Analytics 读权限，用于读取桶的真实用量")),
			},
		},
		{
			Provider: ProviderS3, Name: "S3 兼容存储", Category: CategoryS3, Description: "Amazon S3 及任意 S3 兼容端点",
			Capabilities: ConfigCapabilities{DirectUpload: true, Delete: true, CORS: true},
			BaseURLLabel: "公开访问域名", BaseURLPlaceholder: "https://cdn.example.com",
			Fields: []ConfigField{
				{Key: "endpoint", Label: "Endpoint", Type: FieldURL, Placeholder: "https://s3.amazonaws.com", Required: true},
				text("region", "Region", "us-east-1", true),
				text("bucket", "存储桶", "my-bucket", true),
				text("access_key_id", "Access Key ID", "", true),
				secret("secret_access_key", "Secret Access Key", "", true),
				advanced(secret("session_token", "Session Token", "可选", false)),
				advanced(toggle("force_path_style", "路径风格访问", false)),
				advanced(toggle("use_ssl", "使用 SSL", true)),
			},
		},
		{
			// 统一栈（deploy/swarm）自带的对象存储。Simple Icons 未收录 RustFS，图标走自建类的中性图标
			Provider: ProviderRustFS, Name: "RustFS", Category: CategorySelfHost, Icon: "rustfs",
			Description: "自建 S3 兼容对象存储，集群部署自带",
			Capabilities: ConfigCapabilities{DirectUpload: true, Delete: true},
			BaseURLLabel: "公开访问域名",
			Fields: []ConfigField{
				text("endpoint", "Endpoint", "aegis-rustfs:9000", true),
				text("bucket", "存储桶", "aegis", true),
				text("access_key_id", "Access Key", "", true),
				secret("secret_access_key", "Secret Key", "", true),
				toggle("use_ssl", "使用 SSL", false),
				advanced(text("region", "Region", "us-east-1", false)),
				advanced(toggle("auto_create_bucket", "自动创建存储桶", true)),
			},
		},
		{
			Provider: ProviderMinIO, Name: "MinIO", Category: CategorySelfHost, Icon: "minio", BrandColor: "#C72E49",
			Description: "自建 S3 兼容对象存储",
			Capabilities: ConfigCapabilities{DirectUpload: true, Delete: true},
			BaseURLLabel: "公开访问域名",
			Fields: []ConfigField{
				text("endpoint", "Endpoint", "minio.example.com:9000", true),
				text("bucket", "存储桶", "aegis", true),
				text("access_key_id", "Access Key", "", true),
				secret("secret_access_key", "Secret Key", "", true),
				toggle("use_ssl", "使用 SSL", true),
				advanced(text("region", "Region", "us-east-1", false)),
				advanced(toggle("auto_create_bucket", "自动创建存储桶", false)),
			},
		},
		{
			Provider: ProviderAliyunOSS, Name: "阿里云 OSS", Category: CategoryChina, Icon: "alibabacloud", BrandColor: "#FF6A00",
			Capabilities: ConfigCapabilities{DirectUpload: true, Delete: true},
			BaseURLLabel: "公开访问域名",
			Fields: []ConfigField{
				text("endpoint", "Endpoint", "oss-cn-hangzhou.aliyuncs.com", true),
				text("bucket", "存储桶", "my-bucket", true),
				text("access_key_id", "AccessKey ID", "", true),
				secret("access_key_secret", "AccessKey Secret", "", true),
			},
		},
		{
			Provider: ProviderTencentCOS, Name: "腾讯云 COS", Category: CategoryChina, BrandColor: "#006EFF",
			Capabilities: ConfigCapabilities{DirectUpload: true, Delete: true},
			BaseURLLabel: "公开访问域名",
			Fields: []ConfigField{
				{Key: "bucket_url", Label: "存储桶地址", Type: FieldURL, Placeholder: "https://bucket-125000.cos.ap-guangzhou.myqcloud.com", Required: true},
				text("secret_id", "SecretId", "", true),
				secret("secret_key", "SecretKey", "", true),
				advanced(text("endpoint", "Endpoint", "cos.ap-guangzhou.myqcloud.com", false)),
			},
		},
		{
			Provider: ProviderQiniuKodo, Name: "七牛云 Kodo", Category: CategoryChina, BrandColor: "#07BEFF",
			Capabilities: ConfigCapabilities{Delete: true},
			BaseURLLabel: "公开访问域名",
			Fields: []ConfigField{
				{Key: "region", Label: "区域", Type: FieldSelect, Default: "z0", Required: true, Options: []FieldOption{
					{Value: "z0", Label: "华东"}, {Value: "z1", Label: "华北"}, {Value: "z2", Label: "华南"},
					{Value: "na0", Label: "北美"}, {Value: "as0", Label: "东南亚"},
				}},
				text("bucket", "存储桶", "my-bucket", true),
				text("access_key", "AccessKey", "", true),
				secret("secret_key", "SecretKey", "", true),
				text("domain", "下载域名", "cdn.example.com", true),
				toggle("use_https", "使用 HTTPS", true),
			},
		},
		{
			Provider: ProviderAzureBlob, Name: "Azure Blob", Category: CategoryS3, BrandColor: "#0078D4",
			Capabilities: ConfigCapabilities{Delete: true},
			Fields: []ConfigField{
				text("container", "容器", "aegis", true),
				{Key: "account_url", Label: "账户地址", Type: FieldURL, Placeholder: "https://account.blob.core.windows.net"},
				text("account_name", "账户名", "", false),
				secret("account_key", "账户密钥", "", false),
				advanced(secret("connection_string", "连接字符串", "", false)),
			},
		},
		{
			Provider: ProviderWebDAV, Name: "WebDAV", Category: CategorySelfHost, Description: "Nextcloud、群晖等",
			Capabilities: ConfigCapabilities{Delete: true},
			Fields: []ConfigField{
				{Key: "endpoint", Label: "地址", Type: FieldURL, Placeholder: "https://dav.example.com/remote.php/webdav/", Required: true},
				text("username", "用户名", "", false),
				secret("password", "密码", "", false),
			},
		},
		{
			Provider: ProviderOneDrive, Name: "OneDrive", Category: CategoryDrive, BrandColor: "#0078D4",
			Capabilities: ConfigCapabilities{Delete: true},
			Fields: []ConfigField{
				text("drive_id", "Drive ID", "", true),
				secret("refresh_token", "Refresh Token", "", false),
				secret("access_token", "Access Token", "", false),
				text("client_id", "Client ID", "", false),
				secret("client_secret", "Client Secret", "", false),
				advanced(text("tenant_id", "Tenant ID", "common", false)),
			},
		},
		{
			Provider: ProviderDropbox, Name: "Dropbox", Category: CategoryDrive, Icon: "dropbox", BrandColor: "#0061FF",
			Capabilities: ConfigCapabilities{Delete: true},
			Fields: []ConfigField{
				secret("refresh_token", "Refresh Token", "", false),
				secret("access_token", "Access Token", "", false),
				text("app_key", "App Key", "", false),
				secret("app_secret", "App Secret", "", false),
			},
		},
		{
			Provider: ProviderGoogleDrive, Name: "Google Drive", Category: CategoryDrive, Icon: "googledrive", BrandColor: "#4285F4",
			Capabilities: ConfigCapabilities{Delete: true},
			Fields: []ConfigField{
				secret("refresh_token", "Refresh Token", "", false),
				secret("access_token", "Access Token", "", false),
				text("client_id", "Client ID", "", false),
				secret("client_secret", "Client Secret", "", false),
				advanced(text("drive_id", "共享云端硬盘 ID", "", false)),
				advanced(text("folder_id", "文件夹 ID", "", false)),
			},
		},
		{
			Provider: ProviderLocal, Name: "本地磁盘", Category: CategorySelfHost, Description: "服务器本机目录",
			GlobalOnly: true, Capabilities: ConfigCapabilities{Delete: true},
			Fields: []ConfigField{
				text("root_dir", "存储目录", "data/storage", false),
			},
			Notes: []string{"容器部署时该目录必须挂持久卷，否则重新部署会清空"},
		},
	}
	for i := range items {
		items[i].CategoryName = CategoryNames[items[i].Category]
	}
	return items
}

// ProviderByName 按标识取目录项。
func ProviderByName(provider string) (ProviderMeta, bool) {
	for _, item := range Catalog() {
		if item.Provider == provider {
			return item, true
		}
	}
	return ProviderMeta{}, false
}

// RedactConfig 出网前抹掉密钥字段的值，另给出哪些密钥已配置。
func RedactConfig(cfg Config) Config {
	meta, ok := ProviderByName(cfg.Provider)
	if !ok {
		return cfg
	}
	redacted := make(map[string]any, len(cfg.ConfigData))
	for key, value := range cfg.ConfigData {
		redacted[key] = value
	}
	set := []string{}
	for _, key := range meta.SecretKeys() {
		if text, ok := redacted[key].(string); ok && text != "" {
			set = append(set, key)
		}
		delete(redacted, key)
	}
	cfg.ConfigData = redacted
	cfg.SecretsSet = set
	return cfg
}

// MergeSecrets 保存时密钥字段留空表示不修改：从现有配置里补回。
func MergeSecrets(provider string, incoming, existing map[string]any) map[string]any {
	meta, ok := ProviderByName(provider)
	if !ok || existing == nil {
		return incoming
	}
	merged := make(map[string]any, len(incoming))
	for key, value := range incoming {
		merged[key] = value
	}
	for _, key := range meta.SecretKeys() {
		if text, _ := merged[key].(string); text == "" {
			if old, ok := existing[key]; ok {
				merged[key] = old
			}
		}
	}
	return merged
}
