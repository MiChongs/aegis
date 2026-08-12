package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	walletdomain "aegis/internal/domain/wallet"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// WalletService 余额系统：查询 / 消费 / 流水 / 管理端调整。
// 所有余额变更经由仓储层单事务账本（行锁 + 幂等键），服务层只做参数与权限语义。
type WalletService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
	// payments 凭证引擎的持有者。nil 表示未接入，流水上就不带凭证入口 ——
	// 而不是给出一个点了会 500 的按钮。
	payments *PaymentService
}

func NewWalletService(log *zap.Logger, pg *pgrepo.Repository) *WalletService {
	return &WalletService{log: log, pg: pg}
}

// SetPaymentService 注入凭证引擎（bootstrap 中调用）。
func (s *WalletService) SetPaymentService(p *PaymentService) { s.payments = p }

// GetMyWallet 查询当前用户钱包
func (s *WalletService) GetMyWallet(ctx context.Context, session *authdomain.Session) (*walletdomain.Wallet, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	w, err := s.pg.GetWalletByUser(ctx, session.UserID, session.AppID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return nil, err
	}
	return w, nil
}

// ListMyTransactions 查询当前用户钱包流水
func (s *WalletService) ListMyTransactions(ctx context.Context, session *authdomain.Session, query walletdomain.ListQuery) (*walletdomain.ListResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	page, limit := normalizePageLimit(query.Page, query.Limit, 100)
	items, total, err := s.pg.ListWalletTransactions(ctx, session.UserID, session.AppID, query.Type, page, limit)
	if err != nil {
		return nil, err
	}
	return &walletdomain.ListResult{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: calcPaymentTotalPages(total, limit),
	}, nil
}

// ListMyTransactionViews 带凭证入口的流水分页。
//
// 与订单列表同构：「这条流水能不能开凭证、开出来是收据还是退款凭证、
// 能不能寄到邮箱」由服务端算好。放到客户端会各端各写一套且很快不一致。
func (s *WalletService) ListMyTransactionViews(ctx context.Context, session *authdomain.Session, query walletdomain.ListQuery, opts paymentdomain.ReceiptOptions) (*walletdomain.TransactionViewListResult, error) {
	result, err := s.ListMyTransactions(ctx, session, query)
	if err != nil {
		return nil, err
	}
	info := s.receiptContext(ctx, session, opts)
	items := make([]walletdomain.TransactionView, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, walletdomain.TransactionView{
			Transaction: result.Items[i],
			Receipt:     s.buildReceiptEntry(&result.Items[i], info),
		})
	}
	return &walletdomain.TransactionViewListResult{
		Items:      items,
		Page:       result.Page,
		Limit:      result.Limit,
		Total:      result.Total,
		TotalPages: result.TotalPages,
	}, nil
}

// GetMyTransactionView 单条流水 + 凭证入口。
func (s *WalletService) GetMyTransactionView(ctx context.Context, session *authdomain.Session, transactionNo string, opts paymentdomain.ReceiptOptions) (*walletdomain.TransactionView, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	transactionNo = strings.TrimSpace(transactionNo)
	if transactionNo == "" {
		return nil, apperrors.New(40099, http.StatusBadRequest, "流水号不能为空")
	}
	txn, err := s.pg.GetWalletTransactionByNoForUser(ctx, session.AppID, session.UserID, transactionNo)
	if err != nil {
		return nil, err
	}
	if txn == nil {
		return nil, apperrors.New(40477, http.StatusNotFound, "流水不存在")
	}
	return &walletdomain.TransactionView{
		Transaction: *txn,
		Receipt:     s.buildReceiptEntry(txn, s.receiptContext(ctx, session, opts)),
	}, nil
}

// receiptContext 一次请求内对所有流水都相同的部分（推荐语言、币种、能否寄送）。
// 单独算一次，否则列表里每条流水都要重新读一遍用户设置与资料。
func (s *WalletService) receiptContext(ctx context.Context, session *authdomain.Session, opts paymentdomain.ReceiptOptions) orderReceiptContext {
	if s.payments == nil {
		return orderReceiptContext{}
	}
	info := s.payments.resolveOrderReceiptContext(ctx, session, opts)
	// 币种只在钱包这条链路上解析：订单凭证的币种固化在订单行上，与钱包无关
	info.walletCurrency = s.payments.resolveWalletCurrency(ctx, session.AppID)
	return info
}

func (s *WalletService) buildReceiptEntry(txn *walletdomain.Transaction, info orderReceiptContext) walletdomain.TransactionReceipt {
	if s.payments == nil {
		return walletdomain.TransactionReceipt{Available: false, EmailHint: "凭证服务未接入"}
	}
	return s.payments.BuildWalletTransactionReceipt(txn, info)
}

