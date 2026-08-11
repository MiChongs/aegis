package httptransport

import (
	appdomain "aegis/internal/domain/app"
	"aegis/internal/domain/email"
)

type AdminEmailConfigListRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

type AdminEmailConfigDetailRequest struct {
	AppID    int64 `json:"appid" form:"appid" binding:"required"`
	ConfigID int64 `json:"config_id" form:"config_id" binding:"required"`
}

type AdminEmailConfigSaveRequest struct {
	AppID        int64             `json:"appid" form:"appid" binding:"required"`
	ConfigID     int64             `json:"config_id" form:"config_id"`
	Name         string            `json:"config_name" form:"config_name"`
	Provider     string            `json:"provider" form:"provider"`
	Enabled      *bool             `json:"enabled"`
	IsDefault    *bool             `json:"is_default"`
	Description  string            `json:"description" form:"description"`
	SMTPHost     string            `json:"smtp_host" form:"smtp_host"`
	SMTPPort     int               `json:"smtp_port" form:"smtp_port"`
	SMTPUser     string            `json:"smtp_user" form:"smtp_user"`
	SMTPPassword string            `json:"smtp_password" form:"smtp_password"`
	SMTPFrom     string            `json:"smtp_from" form:"smtp_from"`
	SMTPFromName string            `json:"smtp_from_name" form:"smtp_from_name"`
	SMTPReplyTo  string            `json:"smtp_reply_to" form:"smtp_reply_to"`
	SMTPTLS      *bool             `json:"smtp_tls"`
	SMTPInsecure *bool             `json:"smtp_insecure_skip_verify"`
	SMTP         *email.SMTPConfig `json:"smtp"`

	// Zeabur Email（provider=zeabur）。API Key 与 Webhook 密钥留空表示「不修改」。
	ZeaburAPIKey        string              `json:"zeabur_api_key" form:"zeabur_api_key"`
	ZeaburBaseURL       string              `json:"zeabur_base_url" form:"zeabur_base_url"`
	ZeaburFrom          string              `json:"zeabur_from" form:"zeabur_from"`
	ZeaburFromName      string              `json:"zeabur_from_name" form:"zeabur_from_name"`
	ZeaburReplyTo       string              `json:"zeabur_reply_to" form:"zeabur_reply_to"`
	ZeaburWebhookSecret string              `json:"zeabur_webhook_secret" form:"zeabur_webhook_secret"`
	ZeaburTags          map[string]string   `json:"zeabur_tags"`
	Zeabur              *email.ZeaburConfig `json:"zeabur"`
}

type AdminEmailConfigTestRequest struct {
	AppID     int64  `json:"appid" form:"appid" binding:"required"`
	ConfigID  int64  `json:"config_id" form:"config_id" binding:"required"`
	TestEmail string `json:"test_email" form:"test_email" binding:"required"`
}

type AdminEmailDeliveryListRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	ConfigID int64  `json:"config_id" form:"config_id"`
	Status   string `json:"status" form:"status"`
	Provider string `json:"provider" form:"provider"`
	Keyword  string `json:"keyword" form:"keyword"`
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"pageSize" form:"pageSize"`
}

type EmailCodeRequest struct {
	AppID         int64  `json:"appid" form:"appid" binding:"required"`
	Email         string `json:"email" form:"email" binding:"required"`
	Purpose       string `json:"purpose" form:"purpose"`
	ExpireMinutes int    `json:"expireMinutes" form:"expireMinutes"`
	ConfigName    string `json:"config_name" form:"config_name"`
}

type EmailVerifyRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	Email   string `json:"email" form:"email" binding:"required"`
	Code    string `json:"code" form:"code" binding:"required"`
	Purpose string `json:"purpose" form:"purpose"`
}

type EmailResetRequest struct {
	AppID      int64  `json:"appid" form:"appid" binding:"required"`
	Email      string `json:"email" form:"email" binding:"required"`
	ResetURL   string `json:"resetUrl" form:"resetUrl"`
	ConfigName string `json:"config_name" form:"config_name"`
}

type EmailVerifyResetRequest struct {
	AppID int64  `json:"appid" form:"appid" binding:"required"`
	Email string `json:"email" form:"email" binding:"required"`
	Token string `json:"token" form:"token" binding:"required"`
}

type AdminPaymentConfigListRequest struct {
	AppID         int64  `json:"appid" form:"appid" binding:"required"`
	PaymentMethod string `json:"payment_method" form:"payment_method"`
	EnabledOnly   bool   `json:"enabled_only" form:"enabled_only"`
}

type AdminPaymentConfigDetailRequest struct {
	AppID    int64 `json:"appid" form:"appid" binding:"required"`
	ConfigID int64 `json:"config_id" form:"config_id" binding:"required"`
}

type AdminPaymentConfigSaveRequest struct {
	AppID         int64          `json:"appid" form:"appid" binding:"required"`
	ConfigID      int64          `json:"config_id" form:"config_id"`
	PaymentMethod string         `json:"payment_method" form:"payment_method"`
	ConfigName    string         `json:"config_name" form:"config_name"`
	ConfigData    map[string]any `json:"config_data"`
	Enabled       *bool          `json:"enabled"`
	IsDefault     *bool          `json:"is_default"`
	Description   string         `json:"description" form:"description"`
}

type AdminPaymentConfigTestRequest struct {
	AppID    int64 `json:"appid" form:"appid" binding:"required"`
	ConfigID int64 `json:"config_id" form:"config_id" binding:"required"`
}

type AdminPaymentInitEpayRequest struct {
	AppID      int64          `json:"appid" form:"appid" binding:"required"`
	EpayConfig map[string]any `json:"epay_config" binding:"required"`
}

type CreatePaymentOrderRequest struct {
	Subject    string         `json:"subject" form:"subject" binding:"required"`
	Body       string         `json:"body" form:"body"`
	Amount     string         `json:"amount" form:"amount" binding:"required"`
	Type       string         `json:"type" form:"type"`
	ConfigName string         `json:"config_name" form:"config_name"`
	NotifyURL  string         `json:"notify_url" form:"notify_url"`
	ReturnURL  string         `json:"return_url" form:"return_url"`
	Metadata   map[string]any `json:"metadata"`
}

type UserPaymentOrdersQuery struct {
	Status string `json:"status" form:"status"`
	Page   int    `json:"page" form:"page"`
	Limit  int    `json:"limit" form:"limit"`
}

// PaymentBillExportRequest 凭证导出请求。
//
// locale 留空时按「用户设置的语言 → Accept-Language → 平台默认（en）」依次决定，
// 因此绝大多数客户端不必传这个字段。
type PaymentBillExportRequest struct {
	ExpireMinutes int `json:"expireMinutes" form:"expireMinutes"`
	// Locale 期望语言（BCP 47，如 zh-Hans / ja / pt-BR）
	Locale string `json:"locale" form:"locale"`
	// DocumentType 凭证类型：receipt / invoice / credit_note；留空按订单状态推导
	DocumentType string `json:"documentType" form:"documentType"`
	// Timezone IANA 时区名（如 Asia/Shanghai）；留空按用户设置，再空则 UTC
	Timezone string `json:"timezone" form:"timezone"`
}

type WorkflowListRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Status   string `json:"status" form:"status"`
	Category string `json:"category" form:"category"`
	Keyword  string `json:"keyword" form:"keyword"`
	Page     int    `json:"page" form:"page"`
	Limit    int    `json:"limit" form:"limit"`
}

type WorkflowDetailRequest struct {
	AppID      int64 `json:"appid" form:"appid" binding:"required"`
	WorkflowID int64 `json:"workflow_id" form:"workflow_id" binding:"required"`
}

type WorkflowSaveRequest struct {
	AppID         int64          `json:"appid" form:"appid" binding:"required"`
	WorkflowID    int64          `json:"workflow_id" form:"workflow_id"`
	Name          string         `json:"name" form:"name"`
	Description   string         `json:"description" form:"description"`
	Category      string         `json:"category" form:"category"`
	Status        string         `json:"status" form:"status"`
	Definition    map[string]any `json:"definition"`
	TriggerConfig map[string]any `json:"trigger_config"`
	UIConfig      map[string]any `json:"ui_config"`
	Permissions   map[string]any `json:"permissions"`
}

type WorkflowStartRequest struct {
	AppID        int64          `json:"appid" form:"appid" binding:"required"`
	WorkflowID   int64          `json:"workflow_id" form:"workflow_id"`
	WorkflowID2  int64          `json:"workflowId" form:"workflowId"`
	InputData    map[string]any `json:"input_data"`
	InstanceName string         `json:"instance_name" form:"instance_name"`
	Priority     int            `json:"priority" form:"priority"`
}

type WorkflowInstancesRequest struct {
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	WorkflowID  int64  `json:"workflow_id" form:"workflow_id"`
	WorkflowID2 int64  `json:"workflowId" form:"workflowId"`
	Status      string `json:"status" form:"status"`
	Page        int    `json:"page" form:"page"`
	Limit       int    `json:"limit" form:"limit"`
}

type WorkflowInstanceDetailRequest struct {
	AppID       int64 `json:"appid" form:"appid" binding:"required"`
	InstanceID  int64 `json:"instance_id" form:"instance_id"`
	InstanceID2 int64 `json:"instanceId" form:"instanceId"`
}

type WorkflowTaskQueryRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	UserID   int64  `json:"user_id" form:"user_id"`
	Status   string `json:"status" form:"status"`
	Priority int    `json:"priority" form:"priority"`
	Page     int    `json:"page" form:"page"`
	Limit    int    `json:"limit" form:"limit"`
}

type WorkflowTaskDetailRequest struct {
	AppID  int64 `json:"appid" form:"appid" binding:"required"`
	TaskID int64 `json:"task_id" form:"task_id" binding:"required"`
}

type WorkflowTaskCompleteRequest struct {
	AppID   int64          `json:"appid" form:"appid" binding:"required"`
	TaskID  int64          `json:"task_id" form:"task_id"`
	TaskID2 int64          `json:"taskId" form:"taskId"`
	Output  map[string]any `json:"output_data"`
	Comment string         `json:"comment" form:"comment"`
}

type WorkflowTaskAssignRequest struct {
	AppID      int64  `json:"appid" form:"appid" binding:"required"`
	TaskID     int64  `json:"task_id" form:"task_id" binding:"required"`
	AssignedTo int64  `json:"assigned_to" form:"assigned_to" binding:"required"`
	Comment    string `json:"comment" form:"comment"`
}

type WorkflowTemplatesRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	Category string `json:"category" form:"category"`
	Page     int    `json:"page" form:"page"`
	Limit    int    `json:"limit" form:"limit"`
}

type WorkflowCreateFromTemplateRequest struct {
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	TemplateID  int64  `json:"template_id" form:"template_id" binding:"required"`
	Name        string `json:"name" form:"name" binding:"required"`
	Description string `json:"description" form:"description"`
}

type WorkflowSaveAsTemplateRequest struct {
	AppID               int64  `json:"appid" form:"appid" binding:"required"`
	WorkflowID          int64  `json:"workflow_id" form:"workflow_id" binding:"required"`
	TemplateName        string `json:"template_name" form:"template_name" binding:"required"`
	TemplateDescription string `json:"template_description" form:"template_description"`
	Category            string `json:"category" form:"category"`
	IsPublic            bool   `json:"is_public" form:"is_public"`
}

type WorkflowValidateRequest struct {
	AppID      int64          `json:"appid" form:"appid" binding:"required"`
	Definition map[string]any `json:"definition" binding:"required"`
}

type WorkflowLogsRequest struct {
	AppID      int64 `json:"appid" form:"appid" binding:"required"`
	WorkflowID int64 `json:"workflow_id" form:"workflow_id"`
	InstanceID int64 `json:"instance_id" form:"instance_id"`
	Limit      int   `json:"limit" form:"limit"`
}

// ─────────────────────────────────────────────────────────────────────
// 由匿名请求体提升出来的具名类型
//
// 写成 `var req struct{...}` 的请求体在 OpenAPI 里只能是一个空对象，
// 生成式客户端（Java/Kotlin/Swift/Dart）拿到的就是没有参数的方法。
// 提成具名类型是让它们进 schema 的唯一办法，行为完全不变。
// ─────────────────────────────────────────────────────────────────────

// VersionChannelUsersRequest 版本渠道的用户名单增删。
type VersionChannelUsersRequest struct {
	UserIDs []int64 `json:"user_ids" binding:"required"`
}

// AdminSessionRevokeBatchRequest 按会话指纹批量踢出。
type AdminSessionRevokeBatchRequest struct {
	TokenHashes []string `json:"tokenHashes" binding:"required"`
}

// AdminPasswordPolicyUpdateRequest 更新应用密码策略。
type AdminPasswordPolicyUpdateRequest struct {
	Policy appdomain.PasswordPolicy `json:"policy" binding:"required"`
}

// AdminPasswordPolicyTestRequest 用一个候选密码试跑当前策略。
type AdminPasswordPolicyTestRequest struct {
	Password string `json:"password" binding:"required"`
}

// AppIntegrationSelfTestRequest 接入自检的目标地址；留空时回落到本次请求的 scheme+host。
type AppIntegrationSelfTestRequest struct {
	BaseURL string `json:"baseUrl"`
}
