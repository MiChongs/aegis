package service

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type PaymentService struct {
	log    *zap.Logger
	pg     *pgrepo.Repository
	client *resty.Client
}

func NewPaymentService(log *zap.Logger, pg *pgrepo.Repository) *PaymentService {
	client := resty.New().
		SetRetryCount(2).
		SetTimeout(10 * time.Second)
	return &PaymentService{log: log, pg: pg, client: client}
}

func (s *PaymentService) ListConfigs(ctx context.Context, appID int64, paymentMethod string, enabledOnly bool) ([]paymentdomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListPaymentConfigs(ctx, appID, paymentMethod, enabledOnly)
}

func (s *PaymentService) Detail(ctx context.Context, appID int64, configID int64) (*paymentdomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetPaymentConfigByID(ctx, appID, configID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "支付配置不存在")
	}
	return item, nil
}

func (s *PaymentService) Save(ctx context.Context, mutation paymentdomain.ConfigMutation) (*paymentdomain.Config, error) {
	if _, err := s.requireApp(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetPaymentConfigByID(ctx, mutation.AppID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := paymentdomain.Config{
		ID:            mutation.ID,
		AppID:         mutation.AppID,
		PaymentMethod: "epay",
		ConfigName:    "default",
		ConfigData:    map[string]any{},
		Enabled:       true,
		IsDefault:     mutation.ID == 0,
	}
	if current != nil {
		item = *current
	}
	if mutation.PaymentMethod != nil {
		item.PaymentMethod = strings.TrimSpace(*mutation.PaymentMethod)
	}
	if mutation.ConfigName != nil {
		item.ConfigName = strings.TrimSpace(*mutation.ConfigName)
	}
	if mutation.ConfigData != nil {
		item.ConfigData = mutation.ConfigData
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.IsDefault != nil {
		item.IsDefault = *mutation.IsDefault
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if item.ConfigName == "" {
		return nil, apperrors.New(40070, http.StatusBadRequest, "支付配置名称不能为空")
	}
	if item.PaymentMethod == "" {
		return nil, apperrors.New(40071, http.StatusBadRequest, "支付方式不能为空")
	}
	if item.PaymentMethod == "epay" {
		if _, err := decodeEpayConfig(item.ConfigData); err != nil {
			return nil, err
		}
	}
	return s.pg.UpsertPaymentConfig(ctx, item)
}

func (s *PaymentService) Delete(ctx context.Context, appID int64, configID int64) error {
	deleted, err := s.pg.DeletePaymentConfig(ctx, appID, configID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40470, http.StatusNotFound, "支付配置不存在")
	}
	return nil
}

func (s *PaymentService) TestConfig(ctx context.Context, appID int64, configID int64) (map[string]any, error) {
	config, err := s.Detail(ctx, appID, configID)
	if err != nil {
		return nil, err
	}
	if config.PaymentMethod != "epay" {
		return map[string]any{"config_valid": true, "api_accessible": false, "message": "暂仅支持易支付测试"}, nil
	}
	epay, err := decodeEpayConfig(config.ConfigData)
	if err != nil {
		return nil, err
	}
	queryURL := strings.TrimRight(epay.APIURL, "/") + "/api.php"
	resp, err := s.client.R().SetQueryParams(map[string]string{
		"act":          "order",
		"pid":          epay.PID,
		"key":          epay.Key,
		"out_trade_no": "TEST_" + time.Now().UTC().Format("20060102150405"),
	}).SetContext(ctx).Get(queryURL)
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode(), "body": resp.String()}, nil
}

func (s *PaymentService) InitDefaultEpayConfig(ctx context.Context, appID int64, cfg paymentdomain.EpayConfig) (*paymentdomain.Config, error) {
	name := "default"
	method := "epay"
	isDefault := true
	enabled := true
	return s.Save(ctx, paymentdomain.ConfigMutation{
		AppID:         appID,
		PaymentMethod: &method,
		ConfigName:    &name,
		ConfigData: map[string]any{
			"pid":            cfg.PID,
			"key":            cfg.Key,
			"apiUrl":         cfg.APIURL,
			"sitename":       cfg.SiteName,
			"notifyUrl":      cfg.NotifyURL,
			"returnUrl":      cfg.ReturnURL,
			"signType":       cfg.SignType,
			"supportedTypes": cfg.SupportedTypes,
			"expireMinutes":  cfg.ExpireMinutes,
			"minAmount":      cfg.MinAmount,
			"maxAmount":      cfg.MaxAmount,
			"allowedIPs":     cfg.AllowedIPs,
			"verifyIP":       cfg.VerifyIP,
		},
		Enabled:   &enabled,
		IsDefault: &isDefault,
	})
}

func (s *PaymentService) CreateOrder(ctx context.Context, session *authdomain.Session, subject string, body string, amount string, providerType string, configName string, notifyURL string, returnURL string, metadata map[string]any, clientIP string) (*paymentdomain.PaymentPayload, *paymentdomain.Order, error) {
	if session == nil {
		return nil, nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	config, err := s.pg.GetPaymentConfig(ctx, session.AppID, "epay", configName)
	if err != nil {
		return nil, nil, err
	}
	if config == nil || !config.Enabled {
		return nil, nil, apperrors.New(40471, http.StatusNotFound, "未找到可用支付配置")
	}
	epay, err := decodeEpayConfig(config.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	parsedAmount, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || !parsedAmount.IsPositive() {
		return nil, nil, apperrors.New(40072, http.StatusBadRequest, "支付金额无效")
	}
	if parsedAmount.LessThan(decimal.NewFromFloat(epay.MinAmount)) || parsedAmount.GreaterThan(decimal.NewFromFloat(epay.MaxAmount)) {
		return nil, nil, apperrors.New(40073, http.StatusBadRequest, "支付金额超出配置范围")
	}
	if strings.TrimSpace(subject) == "" {
		return nil, nil, apperrors.New(40079, http.StatusBadRequest, "商品名称不能为空")
	}
	orderNo := fmt.Sprintf("P%d%s%s", session.AppID, time.Now().UTC().Format("20060102150405"), randomDigits(6))
	expireMinutes := epay.ExpireMinutes
	if expireMinutes <= 0 {
		expireMinutes = 30
	}
	expireAt := time.Now().Add(time.Duration(expireMinutes) * time.Minute)
	order, err := s.pg.CreatePaymentOrder(ctx, paymentdomain.OrderMutation{
		AppID:         session.AppID,
		UserID:        &session.UserID,
		ConfigID:      config.ID,
		OrderNo:       orderNo,
		Subject:       strings.TrimSpace(subject),
		Body:          strings.TrimSpace(body),
		Amount:        parsedAmount,
		PaymentMethod: "epay",
		ProviderType:  strings.TrimSpace(providerType),
		ClientIP:      clientIP,
		NotifyURL:     pickString(notifyURL, epay.NotifyURL),
		ReturnURL:     pickString(returnURL, epay.ReturnURL),
		Metadata:      metadata,
		ExpireAt:      &expireAt,
	})
	if err != nil {
		return nil, nil, err
	}

	params := map[string]string{
		"pid":          epay.PID,
		"type":         normalizeProviderType(providerType),
		"out_trade_no": orderNo,
		"notify_url":   order.NotifyURL,
		"return_url":   order.ReturnURL,
		"name":         order.Subject,
		"money":        parsedAmount.StringFixed(2),
		"sign_type":    normalizeSignType(epay.SignType),
	}
	if len(metadata) > 0 {
		raw, _ := json.Marshal(metadata)
		params["param"] = string(raw)
	}
	params["sign"] = generatePaymentSign(params, epay.Key, params["sign_type"])
	submitURL := strings.TrimRight(epay.APIURL, "/") + "/submit.php"
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      orderNo,
		PaymentURL:   submitURL,
		RedirectURL:  submitURL + "?" + url.Values(mapStringSlice(params)).Encode(),
		HTML:         buildPaymentFormHTML(submitURL, params),
		FormData:     mapStringAny(params),
		ProviderType: params["type"],
	}, order, nil
}

func (s *PaymentService) QueryOrder(ctx context.Context, orderNo string) (*paymentdomain.Order, error) {
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	return order, nil
}

func (s *PaymentService) QueryEpayRemoteOrder(ctx context.Context, orderNo string) (map[string]any, error) {
	order, err := s.QueryOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, apperrors.New(40473, http.StatusNotFound, "支付配置不存在")
	}
	epay, err := decodeEpayConfig(config.ConfigData)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.R().SetContext(ctx).SetQueryParams(map[string]string{
		"act":          "order",
		"pid":          epay.PID,
		"key":          epay.Key,
		"out_trade_no": orderNo,
	}).Get(strings.TrimRight(epay.APIURL, "/") + "/api.php")
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		data = map[string]any{"raw": resp.String()}
	}
	return data, nil
}

func (s *PaymentService) HandleEpayCallback(ctx context.Context, callbackData map[string]string, callbackMethod string, clientIP string) (*paymentdomain.CallbackResult, error) {
	orderNo := strings.TrimSpace(callbackData["out_trade_no"])
	if orderNo == "" {
		return nil, apperrors.New(40074, http.StatusBadRequest, "缺少订单号")
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, apperrors.New(40473, http.StatusNotFound, "支付配置不存在")
	}
	epay, err := decodeEpayConfig(config.ConfigData)
	if err != nil {
		return nil, err
	}

	sign := callbackData["sign"]
	signType := normalizeSignType(firstNonEmpty(callbackData["sign_type"], epay.SignType))
	if sign == "" {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, "epay", callbackMethod, clientIP, mapStringAny(callbackData), "missing_sign", "缺少签名")
		return nil, apperrors.New(40075, http.StatusBadRequest, "缺少签名")
	}
	if epay.VerifyIP && len(epay.AllowedIPs) > 0 && !containsString(epay.AllowedIPs, clientIP) {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, "epay", callbackMethod, clientIP, mapStringAny(callbackData), "ip_rejected", "回调IP未授权")
		return nil, apperrors.New(40370, http.StatusForbidden, "回调IP未授权")
	}
	verifyData := map[string]string{}
	for k, v := range callbackData {
		if k == "sign" || k == "sign_type" || strings.TrimSpace(v) == "" {
			continue
		}
		verifyData[k] = v
	}
	if !strings.EqualFold(generatePaymentSign(verifyData, epay.Key, signType), sign) {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, "epay", callbackMethod, clientIP, mapStringAny(callbackData), "sign_failed", "签名验证失败")
		return nil, apperrors.New(40076, http.StatusBadRequest, "签名验证失败")
	}
	tradeStatus := callbackData["trade_status"]
	paid := tradeStatus == "TRADE_SUCCESS" || tradeStatus == "TRADE_FINISHED"
	if paid {
		if err := s.pg.MarkPaymentOrderPaid(ctx, order.ID, callbackData["trade_no"], tradeStatus, mapStringAny(callbackData)); err != nil {
			return nil, err
		}
	} else {
		if err := s.pg.MarkPaymentOrderCallbackFailed(ctx, order.ID, tradeStatus, mapStringAny(callbackData)); err != nil {
			return nil, err
		}
	}
	_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, "epay", callbackMethod, clientIP, mapStringAny(callbackData), tradeStatus, "ok")
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         order.OrderNo,
		ProviderOrderNo: callbackData["trade_no"],
		TradeStatus:     tradeStatus,
		PaymentMethod:   normalizeProviderType(callbackData["type"]),
		Amount:          order.Amount,
		RawData:         mapStringAny(callbackData),
	}, nil
}

