package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	plugindomain "aegis/internal/domain/plugin"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"github.com/signintech/gopdf"
	"go.uber.org/zap"
)

type PaymentService struct {
	log           *zap.Logger
	pg            *pgrepo.Repository
	client        *resty.Client
	providers     map[string]paymentProvider
	plugin        *PluginService
	billExportCfg config.PaymentBillExportConfig
	closeCh       chan struct{}
	closeOnce     sync.Once
	closed        chan struct{}
}

func (s *PaymentService) SetPluginService(p *PluginService) { s.plugin = p }

func NewPaymentService(log *zap.Logger, pg *pgrepo.Repository, billExportCfg config.PaymentBillExportConfig) *PaymentService {
	client := resty.New().
		SetRetryCount(2).
		SetTimeout(10 * time.Second)
	s := &PaymentService{
		log:           log,
		pg:            pg,
		client:        client,
		providers:     make(map[string]paymentProvider),
		billExportCfg: billExportCfg,
		closeCh:       make(chan struct{}),
		closed:        make(chan struct{}),
	}

	// 注册所有支付提供商
	s.registerProvider(newEpayProvider(client))
	s.registerProvider(newRainbowEpayProvider(client))
	s.registerProvider(newXunhupayProvider(client))
	s.registerProvider(newPayjsProvider(client))
	s.registerProvider(newQRPayProvider(client))
	s.registerProvider(newVMQPayProvider(client))
	s.registerProvider(newAlipayNativeProvider(client))
	s.registerProvider(newWechatNativeProvider(client))
	s.registerProvider(newStripeProvider(client))
	s.registerProvider(newPaypalProvider(client))

	s.startBillExportCleaner()
	return s
}

func (s *PaymentService) Close(context.Context) {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		<-s.closed
	})
}

func (s *PaymentService) registerProvider(p paymentProvider) {
	s.providers[p.Name()] = p
}

func (s *PaymentService) resolveProvider(method string) (paymentProvider, error) {
	p, ok := s.providers[strings.TrimSpace(method)]
	if !ok {
		return nil, apperrors.New(40079, http.StatusBadRequest, "不支持的支付方式: "+method)
	}
	return p, nil
}

// AvailableMethods 返回所有已注册支付方式列表
func (s *PaymentService) AvailableMethods() []paymentdomain.ProviderMeta {
	items := make([]paymentdomain.ProviderMeta, 0, len(s.providers))
	for _, p := range s.providers {
		items = append(items, paymentdomain.ProviderMeta{
			Method:         p.Name(),
			Name:           p.Label(),
			SupportedTypes: p.SupportedPayTypes(),
		})
	}
	return items
}

// ── 配置管理 ──

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

	// 通过 Provider 验证配置
	provider, err := s.resolveProvider(item.PaymentMethod)
	if err != nil {
		return nil, err
	}
	if err := provider.ValidateConfig(item.ConfigData); err != nil {
		return nil, err
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
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "message": "不支持的支付方式: " + config.PaymentMethod}, nil
	}
	return provider.TestConnection(ctx, config.ConfigData)
}

