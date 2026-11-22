package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"aegis/internal/domain/cosmetic"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 自定义工坊（docs/cosmetics.md「自定义工坊」）：用户用官方零件拼自己的装扮。规则见 internal/domain/cosmetic/workshop.go。

const (
	errCodeWorkshopUnavailable = 40362 // 工坊未开启或没有工坊额度
	errCodeWorkshopTakenDown   = 40363 // 作品已被下架，不能修改
	errCodeWorkshopQuotaFull   = 40954 // 作品数已达额度
	errCodeWorkshopPartGone    = 40429 // 零件不存在
	errCodeWorkshopPartDup     = 40953 // 这份素材已经是零件了
)

func mapWorkshopError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrWorkshopQuotaFull):
		return apperrors.New(errCodeWorkshopQuotaFull, http.StatusConflict, "作品数已达上限，删掉一件再新建吧")
	case errors.Is(err, pgrepo.ErrWorkshopTakenDown):
		return apperrors.New(errCodeWorkshopTakenDown, http.StatusForbidden, "这件作品已被下架，不能再修改")
	case errors.Is(err, pgrepo.ErrWorkshopPartNotFound):
		return apperrors.New(errCodeWorkshopPartGone, http.StatusNotFound, "零件不存在")
	case errors.Is(err, pgrepo.ErrWorkshopPartDuplicate):
		return apperrors.New(errCodeWorkshopPartDup, http.StatusConflict, "这份素材已经登记为零件")
	}
	return mapCosmeticError(err)
}

// applyWorkshopFacts 把「是谁」「工坊额度」填进判定事实（工坊作品只有作者本人、且有额度时能戴）。
func applyWorkshopFacts(settings cosmetic.Settings, f *pgrepo.CosmeticUserFacts) {
	if f == nil {
		return
	}
	f.Facts.UserID = f.UserID
	f.Facts.WorkshopQuota = settings.Workshop.QuotaFor(f.Facts)
}

