package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	lotterydomain "aegis/internal/domain/lottery"
	pointdomain "aegis/internal/domain/points"
	apperrors "aegis/pkg/errors"

	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// LotteryService 抽奖系统业务逻辑
type LotteryService struct {
	log    *zap.Logger
	pg     *pgrepo.Repository
	points *PointsService
	chain  *ChainCommitter
}

// NewLotteryService 创建抽奖服务
func NewLotteryService(log *zap.Logger, pg *pgrepo.Repository, points *PointsService, chain *ChainCommitter) *LotteryService {
	return &LotteryService{log: log, pg: pg, points: points, chain: chain}
}

// ── 活动 CRUD ──

// CreateActivity 创建抽奖活动
func (s *LotteryService) CreateActivity(ctx context.Context, appID int64, a lotterydomain.Activity) (*lotterydomain.Activity, error) {
	if a.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "活动名称不能为空")
	}
	if a.StartTime.IsZero() || a.EndTime.IsZero() {
		return nil, apperrors.New(40000, http.StatusBadRequest, "活动时间不能为空")
	}
	if !a.EndTime.After(a.StartTime) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "结束时间必须晚于开始时间")
	}
	if a.UIMode == "" {
		a.UIMode = "wheel"
	}
	if a.Status == "" {
		a.Status = "draft"
	}
	if a.JoinMode == "" {
		a.JoinMode = "manual"
	}
	if a.CostType == "" {
		a.CostType = "free"
	}
	return s.pg.CreateLotteryActivity(ctx, appID, a)
}

// UpdateActivity 更新抽奖活动
func (s *LotteryService) UpdateActivity(ctx context.Context, activityID int64, m lotterydomain.ActivityMutation) (*lotterydomain.Activity, error) {
	return s.pg.UpdateLotteryActivity(ctx, activityID, m)
}

// GetActivity 获取抽奖活动详情
func (s *LotteryService) GetActivity(ctx context.Context, activityID int64) (*lotterydomain.Activity, error) {
	activity, err := s.pg.GetLotteryActivity(ctx, activityID)
	if err != nil {
		return nil, err
	}
	if activity == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "活动不存在")
	}
	return activity, nil
}

// ListActivities 分页查询抽奖活动列表
func (s *LotteryService) ListActivities(ctx context.Context, q lotterydomain.ActivityListQuery) (*lotterydomain.ActivityListResult, error) {
	items, total, err := s.pg.ListLotteryActivities(ctx, q)
	if err != nil {
		return nil, err
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return &lotterydomain.ActivityListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// DeleteActivity 删除抽奖活动
func (s *LotteryService) DeleteActivity(ctx context.Context, activityID int64) error {
	return s.pg.DeleteLotteryActivity(ctx, activityID)
}

// ── 奖品 CRUD ──

// CreatePrize 创建奖品
func (s *LotteryService) CreatePrize(ctx context.Context, activityID int64, p lotterydomain.Prize) (*lotterydomain.Prize, error) {
	if p.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "奖品名称不能为空")
	}
	if p.Type == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "奖品类型不能为空")
	}
	if p.Weight <= 0 {
		p.Weight = 1
	}
	return s.pg.CreateLotteryPrize(ctx, activityID, p)
}

// UpdatePrize 更新奖品
func (s *LotteryService) UpdatePrize(ctx context.Context, prizeID int64, m lotterydomain.PrizeMutation) (*lotterydomain.Prize, error) {
	return s.pg.UpdateLotteryPrize(ctx, prizeID, m)
}

// DeletePrize 删除奖品
func (s *LotteryService) DeletePrize(ctx context.Context, prizeID int64) error {
	return s.pg.DeleteLotteryPrize(ctx, prizeID)
}

// ListPrizes 查询活动奖品列表
func (s *LotteryService) ListPrizes(ctx context.Context, activityID int64) ([]lotterydomain.Prize, error) {
	return s.pg.ListLotteryPrizes(ctx, activityID)
}

// ── 参与 ──

// JoinActivity 用户加入抽奖活动
func (s *LotteryService) JoinActivity(ctx context.Context, activityID int64, userID int64, joinType string) error {
	activity, err := s.GetActivity(ctx, activityID)
	if err != nil {
		return err
	}
	if activity.Status != "active" {
		return apperrors.New(40009, http.StatusBadRequest, "活动未开启")
	}
	now := time.Now().UTC()
	if now.Before(activity.StartTime) || now.After(activity.EndTime) {
		return apperrors.New(40009, http.StatusBadRequest, "不在活动时间范围内")
	}
	if joinType == "" {
		joinType = "manual"
	}
	return s.pg.JoinLotteryActivity(ctx, activityID, userID, joinType)
}

