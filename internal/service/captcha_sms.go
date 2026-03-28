package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	captchadomain "aegis/internal/domain/captcha"

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
		httpClient: resty.New().SetTimeout(10 * time.Second),
	}
}

// Send 通过阿里云 SMS API 发送短信
func (p *AliyunSMSProvider) Send(ctx context.Context, phone string, code string, cfg *captchadomain.SMSProviderConfig) (string, error) {
	// 阿里云 SMS API 公共参数
	params := map[string]string{
		"AccessKeyId":      cfg.AccessKey,
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     phone,
		"RegionId":         normalizeRegion(cfg.Region, "cn-hangzhou"),
		"SignName":         cfg.SignName,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   fmt.Sprintf("%d", time.Now().UnixNano()),
		"SignatureVersion": "1.0",
		"TemplateCode":     cfg.TemplateID,
		"TemplateParam":    fmt.Sprintf(`{"code":"%s"}`, code),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2017-05-25",
	}

	// 签名计算
	signature := aliyunSignature(params, cfg.SecretKey)
	params["Signature"] = signature

	resp, err := p.httpClient.R().
		SetContext(ctx).
		SetQueryParams(params).
		Get("https://dysmsapi.aliyuncs.com/")
	if err != nil {
		return "", fmt.Errorf("阿里云短信请求失败: %w", err)
	}

	var result struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("阿里云短信响应解析失败: %w", err)
	}
	if result.Code != "OK" {
		return "", fmt.Errorf("阿里云短信发送失败: %s - %s", result.Code, result.Message)
	}

	return result.RequestID, nil
}

// aliyunSignature 计算阿里云 API 签名（HMAC-SHA1）
func aliyunSignature(params map[string]string, secretKey string) string {
	// 按 key 排序
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构造待签名字符串
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", specialURLEncode(k), specialURLEncode(params[k])))
	}
	query := strings.Join(pairs, "&")
	stringToSign := "GET&%2F&" + specialURLEncode(query)

	// HMAC-SHA1
	mac := hmac.New(sha256.New, []byte(secretKey+"&"))
	mac.Write([]byte(stringToSign))
	return hex.EncodeToString(mac.Sum(nil))
}

// specialURLEncode 阿里云特殊 URL 编码
func specialURLEncode(value string) string {
	encoded := strings.NewReplacer(
		"+", "%20",
		"*", "%2A",
		"%7E", "~",
	).Replace(value)
	return encoded
}

// ────────────────────── 腾讯云短信 ──────────────────────

// TencentSMSProvider 腾讯云短信服务
type TencentSMSProvider struct {
	httpClient *resty.Client
}

// NewTencentSMSProvider 创建腾讯云短信提供商
func NewTencentSMSProvider() *TencentSMSProvider {
	return &TencentSMSProvider{
		httpClient: resty.New().SetTimeout(10 * time.Second),
	}
}

// Send 通过腾讯云 SMS API 发送短信
func (p *TencentSMSProvider) Send(ctx context.Context, phone string, code string, cfg *captchadomain.SMSProviderConfig) (string, error) {
	// 腾讯云 SMS TC3-HMAC-SHA256 签名
	timestamp := time.Now().Unix()
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

	resp, err := p.httpClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", authorization).
		SetHeader("Host", "sms.tencentcloudapi.com").
		SetHeader("X-TC-Action", "SendSms").
		SetHeader("X-TC-Timestamp", fmt.Sprintf("%d", timestamp)).
		SetHeader("X-TC-Version", "2021-01-11").
		SetHeader("X-TC-Region", region).
		SetBody(bodyBytes).
		Post("https://sms.tencentcloudapi.com/")
	if err != nil {
		return "", fmt.Errorf("腾讯云短信请求失败: %w", err)
	}

	var result struct {
		Response struct {
			RequestID string `json:"RequestId"`
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
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
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
		"enabled":       cfg.Enabled,
		"imageEnabled":  cfg.Image.Enabled,
		"mathEnabled":   cfg.Math.Enabled,
		"digitEnabled":  cfg.Digit.Enabled,
		"smsEnabled":    cfg.SMS.Enabled,
		"ttlSeconds":    int(cfg.TTL.Seconds()),
		"smsProviders":  providers,
		"status":        statusText(cfg.Enabled),
		"httpStatus":    http.StatusOK,
	}
}

func statusText(enabled bool) string {
	if enabled {
		return "running"
	}
	return "disabled"
}