func (s *PaymentService) InitDefaultEpayConfig(ctx context.Context, appID int64, cfg paymentdomain.EpayConfig) (*paymentdomain.Config, error) {
	name := "default"
	method := paymentdomain.MethodEpay
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

// ── 订单流程 ──

func (s *PaymentService) CreateOrder(ctx context.Context, session *authdomain.Session, subject string, body string, amount string, providerType string, configName string, notifyURL string, returnURL string, metadata map[string]any, clientIP string) (*paymentdomain.PaymentPayload, *paymentdomain.Order, error) {
	if session == nil {
		return nil, nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}

	// 查找配置（优先按 configName 精确匹配，否则取默认）
	config, err := s.pg.GetPaymentConfig(ctx, session.AppID, "", configName)
	if err != nil {
		return nil, nil, err
	}
	if config == nil || !config.Enabled {
		return nil, nil, apperrors.New(40471, http.StatusNotFound, "未找到可用支付配置")
	}

	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return nil, nil, err
	}

	parsedAmount, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || !parsedAmount.IsPositive() {
		return nil, nil, apperrors.New(40072, http.StatusBadRequest, "支付金额无效")
	}
	if strings.TrimSpace(subject) == "" {
		return nil, nil, apperrors.New(40079, http.StatusBadRequest, "商品名称不能为空")
	}

	orderNo := fmt.Sprintf("P%d%s%s", session.AppID, time.Now().UTC().Format("20060102150405"), randomDigits(6))
	expireAt := time.Now().Add(30 * time.Minute)

	order, err := s.pg.CreatePaymentOrder(ctx, paymentdomain.OrderMutation{
		AppID:         session.AppID,
		UserID:        &session.UserID,
		ConfigID:      config.ID,
		OrderNo:       orderNo,
		Subject:       strings.TrimSpace(subject),
		Body:          strings.TrimSpace(body),
		Amount:        parsedAmount,
		PaymentMethod: config.PaymentMethod,
		ProviderType:  strings.TrimSpace(providerType),
		ClientIP:      clientIP,
		NotifyURL:     pickString(notifyURL, ""),
		ReturnURL:     pickString(returnURL, ""),
		Metadata:      metadata,
		ExpireAt:      &expireAt,
	})
	if err != nil {
		return nil, nil, err
	}

	payload, err := provider.CreateOrder(ctx, config.ConfigData, PaymentOrderRequest{
		OrderNo:      orderNo,
		Subject:      order.Subject,
		Body:         order.Body,
		Amount:       parsedAmount,
		ProviderType: strings.TrimSpace(providerType),
		NotifyURL:    order.NotifyURL,
		ReturnURL:    order.ReturnURL,
		ClientIP:     clientIP,
		Metadata:     metadata,
		ExpireAt:     &expireAt,
	})
	if err != nil {
		return nil, nil, err
	}
	if s.plugin != nil {
		appID := session.AppID
		go s.plugin.ExecuteHook(context.Background(), HookPaymentCreated, map[string]any{
			"orderId": order.ID,
		}, plugindomain.HookMetadata{AppID: &appID, UserID: &session.UserID})
	}
	return payload, order, nil
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

func (s *PaymentService) GetUserOrder(ctx context.Context, session *authdomain.Session, orderNo string) (*paymentdomain.Order, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	order, err := s.pg.GetPaymentOrderByOrderNoForUser(ctx, session.AppID, session.UserID, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	return order, nil
}

func (s *PaymentService) ListUserOrders(ctx context.Context, session *authdomain.Session, query paymentdomain.OrderListQuery) (*paymentdomain.OrderListResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.pg.ListPaymentOrdersByUser(ctx, session.AppID, session.UserID, query.Status, page, limit)
	if err != nil {
		return nil, err
	}
	return &paymentdomain.OrderListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPaymentTotalPages(total, limit),
	}, nil
}

func (s *PaymentService) CreateUserOrderBillExport(ctx context.Context, session *authdomain.Session, orderNo string, ttlOverride time.Duration) (*paymentdomain.BillExport, error) {
	doc, err := s.loadPaymentBillDocument(ctx, session, orderNo)
	if err != nil {
		return nil, err
	}
	pdfBytes, err := renderPaymentBillPDF(doc)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ttl := s.resolveBillExportTTL(ttlOverride)
	export := paymentdomain.BillExport{
		BillID:    randomBillExportID(16),
		OrderNo:   doc.Order.OrderNo,
		FileName:  fmt.Sprintf("payment_bill_%s.pdf", doc.Order.OrderNo),
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	export.DownloadURL = fmt.Sprintf("/api/pay/bills/%s/download", export.BillID)
	if err := s.persistBillExport(session.AppID, session.UserID, export, pdfBytes); err != nil {
		return nil, err
	}
	return &export, nil
}

func (s *PaymentService) DownloadUserOrderBillExport(ctx context.Context, session *authdomain.Session, billID string) ([]byte, string, error) {
	_ = ctx
	if session == nil {
		return nil, "", apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	meta, metaPath, err := s.loadBillExportMeta(session.AppID, billID)
	if err != nil {
		return nil, "", err
	}
	if meta.UserID != session.UserID {
		return nil, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
	}
	if time.Now().UTC().After(meta.ExpiresAt) {
		s.deleteBillExport(meta, metaPath)
		return nil, "", apperrors.New(41075, http.StatusGone, "账单文件已过期")
	}
	pdfBytes, err := os.ReadFile(meta.FilePath)
	if err != nil {
		s.log.Warn("read bill export file failed", zap.String("bill_id", meta.BillID), zap.String("file_path", meta.FilePath), zap.Error(err))
		if os.IsNotExist(err) {
			_ = os.Remove(metaPath)
			return nil, "", apperrors.New(40476, http.StatusNotFound, "账单文件不存在")
		}
		return nil, "", apperrors.New(50075, http.StatusInternalServerError, "读取账单文件失败")
	}
	return pdfBytes, meta.FileName, nil
}

func (s *PaymentService) QueryEpayRemoteOrder(ctx context.Context, orderNo string) (map[string]any, error) {
	return s.QueryRemoteOrder(ctx, orderNo)
}

// QueryRemoteOrder 向上游查询订单状态（通用）
func (s *PaymentService) QueryRemoteOrder(ctx context.Context, orderNo string) (map[string]any, error) {
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
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return nil, err
	}
	return provider.QueryRemoteOrder(ctx, config.ConfigData, orderNo)
}

// HandleEpayCallback 处理易支付回调（兼容旧路由）
func (s *PaymentService) HandleEpayCallback(ctx context.Context, callbackData map[string]string, callbackMethod string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return s.HandleCallback(ctx, paymentdomain.MethodEpay, callbackData, callbackMethod, clientIP)
}

// HandleCallback 处理通用支付回调
func (s *PaymentService) HandleCallback(ctx context.Context, method string, callbackData map[string]string, callbackMethod string, clientIP string) (*paymentdomain.CallbackResult, error) {
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
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, mapStringAny(callbackData), "unsupported_method", "不支持的支付方式")
		return nil, err
	}

	result, err := provider.HandleCallback(ctx, config.ConfigData, callbackData, clientIP)
	if err != nil {
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, mapStringAny(callbackData), "verify_failed", err.Error())
		return nil, err
	}

	// 更新订单状态
	if result.Paid {
		if err := s.pg.MarkPaymentOrderPaid(ctx, order.ID, result.ProviderOrderNo, result.TradeStatus, result.RawData); err != nil {
			return nil, err
		}
		if s.plugin != nil {
			appID := order.AppID
			go s.plugin.ExecuteHook(context.Background(), HookPaymentCompleted, map[string]any{
				"orderId": order.ID,
			}, plugindomain.HookMetadata{AppID: &appID})
		}
	} else {
		if err := s.pg.MarkPaymentOrderCallbackFailed(ctx, order.ID, result.TradeStatus, result.RawData); err != nil {
			return nil, err
		}
	}

	result.OrderNo = order.OrderNo
	result.Amount = order.Amount
	_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, method, callbackMethod, clientIP, result.RawData, result.TradeStatus, "ok")
	return result, nil
}