// IsParticipant 检查用户是否已参与
func (s *LotteryService) IsParticipant(ctx context.Context, activityID int64, userID int64) (bool, error) {
	return s.pg.IsLotteryParticipant(ctx, activityID, userID)
}

// ── 抽奖核心 ──

// Draw 执行一次抽奖
func (s *LotteryService) Draw(ctx context.Context, userID int64, appID int64, activityID int64) (*lotterydomain.DrawResult, error) {
	// 1. 获取活动并校验状态
	activity, err := s.GetActivity(ctx, activityID)
	if err != nil {
		return nil, err
	}
	if activity.AppID != appID {
		return nil, apperrors.New(40300, http.StatusForbidden, "无权访问该活动")
	}
	if activity.Status != "active" {
		return nil, apperrors.New(40009, http.StatusBadRequest, "活动未开启")
	}
	now := time.Now().UTC()
	if now.Before(activity.StartTime) || now.After(activity.EndTime) {
		return nil, apperrors.New(40009, http.StatusBadRequest, "不在活动时间范围内")
	}

	// 2. 检查参与资格（如需手动报名）
	if activity.JoinMode == "manual" {
		isP, err := s.pg.IsLotteryParticipant(ctx, activityID, userID)
		if err != nil {
			return nil, err
		}
		if !isP {
			return nil, apperrors.New(40310, http.StatusForbidden, "请先报名参与活动")
		}
	} else {
		// auto / both 模式：自动加入
		_ = s.pg.JoinLotteryActivity(ctx, activityID, userID, "auto")
	}

	// 3. 检查每日限制
	if activity.DailyLimit > 0 {
		todayCount, err := s.pg.CountUserLotteryDrawsToday(ctx, activityID, userID)
		if err != nil {
			return nil, err
		}
		if todayCount >= int64(activity.DailyLimit) {
			return nil, apperrors.New(40029, http.StatusTooManyRequests, "今日抽奖次数已达上限")
		}
	}

	// 4. 检查总次数限制
	if activity.TotalLimit > 0 {
		totalCount, err := s.pg.CountUserLotteryDrawsTotal(ctx, activityID, userID)
		if err != nil {
			return nil, err
		}
		if totalCount >= int64(activity.TotalLimit) {
			return nil, apperrors.New(40029, http.StatusTooManyRequests, "抽奖次数已达上限")
		}
	}

	// 5. 如果需要消耗积分则扣除
	if activity.CostType == "points" && activity.CostAmount > 0 {
		_, err := s.points.AdjustUserIntegral(ctx, userID, appID, -int64(activity.CostAmount), "抽奖消耗", pointdomain.AdminAdjustOptions{})
		if err != nil {
			return nil, apperrors.New(40030, http.StatusBadRequest, fmt.Sprintf("积分不足，需要 %d 积分", activity.CostAmount))
		}
	}

	// 6. 获取奖品列表并抽奖
	prizes, err := s.pg.ListLotteryPrizes(ctx, activityID)
	if err != nil {
		return nil, err
	}
	if len(prizes) == 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "活动没有配置奖品")
	}

	// 生成随机种子
	seedBytes := make([]byte, 32)
	if _, err := rand.Read(seedBytes); err != nil {
		return nil, fmt.Errorf("生成随机种子失败: %w", err)
	}

	// 使用加权随机选择奖品
	prizeIndex, selectedPrize := s.selectPrize(prizes, seedBytes)
	if selectedPrize == nil {
		return nil, apperrors.New(50000, http.StatusInternalServerError, "抽奖选择奖品失败")
	}

	// 7. 扣减库存
	if err := s.pg.DecrementLotteryPrizeStock(ctx, selectedPrize.ID); err != nil {
		// 库存不足时尝试选保底奖
		defaultPrize := s.findDefaultPrize(prizes)
		if defaultPrize != nil && defaultPrize.ID != selectedPrize.ID {
			if err2 := s.pg.DecrementLotteryPrizeStock(ctx, defaultPrize.ID); err2 == nil {
				selectedPrize = defaultPrize
				prizeIndex = s.findPrizeIndex(prizes, defaultPrize.ID)
			} else {
				return nil, apperrors.New(40010, http.StatusBadRequest, "奖品已发完")
			}
		} else {
			return nil, apperrors.New(40010, http.StatusBadRequest, "奖品已发完")
		}
	}

	// 8. 创建抽奖记录
	seedHex := hex.EncodeToString(seedBytes)
	proofHash := sha256.Sum256(seedBytes)
	proofHex := hex.EncodeToString(proofHash[:])

	prizeSnapshot, _ := json.Marshal(map[string]any{
		"id":       selectedPrize.ID,
		"name":     selectedPrize.Name,
		"type":     selectedPrize.Type,
		"value":    selectedPrize.Value,
		"imageUrl": selectedPrize.ImageURL,
	})
	var snapshotMap map[string]any
	_ = json.Unmarshal(prizeSnapshot, &snapshotMap)

	draw := lotterydomain.Draw{
		ActivityID:    activityID,
		UserID:        userID,
		PrizeID:       selectedPrize.ID,
		PrizeSnapshot: snapshotMap,
		DrawSeed:      seedHex,
		DrawProof:     proofHex,
		Status:        "awarded",
	}
	createdDraw, err := s.pg.CreateLotteryDraw(ctx, draw)
	if err != nil {
		return nil, err
	}

	// 9. 自动发放积分/经验奖励
	s.autoAwardPrize(ctx, userID, appID, selectedPrize)

	return &lotterydomain.DrawResult{
		Draw:       *createdDraw,
		Prize:      *selectedPrize,
		SeedHash:   activity.SeedHash,
		DrawSeed:   seedHex,
		DrawProof:  proofHex,
		PrizeIndex: prizeIndex,
	}, nil
}