// workshopItemsFor 身上戴着的工坊作品（不在上架装扮缓存里的那些编号），素材地址已解析。
func (s *CosmeticService) workshopItemsFor(ctx context.Context, appID int64, entry cosmeticLiveEntry, facts ...*pgrepo.CosmeticUserFacts) map[int64]cosmetic.Item {
	ids := make([]int64, 0)
	for _, f := range facts {
		if f == nil {
			continue
		}
		for _, e := range f.Equips {
			if _, ok := entry.byID[e.CosmeticID]; !ok {
				ids = append(ids, e.CosmeticID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	items, err := s.pg.GetWorkshopItems(ctx, appID, ids)
	if err != nil {
		s.log.Warn("load workshop items failed", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	s.fillItemsAssets(ctx, appID, items)
	return items
}

// fillItemsAssets 一次取齐这些装扮引用的素材并解析地址。
func (s *CosmeticService) fillItemsAssets(ctx context.Context, appID int64, items map[int64]cosmetic.Item) {
	assetIDs := make([]int64, 0)
	for _, it := range items {
		if it.Design != nil {
			assetIDs = append(assetIDs, it.Design.AssetIDs()...)
		}
	}
	if len(assetIDs) == 0 {
		return
	}
	assets, err := s.pg.GetCosmeticAssets(ctx, appID, assetIDs)
	if err != nil {
		s.log.Warn("load workshop assets failed", zap.Int64("appid", appID), zap.Error(err))
		return
	}
	for id, it := range items {
		s.fillDesignAssets(ctx, appID, it.Design, assets)
		items[id] = it
	}
}

// workshopView 工坊作品给客户端的视图：名称只给作者本人看。
func workshopView(item cosmetic.Item, viewer int64, baseURL string) cosmetic.View {
	view := absoluteView(item, baseURL)
	if item.CreatorUserID != viewer {
		view.Name = cosmetic.WorkshopDisplayName
		view.Code = ""
	}
	return view
}

// workshopAccess 这个人此刻的工坊额度与已用数；工坊未开启时报 40361 / 40362。
func (s *CosmeticService) workshopAccess(ctx context.Context, appID, userID int64) (cosmeticLiveEntry, *pgrepo.CosmeticUserFacts, int, error) {
	entry, err := s.live(ctx, appID)
	if err != nil {
		return entry, nil, 0, err
	}
	if err := s.ensureEnabled(entry); err != nil {
		return entry, nil, 0, err
	}
	if !entry.settings.Workshop.Enabled {
		return entry, nil, 0, apperrors.New(errCodeWorkshopUnavailable, http.StatusForbidden, "自定义工坊暂未开放")
	}
	f, err := s.userFacts(ctx, appID, userID)
	if err != nil {
		return entry, nil, 0, err
	}
	applyWorkshopFacts(entry.settings, f)
	return entry, f, f.Facts.WorkshopQuota, nil
}

// workshopParts 启用的零件（以素材编号为键，校验作品用）与带素材地址的列表（给编辑器）。
func (s *CosmeticService) workshopParts(ctx context.Context, appID int64, onlyEnabled bool, baseURL string) (map[int64]cosmetic.Part, []cosmetic.Part, error) {
	parts, err := s.pg.ListWorkshopParts(ctx, appID, onlyEnabled)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.AssetID)
	}
	assets, err := s.pg.GetCosmeticAssets(ctx, appID, ids)
	if err != nil {
		return nil, nil, err
	}
	byAsset := make(map[int64]cosmetic.Part, len(parts))
	out := make([]cosmetic.Part, 0, len(parts))
	for _, p := range parts {
		a, ok := assets[p.AssetID]
		if !ok {
			continue
		}
		a.URL = absoluteLink(baseURL, s.storageLink(ctx, appID, a.Reference))
		a.UsedBy = 0
		p.Asset = &a
		byAsset[p.AssetID] = p
		out = append(out, p)
	}
	return byAsset, out, nil
}

// prepareWorkshopDesign 规整并校验一份作品设计稿。
func (s *CosmeticService) prepareWorkshopDesign(ctx context.Context, appID int64, slot string, d cosmetic.Design) (cosmetic.Design, error) {
	if _, ok := cosmetic.FindSlot(slot); !ok {
		return d, badRequest(fmt.Errorf("未登记的槽位：%s", slot))
	}
	d = cosmetic.NormalizeDesign(slot, d)
	parts, _, err := s.workshopParts(ctx, appID, true, "")
	if err != nil {
		return d, err
	}
	if err := cosmetic.ValidateWorkshopDesign(slot, d, parts); err != nil {
		return d, badRequest(err)
	}
	if err := s.checkDesignAssets(ctx, appID, d); err != nil {
		return d, err
	}
	return d, nil
}

// workshopStarters 新建作品时可以套用的起点：内置模板里不带文字图层的那些。
func workshopStarters() []cosmetic.Template {
	out := make([]cosmetic.Template, 0)
	for _, t := range cosmetic.Templates() {
		ok := true
		for _, l := range t.Design.Layers {
			if l.Type == cosmetic.LayerText || l.Asset != "" {
				ok = false
				break
			}
		}
		if spec, found := cosmetic.FindSlot(t.Slot); found && spec.Layered && t.Design.Canvas != spec.Canvas {
			ok = false
		}
		if ok {
			out = append(out, t)
		}
	}
	return out
}

// Workshop 工坊首页：我的额度、编辑器上界、零件、起点模板、我的作品。
func (s *CosmeticService) Workshop(ctx context.Context, appID, userID int64, baseURL string) (map[string]any, error) {
	entry, f, quota, err := s.workshopAccess(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	_, parts, err := s.workshopParts(ctx, appID, true, baseURL)
	if err != nil {
		return nil, err
	}
	items, err := s.pg.ListUserWorkshopItems(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]cosmetic.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	s.fillItemsAssets(ctx, appID, byID)
	equipped := make(map[int64]cosmetic.Equip, len(f.Equips))
	for _, e := range f.Equips {
		equipped[e.CosmeticID] = e
	}
	out := make([]cosmetic.WorkshopItem, 0, len(items))
	for _, it := range items {
		it = byID[it.ID]
		w := cosmetic.WorkshopItem{View: workshopView(it, userID, baseURL), Status: it.Status, TakedownReason: it.TakedownReason,
			CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt}
		if e, ok := equipped[it.ID]; ok && it.Status == cosmetic.StatusActive && quota > 0 {
			w.Equipped, w.Position = true, e.Position
		}
		out = append(out, w)
	}
	reason := ""
	if quota <= 0 {
		reason = cosmetic.ReasonNeedWorkshop
	}
	return map[string]any{
		"settings": map[string]any{
			"quota": quota, "used": len(items), "canCreate": quota > len(items), "reason": reason,
			"badgeSlots": entry.settings.BadgeSlots,
			"freeQuota":  entry.settings.Workshop.FreeQuota, "vipQuota": entry.settings.Workshop.VIPQuota,
		},
		"slots":      cosmetic.Slots(),
		"vocabulary": cosmetic.DesignVocabulary(),
		"limits":     cosmetic.WorkshopLimits(),
		"parts":      parts,
		"starters":   workshopStarters(),
		"items":      out,
	}, nil
}

// CreateWorkshopItem 新建作品。
func (s *CosmeticService) CreateWorkshopItem(ctx context.Context, appID, userID int64, in cosmetic.WorkshopInput, baseURL string) (*cosmetic.WorkshopItem, error) {
	_, _, quota, err := s.workshopAccess(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if quota <= 0 {
		return nil, apperrors.New(errCodeWorkshopUnavailable, http.StatusForbidden, "开通会员后即可使用自定义工坊").
			WithData(map[string]any{"reason": cosmetic.ReasonNeedWorkshop})
	}
	in.Slot = strings.TrimSpace(in.Slot)
	in.Name = strings.TrimSpace(in.Name)
	if err := cosmetic.ValidateWorkshopName(in.Name); err != nil {
		return nil, badRequest(err)
	}
	design, err := s.prepareWorkshopDesign(ctx, appID, in.Slot, in.Design)
	if err != nil {
		return nil, err
	}
	in.Design = design
	item, err := s.pg.CreateWorkshopItem(ctx, appID, userID, quota, in)
	if err != nil {
		return nil, mapWorkshopError(err)
	}
	return s.workshopResult(ctx, appID, userID, *item, baseURL), nil
}

// UpdateWorkshopItem 修改作品。额度失效时也允许改（只是戴不上），被下架的不能改。
func (s *CosmeticService) UpdateWorkshopItem(ctx context.Context, appID, userID, id int64, name string, d cosmetic.Design, baseURL string) (*cosmetic.WorkshopItem, error) {
	if _, _, _, err := s.workshopAccess(ctx, appID, userID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetWorkshopItems(ctx, appID, []int64{id})
	if err != nil {
		return nil, err
	}
	item, ok := current[id]
	if !ok || item.CreatorUserID != userID {
		return nil, mapCosmeticError(pgrepo.ErrCosmeticNotFound)
	}
	if item.Status != cosmetic.StatusActive {
		return nil, mapWorkshopError(pgrepo.ErrWorkshopTakenDown)
	}
	name = strings.TrimSpace(name)
	if err := cosmetic.ValidateWorkshopName(name); err != nil {
		return nil, badRequest(err)
	}
	design, err := s.prepareWorkshopDesign(ctx, appID, item.Slot, d)
	if err != nil {
		return nil, err
	}
	updated, err := s.pg.UpdateWorkshopItem(ctx, appID, userID, id, name, design)
	if err != nil {
		return nil, mapWorkshopError(err)
	}
	s.publishChanged(ctx, appID, userID, "equipped")
	return s.workshopResult(ctx, appID, userID, *updated, baseURL), nil
}

// DeleteWorkshopItem 删除作品。
func (s *CosmeticService) DeleteWorkshopItem(ctx context.Context, appID, userID, id int64) error {
	if err := s.pg.DeleteWorkshopItem(ctx, appID, userID, id); err != nil {
		return mapWorkshopError(err)
	}
	s.publishChanged(ctx, appID, userID, "unequipped")
	return nil
}

func (s *CosmeticService) workshopResult(ctx context.Context, appID, userID int64, item cosmetic.Item, baseURL string) *cosmetic.WorkshopItem {
	item.Draft = nil
	m := map[int64]cosmetic.Item{item.ID: item}
	s.fillItemsAssets(ctx, appID, m)
	item = m[item.ID]
	out := &cosmetic.WorkshopItem{View: workshopView(item, userID, baseURL), Status: item.Status,
		TakedownReason: item.TakedownReason, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if equips, err := s.pg.GetUserCosmeticEquips(ctx, appID, userID); err == nil {
		for _, e := range equips {
			if e.CosmeticID == item.ID {
				out.Equipped, out.Position = true, e.Position
			}
		}
	}
	return out
}

// ── 管理端 ──

// AdminWorkshopParts 全部零件（含停用的）。
func (s *CosmeticService) AdminWorkshopParts(ctx context.Context, appID int64) ([]cosmetic.Part, error) {
	_, parts, err := s.workshopParts(ctx, appID, false, "")
	return parts, err
}

func (s *CosmeticService) checkPart(ctx context.Context, appID int64, in cosmetic.PartInput) (cosmetic.PartInput, error) {
	in = cosmetic.NormalizePartInput(in)
	assets, err := s.pg.GetCosmeticAssets(ctx, appID, []int64{in.AssetID})
	if err != nil {
		return in, err
	}
	asset, ok := assets[in.AssetID]
	if !ok {
		return in, mapCosmeticError(pgrepo.ErrCosmeticAssetNotFound)
	}
	if err := cosmetic.ValidatePartInput(in, asset); err != nil {
		return in, badRequest(err)
	}
	if in.FrameCount > 0 && asset.ContentType == "image/svg+xml" {
		return in, badRequest(fmt.Errorf("序列帧不能使用 SVG 素材"))
	}
	return in, nil
}

// CreateWorkshopPart 登记零件。
func (s *CosmeticService) CreateWorkshopPart(ctx context.Context, appID int64, in cosmetic.PartInput) (*cosmetic.Part, error) {
	in, err := s.checkPart(ctx, appID, in)
	if err != nil {
		return nil, err
	}
	if n, err := s.pg.CountWorkshopParts(ctx, appID); err != nil {
		return nil, err
	} else if n >= cosmetic.MaxWorkshopParts {
		return nil, badRequest(fmt.Errorf("每个应用最多 %d 个零件", cosmetic.MaxWorkshopParts))
	}
	p, err := s.pg.CreateWorkshopPart(ctx, appID, in)
	return p, mapWorkshopError(err)
}

// UpdateWorkshopPart 修改零件。
func (s *CosmeticService) UpdateWorkshopPart(ctx context.Context, appID, id int64, in cosmetic.PartInput) (*cosmetic.Part, error) {
	in, err := s.checkPart(ctx, appID, in)
	if err != nil {
		return nil, err
	}
	p, err := s.pg.UpdateWorkshopPart(ctx, appID, id, in)
	return p, mapWorkshopError(err)
}

// DeleteWorkshopPart 删零件。
func (s *CosmeticService) DeleteWorkshopPart(ctx context.Context, appID, id int64) error {
	return mapWorkshopError(s.pg.DeleteWorkshopPart(ctx, appID, id))
}

// AdminWorkshopItems 作品列表（带作者，设计稿素材已解析）。
func (s *CosmeticService) AdminWorkshopItems(ctx context.Context, f pgrepo.WorkshopItemFilter) (map[string]any, error) {
	items, total, err := s.pg.ListWorkshopItemsAdmin(ctx, f)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]cosmetic.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it.Item
	}
	s.fillItemsAssets(ctx, f.AppID, byID)
	for i := range items {
		items[i].Item = byID[items[i].ID]
	}
	return map[string]any{"items": items, "total": total}, nil
}

// SetWorkshopItemStatus 下架（带原因）或恢复一件作品；作者身上的会随读取时的判定消失。
func (s *CosmeticService) SetWorkshopItemStatus(ctx context.Context, appID, id int64, takedown bool, reason string) (*cosmetic.Item, error) {
	status := cosmetic.StatusActive
	if takedown {
		status = cosmetic.StatusArchived
		if strings.TrimSpace(reason) == "" {
			return nil, badRequest(fmt.Errorf("请填写下架原因（作者会看到）"))
		}
	} else {
		reason = ""
	}
	item, err := s.pg.SetWorkshopItemStatus(ctx, appID, id, status, reason)
	if err != nil {
		return nil, mapWorkshopError(err)
	}
	action := "equipped"
	if takedown {
		action = "revoked"
	}
	s.publishChanged(ctx, appID, item.CreatorUserID, action)
	return item, nil
}