func (s *PaymentService) requireApp(ctx context.Context, appID int64) (appNameHolder, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return appNameHolder{}, err
	}
	if app == nil {
		return appNameHolder{}, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return appNameHolder{Name: app.Name}, nil
}

func decodeEpayConfig(data map[string]any) (*paymentdomain.EpayConfig, error) {
	raw, _ := json.Marshal(data)
	var cfg paymentdomain.EpayConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, apperrors.New(40077, http.StatusBadRequest, "支付配置解析失败")
	}
	if strings.TrimSpace(cfg.PID) == "" || strings.TrimSpace(cfg.Key) == "" || strings.TrimSpace(cfg.APIURL) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "易支付配置不完整")
	}
	if cfg.SignType == "" {
		cfg.SignType = "MD5"
	}
	if cfg.ExpireMinutes <= 0 {
		cfg.ExpireMinutes = 30
	}
	if cfg.MinAmount <= 0 {
		cfg.MinAmount = 0.01
	}
	if cfg.MaxAmount <= 0 {
		cfg.MaxAmount = 50000
	}
	if len(cfg.SupportedTypes) == 0 {
		cfg.SupportedTypes = []string{"alipay", "wxpay", "qqpay", "bank"}
	}
	return &cfg, nil
}

func normalizeProviderType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "wechat":
		return "wxpay"
	case "alipay", "wxpay", "qqpay", "bank", "jdpay":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
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

func randomDigits(length int) string {
	const digits = "0123456789"
	buf := make([]byte, length)
	for i := range buf {
		var b [1]byte
		_, _ = rand.Read(b[:])
		buf[i] = digits[int(b[0])%len(digits)]
	}
	return string(buf)
}
