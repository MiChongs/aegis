package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	captchadomain "aegis/internal/domain/captcha"
	"aegis/pkg/circuitbreaker"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	dysmsclient "github.com/alibabacloud-go/dysmsapi-20170525/v5/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
	"github.com/go-resty/resty/v2"
)

// SMSProvider 短信服务商统一接口
type SMSProvider interface {
	// Send 发送短信验证码，返回请求 ID
	Send(ctx context.Context, phone string, code string, cfg *captchadomain.SMSProviderConfig) (string, error)
}

// ────────────────────── 阿里云短信 ──────────────────────

// AliyunSMSProvider 阿里云短信服务
type AliyunSMSProvider struct {
	httpClient *resty.Client
}

// NewAliyunSMSProvider 创建阿里云短信提供商
func NewAliyunSMSProvider() *AliyunSMSProvider {
	return &AliyunSMSProvider{
		httpClient: resty.New().SetTimeout(timeutil.Seconds(10)).SetRetryCount(0),
	}
}

// Send 通过阿里云 SMS API 发送短信
func (p *AliyunSMSProvider) Send(ctx context.Context, phone string, code string, cfg *captchadomain.SMSProviderConfig) (string, error) {
	client, err := createAliyunSMSClient(cfg)
	if err != nil {
		return "", fmt.Errorf("阿里云短信客户端初始化失败: %w", err)
	}
	templateParam, err := json.Marshal(map[string]string{
		resolveSMSCodeParamKey(cfg.CodeParamKey): code,
	})
	if err != nil {
		return "", fmt.Errorf("阿里云短信模板参数序列化失败: %w", err)
	}

	request := &dysmsclient.SendSmsRequest{
		PhoneNumbers:  tea.String(phone),
		SignName:      tea.String(cfg.SignName),
		TemplateCode:  tea.String(cfg.TemplateID),
		TemplateParam: tea.String(string(templateParam)),
	}
	runtime := &dara.RuntimeOptions{}
	breakerName := circuitbreaker.Name("sms", string(cfg.Provider), fmt.Sprintf("app-%d", cfg.AppID), fmt.Sprintf("config-%d", cfg.ID))
	resp, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(10),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(200),
		MaxBackoff:  timeutil.Seconds(1),
		RatePerSec:  10,
		Burst:       20,
	}, func(callCtx context.Context) (*dysmsclient.SendSmsResponse, error) {
		return client.SendSmsWithOptions(request, runtime)
	})
	if err != nil {
		return "", fmt.Errorf("阿里云短信请求失败: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return "", fmt.Errorf("阿里云短信响应为空")
	}
	if tea.StringValue(resp.Body.Code) != "OK" {
		return "", fmt.Errorf("阿里云短信发送失败: %s - %s", tea.StringValue(resp.Body.Code), tea.StringValue(resp.Body.Message))
	}
	return tea.StringValue(resp.Body.RequestId), nil
}

// ────────────────────── 腾讯云短信 ──────────────────────

// TencentSMSProvider 腾讯云短信服务
type TencentSMSProvider struct {
	httpClient *resty.Client
}

// NewTencentSMSProvider 创建腾讯云短信提供商
func NewTencentSMSProvider() *TencentSMSProvider {
	return &TencentSMSProvider{
		httpClient: resty.New().SetTimeout(timeutil.Seconds(10)).SetRetryCount(0),
	}
}

