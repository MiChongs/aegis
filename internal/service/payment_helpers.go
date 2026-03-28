package service

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

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