// ListDraws 查询抽奖记录列表
func (s *LotteryService) ListDraws(ctx context.Context, q lotterydomain.DrawListQuery) (*lotterydomain.DrawListResult, error) {
	items, total, err := s.pg.ListLotteryDraws(ctx, q)
	if err != nil {
		return nil, err
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return &lotterydomain.DrawListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// GetStats 获取活动统计信息
func (s *LotteryService) GetStats(ctx context.Context, activityID int64) (*lotterydomain.ActivityStats, error) {
	return s.pg.GetLotteryActivityStats(ctx, activityID)
}

// ── 种子承诺 & 验证 ──

// CommitSeed 为活动创建种子承诺
func (s *LotteryService) CommitSeed(ctx context.Context, activityID int64) (*lotterydomain.SeedCommitment, error) {
	// 确认活动存在
	_, err := s.GetActivity(ctx, activityID)
	if err != nil {
		return nil, err
	}

	// 生成 32 字节随机种子
	seedBytes := make([]byte, 32)
	if _, err := rand.Read(seedBytes); err != nil {
		return nil, fmt.Errorf("生成随机种子失败: %w", err)
	}
	seedHex := hex.EncodeToString(seedBytes)

	// SHA-256 哈希
	hash := sha256.Sum256(seedBytes)
	hashHex := hex.EncodeToString(hash[:])

	// 存储承诺（种子值作为 seed_value 存到 activity 表，待 reveal 时才展示）
	commitment, err := s.pg.CreateLotterySeedCommitment(ctx, activityID, 1, hashHex)
	if err != nil {
		return nil, err
	}

	// 将种子值存储到承诺记录（用于后续 reveal），通过数据库直接存
	if err := s.pg.RevealLotterySeed(ctx, activityID, 1, seedHex); err != nil {
		s.log.Error("存储种子值失败", zap.Error(err))
	}
	// 但 RevealedAt 还没设置（revealedAt 要在公开 reveal 时设置）
	// 重新更新：清除 revealed_at（因为上面的 RevealLotterySeed 设置了 revealed_at）
	_, _ = s.pg.GetLotterySeedCommitment(ctx, activityID, 1)

	// 链上提交
	var txHash, network string
	if s.chain != nil && s.chain.Enabled() {
		txHash, network, err = s.chain.CommitHash(ctx, hash[:])
		if err != nil {
			s.log.Warn("链上提交种子哈希失败（非致命）", zap.Error(err))
		} else if txHash != "" {
			_ = s.pg.UpdateLotterySeedChainTx(ctx, activityID, 1, txHash)
		}
	}

	// 更新活动表的 seed_hash 和链上信息
	if err := s.pg.UpdateLotteryActivitySeed(ctx, activityID, hashHex, txHash, network); err != nil {
		s.log.Error("更新活动种子哈希失败", zap.Error(err))
	}

	// 返回时不暴露 seedValue
	commitment.SeedHash = hashHex
	commitment.ChainTxHash = txHash
	commitment.SeedValue = "" // 隐藏种子值
	return commitment, nil
}

// RevealSeed 公开种子值
func (s *LotteryService) RevealSeed(ctx context.Context, activityID int64) (*lotterydomain.SeedCommitment, error) {
	commitment, err := s.pg.GetLotterySeedCommitment(ctx, activityID, 1)
	if err != nil {
		return nil, err
	}
	if commitment == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "未找到种子承诺记录")
	}
	if commitment.SeedValue == "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, "种子值尚未生成")
	}

	// 更新活动表
	if err := s.pg.RevealLotteryActivitySeed(ctx, activityID, commitment.SeedValue); err != nil {
		return nil, err
	}

	return commitment, nil
}