// Send 通过腾讯云 SMS API 发送短信
func (p *TencentSMSProvider) Send(ctx context.Context, phone string, code string, cfg *captchadomain.SMSProviderConfig) (string, error) {
	// 腾讯云 SMS TC3-HMAC-SHA256 签名
	timestamp := timeutil.NowUTC().Unix()
	region := normalizeRegion(cfg.Region, "ap-guangzhou")

	// 请求体
	body := map[string]any{
		"SmsSdkAppId": cfg.SDKAppID,
		"SignName":    cfg.SignName,
		"TemplateId":  cfg.TemplateID,
		"PhoneNumberSet": []string{
			"+86" + phone,
		},
		"TemplateParamSet": []string{code},
	}
	bodyBytes, _ := json.Marshal(body)

	// 计算签名
	authorization := tencentSign(cfg.AccessKey, cfg.SecretKey, region, string(bodyBytes), timestamp)

	breakerName := circuitbreaker.Name("sms", string(cfg.Provider), fmt.Sprintf("app-%d", cfg.AppID), fmt.Sprintf("config-%d", cfg.ID))
	resp, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(10),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(200),
		MaxBackoff:  timeutil.Seconds(1),
		RatePerSec:  10,
		Burst:       20,
	}, func(callCtx context.Context) (*resty.Response, error) {
		return p.httpClient.R().
			SetContext(callCtx).
			SetHeader("Content-Type", "application/json").
			SetHeader("Authorization", authorization).
			SetHeader("Host", "sms.tencentcloudapi.com").
			SetHeader("X-TC-Action", "SendSms").
			SetHeader("X-TC-Timestamp", fmt.Sprintf("%d", timestamp)).
			SetHeader("X-TC-Version", "2021-01-11").
			SetHeader("X-TC-Region", region).
			SetBody(bodyBytes).
			Post("https://sms.tencentcloudapi.com/")
	})
	if err != nil {
		return "", fmt.Errorf("腾讯云短信请求失败: %w", err)
	}

	var result struct {
		Response struct {
			RequestID     string `json:"RequestId"`
			SendStatusSet []struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"SendStatusSet"`
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("腾讯云短信响应解析失败: %w", err)
	}
	if result.Response.Error != nil {
		return "", fmt.Errorf("腾讯云短信发送失败: %s - %s", result.Response.Error.Code, result.Response.Error.Message)
	}
	if len(result.Response.SendStatusSet) > 0 && result.Response.SendStatusSet[0].Code != "Ok" {
		return "", fmt.Errorf("腾讯云短信发送失败: %s - %s", result.Response.SendStatusSet[0].Code, result.Response.SendStatusSet[0].Message)
	}

	return result.Response.RequestID, nil
}

// tencentSign 计算腾讯云 TC3-HMAC-SHA256 签名
func tencentSign(secretID, secretKey, region, body string, timestamp int64) string {
	date := timeutil.Unix(timestamp, 0).Format("2006-01-02")
	service := "sms"

	// 拼接规范请求串
	canonicalRequest := fmt.Sprintf("POST\n/\n\ncontent-type:application/json\nhost:sms.tencentcloudapi.com\n\ncontent-type;host\n%s", sha256Hex(body))

	// 拼接待签名字符串
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := fmt.Sprintf("TC3-HMAC-SHA256\n%d\n%s\n%s", timestamp, credentialScope, sha256Hex(canonicalRequest))

	// 计算签名
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	return fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=content-type;host, Signature=%s",
		secretID, credentialScope, signature)
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// ────────────────────── 辅助工具 ──────────────────────

// normalizeRegion 规范化地域参数
func normalizeRegion(input, fallback string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return fallback
	}
	return input
}

func createAliyunSMSClient(cfg *captchadomain.SMSProviderConfig) (*dysmsclient.Client, error) {
	config := &openapiutil.Config{
		Endpoint: tea.String("dysmsapi.aliyuncs.com"),
		RegionId: tea.String(normalizeRegion(cfg.Region, "cn-hangzhou")),
	}
	if strings.TrimSpace(cfg.AccessKey) != "" && strings.TrimSpace(cfg.SecretKey) != "" {
		config.AccessKeyId = tea.String(strings.TrimSpace(cfg.AccessKey))
		config.AccessKeySecret = tea.String(strings.TrimSpace(cfg.SecretKey))
	} else {
		cred, err := credential.NewCredential(nil)
		if err != nil {
			return nil, err
		}
		config.Credential = cred
	}
	return dysmsclient.NewClient(config)
}

func resolveSMSCodeParamKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "code"
	}
	return value
}

// AvailableSMSProviders 返回当前已注册的短信服务商列表
func (s *CaptchaService) AvailableSMSProviders() []string {
	names := make([]string, 0, len(s.smsProviders))
	for k := range s.smsProviders {
		names = append(names, string(k))
	}
	return names
}

// ────────────────────── MonitorService 兼容 ──────────────────────

// Stats 返回验证码服务状态摘要（用于监控面板）
func (s *CaptchaService) Stats() map[string]any {
	cfg := s.cfg.Captcha
	providers := s.AvailableSMSProviders()
	return map[string]any{
		"enabled":      cfg.Enabled,
		"imageEnabled": cfg.Image.Enabled,
		"mathEnabled":  cfg.Math.Enabled,
		"digitEnabled": cfg.Digit.Enabled,
		"smsEnabled":   cfg.SMS.Enabled,
		"ttlSeconds":   int(cfg.TTL.Seconds()),
		"smsProviders": providers,
		"status":       statusText(cfg.Enabled),
		"httpStatus":   http.StatusOK,
	}
}

func statusText(enabled bool) string {
	if enabled {
		return "running"
	}
	return "disabled"
}