// ── 辅助函数 ──

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

func (s *PaymentService) loadPaymentBillDocument(ctx context.Context, session *authdomain.Session, orderNo string) (paymentBillDocument, error) {
	if session == nil {
		return paymentBillDocument{}, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	order, err := s.GetUserOrder(ctx, session, orderNo)
	if err != nil {
		return paymentBillDocument{}, err
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return paymentBillDocument{}, err
	}
	if user == nil || user.AppID != session.AppID {
		return paymentBillDocument{}, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, session.UserID)
	if err != nil {
		return paymentBillDocument{}, err
	}
	app, err := s.requireApp(ctx, session.AppID)
	if err != nil {
		return paymentBillDocument{}, err
	}
	return paymentBillDocument{
		Order:   *order,
		AppName: app.Name,
		User:    user,
		Profile: profile,
	}, nil
}

func (s *PaymentService) resolveBillExportTTL(override time.Duration) time.Duration {
	ttl := s.billExportCfg.TTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if override <= 0 {
		return ttl
	}
	if override > ttl {
		return ttl
	}
	return override
}

func (s *PaymentService) persistBillExport(appID int64, userID int64, export paymentdomain.BillExport, pdfBytes []byte) error {
	dirPath := s.billExportAppDir(appID)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		s.log.Warn("create payment bill export dir failed", zap.String("dir", dirPath), zap.Error(err))
		return apperrors.New(50076, http.StatusInternalServerError, "创建账单导出目录失败")
	}
	pdfPath, metaPath := s.billExportPaths(appID, export.BillID)
	meta := paymentBillExportMeta{
		BillID:    export.BillID,
		AppID:     appID,
		UserID:    userID,
		OrderNo:   export.OrderNo,
		FileName:  export.FileName,
		FilePath:  pdfPath,
		CreatedAt: export.CreatedAt,
		ExpiresAt: export.ExpiresAt,
	}
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		s.log.Warn("write payment bill export file failed", zap.String("bill_id", export.BillID), zap.String("file_path", pdfPath), zap.Error(err))
		return apperrors.New(50077, http.StatusInternalServerError, "写入账单文件失败")
	}
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		_ = os.Remove(pdfPath)
		return apperrors.New(50078, http.StatusInternalServerError, "写入账单元数据失败")
	}
	if err := os.WriteFile(metaPath, rawMeta, 0o600); err != nil {
		_ = os.Remove(pdfPath)
		s.log.Warn("write payment bill export metadata failed", zap.String("bill_id", export.BillID), zap.String("meta_path", metaPath), zap.Error(err))
		return apperrors.New(50078, http.StatusInternalServerError, "写入账单元数据失败")
	}
	return nil
}