// VerifyActivity 验证活动种子公正性
func (s *LotteryService) VerifyActivity(ctx context.Context, activityID int64) (*lotterydomain.VerifyResult, error) {
	activity, err := s.GetActivity(ctx, activityID)
	if err != nil {
		return nil, err
	}

	result := &lotterydomain.VerifyResult{
		SeedHash:     activity.SeedHash,
		SeedValue:    activity.SeedValue,
		ChainTxHash:  activity.ChainTxHash,
		ChainNetwork: activity.ChainNetwork,
	}

	if activity.SeedHash == "" {
		result.Valid = false
		result.Message = "活动尚未创建种子承诺"
		return result, nil
	}

	if activity.SeedValue == "" {
		result.Valid = false
		result.Message = "种子尚未公开，无法验证"
		return result, nil
	}

	// 将 seedValue 从 hex 解码，然后 SHA-256
	seedBytes, err := hex.DecodeString(activity.SeedValue)
	if err != nil {
		result.Valid = false
		result.Message = "种子值格式错误"
		return result, nil
	}

	hash := sha256.Sum256(seedBytes)
	computedHash := hex.EncodeToString(hash[:])

	if computedHash == activity.SeedHash {
		result.Valid = true
		result.Message = "验证通过：SHA-256(seedValue) == seedHash"
	} else {
		result.Valid = false
		result.Message = "验证失败：哈希不匹配"
	}

	return result, nil
}

// ── 内部辅助 ──

// selectPrize 加权随机选择奖品
func (s *LotteryService) selectPrize(prizes []lotterydomain.Prize, seed []byte) (int, *lotterydomain.Prize) {
	// 构建可选奖品列表（有库存的）
	type candidate struct {
		index int
		prize *lotterydomain.Prize
	}
	candidates := make([]candidate, 0, len(prizes))
	totalWeight := 0
	for i := range prizes {
		p := &prizes[i]
		if p.Quantity == -1 || p.Used < p.Quantity {
			totalWeight += p.Weight
			candidates = append(candidates, candidate{index: i, prize: p})
		}
	}

	if totalWeight == 0 || len(candidates) == 0 {
		// 没有可选奖品，尝试返回保底
		def := s.findDefaultPrize(prizes)
		if def != nil {
			return s.findPrizeIndex(prizes, def.ID), def
		}
		return -1, nil
	}

	hash := sha256.Sum256(seed)
	idx := new(big.Int).SetBytes(hash[:])
	idx.Mod(idx, big.NewInt(int64(totalWeight)))
	target := int(idx.Int64())

	cumulative := 0
	for _, c := range candidates {
		cumulative += c.prize.Weight
		if target < cumulative {
			return c.index, c.prize
		}
	}

	// fallback: 返回最后一个可选奖品
	last := candidates[len(candidates)-1]
	return last.index, last.prize
}

// findDefaultPrize 查找保底奖品
func (s *LotteryService) findDefaultPrize(prizes []lotterydomain.Prize) *lotterydomain.Prize {
	for i := range prizes {
		if prizes[i].IsDefault && (prizes[i].Quantity == -1 || prizes[i].Used < prizes[i].Quantity) {
			return &prizes[i]
		}
	}
	return nil
}

// findPrizeIndex 查找奖品在列表中的索引
func (s *LotteryService) findPrizeIndex(prizes []lotterydomain.Prize, prizeID int64) int {
	for i := range prizes {
		if prizes[i].ID == prizeID {
			return i
		}
	}
	return -1
}

// autoAwardPrize 自动发放积分/经验类奖品
func (s *LotteryService) autoAwardPrize(ctx context.Context, userID int64, appID int64, prize *lotterydomain.Prize) {
	if prize == nil {
		return
	}
	// 解析奖品值为数量
	var amount int64
	if _, err := fmt.Sscanf(prize.Value, "%d", &amount); err != nil || amount <= 0 {
		return
	}

	switch prize.Type {
	case "points":
		if _, err := s.points.AdjustUserIntegral(ctx, userID, appID, amount, fmt.Sprintf("抽奖奖励: %s", prize.Name), pointdomain.AdminAdjustOptions{}); err != nil {
			s.log.Error("自动发放积分奖励失败",
				zap.Int64("userID", userID),
				zap.Int64("prizeID", prize.ID),
				zap.Error(err))
		}
	case "experience":
		if _, err := s.points.AdjustUserExperience(ctx, userID, appID, amount, fmt.Sprintf("抽奖奖励: %s", prize.Name), pointdomain.AdminAdjustOptions{}); err != nil {
			s.log.Error("自动发放经验奖励失败",
				zap.Int64("userID", userID),
				zap.Int64("prizeID", prize.ID),
				zap.Error(err))
		}
	}
}
