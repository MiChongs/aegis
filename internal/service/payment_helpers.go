package service

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	apperrors "aegis/pkg/errors"
	"github.com/shopspring/decimal"
)

// checkAmountRange 单笔金额限额校验（min/max ≤ 0 表示不限制）
func checkAmountRange(amount decimal.Decimal, min float64, max float64, channelLabel string) error {
	if min > 0 && amount.LessThan(decimal.NewFromFloat(min)) {
		return apperrors.New(40072, http.StatusBadRequest,
			fmt.Sprintf("%s单笔金额不能低于 %.2f", channelLabel, min))
	}
	if max > 0 && amount.GreaterThan(decimal.NewFromFloat(max)) {
		return apperrors.New(40072, http.StatusBadRequest,
			fmt.Sprintf("%s单笔金额不能超过 %.2f", channelLabel, max))
	}
	return nil
}

// configFloat 从原始配置 map 中安全读取数值（JSON 反序列化后可能是 float64 / json.Number / 字符串）
func configFloat(data map[string]any, key string) float64 {
	if data == nil {
		return 0
	}
	switch v := data[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return 0
	}
}

// ── 支付签名与表单辅助 ──

func normalizeProviderType(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" || v == "wechat" {
		return "wxpay"
	}
	return v
}

func normalizeSignType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SHA1", "SHA256":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return "MD5"
	}
}

func generatePaymentSign(params map[string]string, key string, signType string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	raw := strings.Join(parts, "&") + key
	switch normalizeSignType(signType) {
	case "SHA1":
		sum := sha1.Sum([]byte(raw))
		return hex.EncodeToString(sum[:])
	case "SHA256":
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])
	default:
		sum := md5.Sum([]byte(raw))
		return hex.EncodeToString(sum[:])
	}
}

func buildPaymentFormHTML(action string, params map[string]string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><title>支付跳转</title></head><body><form id="payForm" action="`)
	b.WriteString(action)
	b.WriteString(`" method="post">`)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(`<input type="hidden" name="`)
		b.WriteString(k)
		b.WriteString(`" value="`)
		b.WriteString(params[k])
		b.WriteString(`">`)
	}
	b.WriteString(`</form><script>document.getElementById('payForm').submit()</script></body></html>`)
	return b.String()
}

// ── Webhook HMAC 验签辅助（国际渠道通用）──

// hmacSHA256Hex 计算 HMAC-SHA256 并以小写十六进制返回
func hmacSHA256Hex(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// hmacSHA256Base64 计算 HMAC-SHA256 并以标准 Base64 返回
func hmacSHA256Base64(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// secureCompare 常量时间比较，避免签名校验被时序攻击逐字节试探
func secureCompare(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// verifyWebhookHMAC 校验「原始报文 + 共享密钥」的 HMAC 签名。
// encoding 取 "hex" 或 "base64"，与各平台文档一致。
func verifyWebhookHMAC(secret, payload, provided, encoding, channelLabel string) error {
	if strings.TrimSpace(secret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, channelLabel+"未配置 Webhook 签名密钥，无法验签")
	}
	if strings.TrimSpace(provided) == "" {
		return apperrors.New(40075, http.StatusBadRequest, channelLabel+" Webhook 缺少签名头")
	}
	var expected string
	if encoding == "base64" {
		expected = hmacSHA256Base64(secret, payload)
	} else {
		expected = hmacSHA256Hex(secret, payload)
	}
	if !secureCompare(strings.TrimSpace(provided), expected) {
		return apperrors.New(40076, http.StatusBadRequest, channelLabel+" Webhook 验签失败")
	}
	return nil
}

func mapStringSlice(input map[string]string) map[string][]string {
	result := make(map[string][]string, len(input))
	for k, v := range input {
		result[k] = []string{v}
	}
	return result
}

func mapStringAny(input map[string]string) map[string]any {
	result := make(map[string]any, len(input))
	for k, v := range input {
		result[k] = v
	}
	return result
}

func pickString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}