func (s *PaymentService) loadBillExportMeta(appID int64, billID string) (paymentBillExportMeta, string, error) {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return paymentBillExportMeta{}, "", apperrors.New(40075, http.StatusBadRequest, "账单标识不能为空")
	}
	_, metaPath := s.billExportPaths(appID, billID)
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return paymentBillExportMeta{}, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
		}
		s.log.Warn("read payment bill export metadata failed", zap.String("bill_id", billID), zap.String("meta_path", metaPath), zap.Error(err))
		return paymentBillExportMeta{}, "", apperrors.New(50079, http.StatusInternalServerError, "读取账单元数据失败")
	}
	var meta paymentBillExportMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		s.log.Warn("parse payment bill export metadata failed", zap.String("bill_id", billID), zap.String("meta_path", metaPath), zap.Error(err))
		s.deleteBillExport(paymentBillExportMeta{
			BillID:   billID,
			AppID:    appID,
			FilePath: filepath.Join(s.billExportAppDir(appID), billID+".pdf"),
		}, metaPath)
		return paymentBillExportMeta{}, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
	}
	return meta, metaPath, nil
}

func (s *PaymentService) billExportAppDir(appID int64) string {
	return filepath.Join(s.billExportCfg.RootDir, fmt.Sprintf("%d", appID))
}

func (s *PaymentService) billExportPaths(appID int64, billID string) (string, string) {
	dirPath := s.billExportAppDir(appID)
	return filepath.Join(dirPath, billID+".pdf"), filepath.Join(dirPath, billID+".meta.json")
}

func (s *PaymentService) startBillExportCleaner() {
	go func() {
		defer close(s.closed)
		s.cleanupExpiredBillExports()
		ticker := time.NewTicker(s.billExportCleanupInterval())
		defer ticker.Stop()
		for {
			select {
			case <-s.closeCh:
				return
			case <-ticker.C:
				s.cleanupExpiredBillExports()
			}
		}
	}()
}

func (s *PaymentService) billExportCleanupInterval() time.Duration {
	if s.billExportCfg.CleanupInterval <= 0 {
		return 5 * time.Minute
	}
	return s.billExportCfg.CleanupInterval
}

func (s *PaymentService) cleanupExpiredBillExports() {
	rootDir := strings.TrimSpace(s.billExportCfg.RootDir)
	if rootDir == "" {
		return
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		s.log.Warn("create payment bill export root failed", zap.String("root_dir", rootDir), zap.Error(err))
		return
	}
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Warn("list payment bill export root failed", zap.String("root_dir", rootDir), zap.Error(err))
		}
		return
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPaths, err := filepath.Glob(filepath.Join(rootDir, entry.Name(), "*.meta.json"))
		if err != nil {
			s.log.Warn("glob payment bill export metadata failed", zap.String("app_dir", filepath.Join(rootDir, entry.Name())), zap.Error(err))
			continue
		}
		for _, metaPath := range metaPaths {
			meta, ok := s.readBillExportMetaFile(metaPath)
			if !ok {
				continue
			}
			if now.After(meta.ExpiresAt) {
				s.deleteBillExport(meta, metaPath)
			}
		}
	}
}