// Consume 业务消费扣款（用户侧）。
// idempotencyKey 由客户端携带（如业务订单号），保证网络重试不重复扣款。
func (s *WalletService) Consume(ctx context.Context, session *authdomain.Session, amount decimal.Decimal, title string, remark string, idempotencyKey string, clientIP string) (*walletdomain.ChangeResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	if !amount.IsPositive() {
		return nil, apperrors.New(40080, http.StatusBadRequest, "消费金额必须大于 0")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, apperrors.New(40081, http.StatusBadRequest, "消费说明不能为空")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, apperrors.New(40082, http.StatusBadRequest, "必须提供幂等键 idempotencyKey，防止重复扣款")
	}
	result, err := s.pg.ApplyWalletChange(ctx, walletdomain.Change{
		UserID: session.UserID,
		AppID:  session.AppID,
		Type:   walletdomain.TxnTypeConsume,
		Amount: amount.Neg(),
		// 幂等键按应用与用户命名空间化，杜绝跨用户/跨应用键碰撞
		IdempotencyKey: walletIdemKey(session.AppID, session.UserID, idempotencyKey),
		Title:          title,
		Remark:         strings.TrimSpace(remark),
		ClientIP:       clientIP,
	})
	result, err = s.translateWalletError(result, err)
	if err != nil || result == nil {
		return nil, err
	}
	// 扣完款当场把凭证入口给出来：这一刻用户手里才第一次有「这笔钱花在哪」的凭据
	result.Receipt = s.attachReceiptEntry(ctx, session, &result.Transaction)
	return result, nil
}

// attachReceiptEntry 为一条刚落地的流水算出凭证入口。
// 算不出来（未接入凭证引擎）就返回 nil，字段整个消失，而不是给出一个点了报错的按钮。
func (s *WalletService) attachReceiptEntry(ctx context.Context, session *authdomain.Session, txn *walletdomain.Transaction) *walletdomain.TransactionReceipt {
	if s.payments == nil || txn == nil {
		return nil
	}
	entry := s.buildReceiptEntry(txn, s.receiptContext(ctx, session, paymentdomain.ReceiptOptions{}))
	if !entry.Available {
		return nil
	}
	return &entry
}

// AdminAdjust 管理员调整余额（正数充入 / 负数扣减），用于人工补偿、退款等场景
func (s *WalletService) AdminAdjust(ctx context.Context, userID int64, appID int64, amount decimal.Decimal, reason string, operator string, clientIP string) (*walletdomain.ChangeResult, error) {
	if userID <= 0 || appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID与应用ID不能为空")
	}
	if amount.IsZero() {
		return nil, apperrors.New(40080, http.StatusBadRequest, "调整金额不能为 0")
	}
	title := "管理员余额调整"
	if strings.TrimSpace(reason) == "" {
		reason = title
	}
	result, err := s.pg.ApplyWalletChange(ctx, walletdomain.Change{
		UserID:   userID,
		AppID:    appID,
		Type:     walletdomain.TxnTypeAdminAdjust,
		Amount:   amount,
		Title:    title,
		Remark:   strings.TrimSpace(reason),
		Operator: operator,
		ClientIP: clientIP,
	})
	return s.translateWalletError(result, err)
}

// AdminGetWallet 管理端查询任意用户钱包
func (s *WalletService) AdminGetWallet(ctx context.Context, userID int64, appID int64) (*walletdomain.Wallet, error) {
	w, err := s.pg.GetWalletByUser(ctx, userID, appID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return nil, err
	}
	return w, nil
}

// AdminListTransactions 管理端查询任意用户钱包流水
func (s *WalletService) AdminListTransactions(ctx context.Context, userID int64, appID int64, query walletdomain.ListQuery) (*walletdomain.ListResult, error) {
	page, limit := normalizePageLimit(query.Page, query.Limit, 200)
	items, total, err := s.pg.ListWalletTransactions(ctx, userID, appID, query.Type, page, limit)
	if err != nil {
		return nil, err
	}
	return &walletdomain.ListResult{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: calcPaymentTotalPages(total, limit),
	}, nil
}

// AdminListAppTransactions 管理端按**应用**分页查询流水（全用户）。
//
// 此前管理端只能按用户查（/users/:userId/wallet/transactions），
// 于是「这个应用今天的资金往来」只能靠一个个用户点过去 —— 对账根本无从下手。
func (s *WalletService) AdminListAppTransactions(ctx context.Context, appID int64, query walletdomain.AdminListQuery) (*walletdomain.AdminTransactionListResult, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	query.Page, query.Limit = normalizePageLimit(query.Page, query.Limit, 200)
	items, total, err := s.pg.ListWalletTransactionsByApp(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	return &walletdomain.AdminTransactionListResult{
		Items: items, Page: query.Page, Limit: query.Limit, Total: total,
		TotalPages: calcPaymentTotalPages(total, query.Limit),
	}, nil
}

// AdminStats 应用维度的资金面板（入账 / 出账 / 净额 / 余额合计 / 分类型）。
func (s *WalletService) AdminStats(ctx context.Context, appID int64, start *time.Time, end *time.Time) (*walletdomain.Stats, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	return s.pg.WalletStats(ctx, appID, start, end)
}

func (s *WalletService) translateWalletError(result *walletdomain.ChangeResult, err error) (*walletdomain.ChangeResult, error) {
	if err == nil {
		return result, nil
	}
	switch {
	case errors.Is(err, pgrepo.ErrInsufficientBalance):
		return nil, apperrors.New(40083, http.StatusBadRequest, "余额不足")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	default:
		return nil, err
	}
}

func walletIdemKey(appID int64, userID int64, key string) string {
	return "wallet:" + strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(userID, 10) + ":" + key
}

func normalizePageLimit(page int, limit int, maxLimit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return page, limit
}
