package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 会员档位（docs/vip-tiers.md）：权益只属于档位，套餐只是档位的售卖规格。

const (
	errCodeVipTierNotFound = 40499 // 404：档位不存在（或迁往的档位不存在 / 就是它自己）
	errCodeVipTierInUse    = 40996 // 409：档位下还有套餐或会员，删除时必须指定迁往的档位
)

// AdminListTiers 应用的全部档位（带档位下的套餐、在期会员数与默认永久档位标记）。
func (s *VipService) AdminListTiers(ctx context.Context, appID int64) ([]vipdomain.Tier, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	s.fillAutoPerks(ctx, appID, tiers)
	return tiers, nil
}

// AdminSaveTier 新建或更新档位。改功能即对档位下全部在期会员生效。
func (s *VipService) AdminSaveTier(ctx context.Context, mutation vipdomain.TierMutation) (*vipdomain.Tier, error) {
	if mutation.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	if mutation.Name != nil {
		trimmed := strings.TrimSpace(*mutation.Name)
		mutation.Name = &trimmed
	}
	if mutation.ID == 0 && (mutation.Name == nil || *mutation.Name == "") {
		return nil, apperrors.New(40000, http.StatusBadRequest, "档位名称不能为空")
	}
	if mutation.ID > 0 && mutation.Name != nil && *mutation.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "档位名称不能为空")
	}
	// 与套餐同理：拼错的标识不会有任何提示，直到会员来问「为什么买了还是用不了」
	if mutation.Features != nil {
		if err := s.EnsureFeatureTagsRegistered(ctx, mutation.AppID, *mutation.Features); err != nil {
			return nil, err
		}
	}
	if mutation.Perks != nil {
		perks, err := vipdomain.NormalizeCustomPerks(*mutation.Perks)
		if err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		mutation.Perks = &perks
	}
	tier, err := s.pg.UpsertVipTier(ctx, mutation)
	if err != nil {
		return nil, translateVipTierError(err)
	}
	if settings, err := s.pg.GetAIGatewaySettings(ctx, mutation.AppID); err == nil {
		tier.AutoPerks = aiMemberPerks(*settings, tier.ID, tier.Name)
	}
	return tier, nil
}

// AdminDeleteTier 删除档位。档位下还有套餐或在期会员时必须给 moveTo（迁往的档位），
// 套餐与全部开通记录改挂目标档位，会员从此按目标档位的功能算。
func (s *VipService) AdminDeleteTier(ctx context.Context, appID, tierID, moveTo int64) error {
	if appID <= 0 || tierID <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "应用ID与档位ID不能为空")
	}
	if err := s.pg.DeleteVipTier(ctx, appID, tierID, moveTo); err != nil {
		return translateVipTierError(err)
	}
	s.log.Info("vip tier deleted", zap.Int64("appid", appID), zap.Int64("tierId", tierID), zap.Int64("moveTo", moveTo))
	return nil
}

// translateVipTierError 仓储的档位错误 → 业务错误。
func translateVipTierError(err error) error {
	var inUse *pgrepo.VipTierInUseError
	switch {
	case errors.Is(err, pgrepo.ErrVipTierNotFound):
		return apperrors.New(errCodeVipTierNotFound, http.StatusNotFound, "会员档位不存在")
	case errors.As(err, &inUse):
		return apperrors.New(errCodeVipTierInUse, http.StatusConflict,
			fmt.Sprintf("档位下还有 %d 个套餐、%d 位在期会员，请指定迁往的档位", inUse.Plans, inUse.Members))
	default:
		return err
	}
}