func (s *PaymentService) readBillExportMetaFile(metaPath string) (paymentBillExportMeta, bool) {
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Warn("read payment bill export metadata failed", zap.String("meta_path", metaPath), zap.Error(err))
		}
		return paymentBillExportMeta{}, false
	}
	var meta paymentBillExportMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		s.log.Warn("parse payment bill export metadata failed", zap.String("meta_path", metaPath), zap.Error(err))
		_ = os.Remove(metaPath)
		return paymentBillExportMeta{}, false
	}
	return meta, true
}

func (s *PaymentService) deleteBillExport(meta paymentBillExportMeta, metaPath string) {
	if strings.TrimSpace(meta.FilePath) != "" {
		if err := os.Remove(meta.FilePath); err != nil && !os.IsNotExist(err) {
			s.log.Warn("delete payment bill export file failed", zap.String("bill_id", meta.BillID), zap.String("file_path", meta.FilePath), zap.Error(err))
		}
	}
	if strings.TrimSpace(metaPath) != "" {
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			s.log.Warn("delete payment bill export metadata failed", zap.String("bill_id", meta.BillID), zap.String("meta_path", metaPath), zap.Error(err))
		}
	}
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

func randomBillExportID(byteLen int) string {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d%s", time.Now().UTC().UnixNano(), randomDigits(6))
	}
	return hex.EncodeToString(buf)
}

// decodeProviderConfig 通用配置解码辅助
func decodeProviderConfig[T any](data map[string]any) (*T, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, apperrors.New(40077, http.StatusBadRequest, "支付配置序列化失败")
	}
	var cfg T
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, apperrors.New(40077, http.StatusBadRequest, "支付配置解析失败")
	}
	return &cfg, nil
}

func calcPaymentTotalPages(total int64, limit int) int {
	if limit <= 0 {
		return 0
	}
	pages := int(total / int64(limit))
	if total%int64(limit) != 0 {
		pages++
	}
	if pages == 0 {
		return 1
	}
	return pages
}

type paymentBillDocument struct {
	Order   paymentdomain.Order
	AppName string
	User    *userdomain.User
	Profile *userdomain.Profile
}

