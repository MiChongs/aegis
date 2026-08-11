package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	authdomain "aegis/internal/domain/auth"
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
}

func NewWalletService(log *zap.Logger, pg *pgrepo.Repository) *WalletService {
	return &WalletService{log: log, pg: pg}
}

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
	return s.translateWalletError(result, err)
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