type paymentBillExportMeta struct {
	BillID    string    `json:"billId"`
	AppID     int64     `json:"appid"`
	UserID    int64     `json:"userId"`
	OrderNo   string    `json:"orderNo"`
	FileName  string    `json:"fileName"`
	FilePath  string    `json:"filePath"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func renderPaymentBillPDF(doc paymentBillDocument) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	fontPath, err := registerPaymentBillFont(pdf)
	if err != nil {
		return nil, err
	}
	if err := pdf.SetFont("bill", "", 20); err != nil {
		return nil, err
	}
	pdf.SetTextColor(26, 38, 52)
	pdf.SetXY(36, 40)
	_ = pdf.Cell(nil, "Aegis Electronic Bill")

	if err := pdf.SetFont("bill", "", 10); err != nil {
		return nil, err
	}
	pdf.SetTextColor(102, 112, 133)
	pdf.SetXY(36, 64)
	_ = pdf.Cell(nil, "Generated At: "+time.Now().UTC().Format(time.RFC3339))
	pdf.SetXY(36, 78)
	_ = pdf.Cell(nil, "Font: "+filepath.Base(fontPath))

	pdf.SetStrokeColor(223, 227, 234)
	pdf.Line(36, 92, 559, 92)

	y := 112.0
	y = writeBillSection(pdf, y, "Bill Summary", [][2]string{
		{"Order No", doc.Order.OrderNo},
		{"Status", doc.Order.Status},
		{"Amount", doc.Order.Amount.StringFixed(2)},
		{"Payment Method", doc.Order.PaymentMethod},
		{"Provider Type", emptyFallback(doc.Order.ProviderType, "-")},
		{"Provider Order No", emptyFallback(doc.Order.ProviderOrderNo, "-")},
	})
	y = writeBillSection(pdf, y+10, "Merchant", [][2]string{
		{"Application", emptyFallback(doc.AppName, fmt.Sprintf("App #%d", doc.Order.AppID))},
		{"App ID", fmt.Sprintf("%d", doc.Order.AppID)},
	})

	displayName := ""
	if doc.Profile != nil {
		displayName = strings.TrimSpace(doc.Profile.Nickname)
	}
	account := ""
	if doc.User != nil {
		account = strings.TrimSpace(doc.User.Account)
	}
	y = writeBillSection(pdf, y+10, "Payer", [][2]string{
		{"User ID", nullableInt64ToString(doc.Order.UserID)},
		{"Account", emptyFallback(account, "-")},
		{"Display Name", emptyFallback(displayName, "-")},
	})
	y = writeBillSection(pdf, y+10, "Order Details", [][2]string{
		{"Subject", emptyFallback(doc.Order.Subject, "-")},
		{"Description", emptyFallback(doc.Order.Body, "-")},
		{"Created At", doc.Order.CreatedAt.Format(time.RFC3339)},
		{"Paid At", formatOptionalTime(doc.Order.PaidAt)},
		{"Expire At", formatOptionalTime(doc.Order.ExpireAt)},
		{"Client IP", emptyFallback(doc.Order.ClientIP, "-")},
	})

	metadata := "{}"
	if len(doc.Order.Metadata) > 0 {
		raw, _ := json.MarshalIndent(doc.Order.Metadata, "", "  ")
		metadata = string(raw)
	}
	y = writeBillMultilineSection(pdf, y+10, "Metadata", metadata)

	pdf.SetStrokeColor(223, 227, 234)
	pdf.Line(36, y+18, 559, y+18)
	if err := pdf.SetFont("bill", "", 9); err != nil {
		return nil, err
	}
	pdf.SetTextColor(102, 112, 133)
	pdf.SetXY(36, y+28)
	_ = pdf.Cell(nil, "This bill is generated by Aegis and can be used as an electronic transaction record.")

	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeBillSection(pdf *gopdf.GoPdf, startY float64, title string, items [][2]string) float64 {
	_ = pdf.SetFont("bill", "", 13)
	pdf.SetTextColor(26, 38, 52)
	pdf.SetXY(36, startY)
	_ = pdf.Cell(nil, title)

	_ = pdf.SetFont("bill", "", 10)
	labelX := 36.0
	valueX := 180.0
	y := startY + 18
	for _, item := range items {
		pdf.SetTextColor(102, 112, 133)
		pdf.SetXY(labelX, y)
		_ = pdf.Cell(nil, item[0])
		pdf.SetTextColor(26, 38, 52)
		pdf.SetXY(valueX, y)
		_ = pdf.Cell(nil, item[1])
		y += 16
	}
	return y
}

func writeBillMultilineSection(pdf *gopdf.GoPdf, startY float64, title string, content string) float64 {
	_ = pdf.SetFont("bill", "", 13)
	pdf.SetTextColor(26, 38, 52)
	pdf.SetXY(36, startY)
	_ = pdf.Cell(nil, title)

	_ = pdf.SetFont("bill", "", 10)
	pdf.SetTextColor(26, 38, 52)
	pdf.SetXY(36, startY+18)
	lines, err := pdf.SplitText(content, 523)
	if err != nil || len(lines) == 0 {
		lines = []string{content}
	}
	for _, line := range lines {
		_ = pdf.Cell(nil, line)
		pdf.Br(14)
	}
	return startY + 18 + float64(len(lines))*14
}

func registerPaymentBillFont(pdf *gopdf.GoPdf) (string, error) {
	paths := []string{}
	if envPath := strings.TrimSpace(os.Getenv("BILL_EXPORT_FONT_PATH")); envPath != "" {
		paths = append(paths, envPath)
	}
	paths = append(paths,
		`C:\Windows\Fonts\simhei.ttf`,
		`C:\Windows\Fonts\msyh.ttf`,
		`C:\Windows\Fonts\msyh.ttc`,
		`C:\Windows\Fonts\simsun.ttc`,
		`/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc`,
		`/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.otf`,
		`/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc`,
		`/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf`,
		`/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc`,
		`/usr/share/fonts/truetype/arphic/ukai.ttc`,
		`/System/Library/Fonts/PingFang.ttc`,
		`/System/Library/Fonts/Hiragino Sans GB.ttc`,
	)
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		if err := pdf.AddTTFFontData("bill", data); err == nil {
			return path, nil
		}
	}
	return "", apperrors.New(50372, http.StatusServiceUnavailable, "电子账单字体不可用，请配置 BILL_EXPORT_FONT_PATH")
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func emptyFallback(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func nullableInt64ToString(value *int64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *value)
}
