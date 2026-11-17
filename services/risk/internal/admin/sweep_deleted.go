package admin

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"aegis/services/risk/internal/backfill"

	"github.com/jackc/pgx/v5"
)

// 已删除账号清扫。
//
// 账号在主服务删除时会通知风控收尾（subjects/forget），但删号有好几条路径（管理员删除、批量删除、账号注销、风控关联删除），
// 通知也可能因为风控暂时不可用而失败；改造之前删掉的账号更是从没收过尾。清扫按主库为准：
// 用户 ID 序列里的空缺就是删掉的账号，到关系图里核对还有没有它的点或边，有就按删号收尾——
// 删用户点与只属于它的登录名、手机号、设备等，设备与 IP 按剩下的使用者重写，图特征缓存与滑窗计数回退，待复核单结案。
// 每次回灌之后、服务启动时各跑一次；可重复执行，没有残留时只是几次查询。

var sweepMu sync.Mutex

// errSweepRunning 已有一次清扫在跑
var errSweepRunning = errors.New("已有一次清扫在进行")

// SweepDeletedUsers 清扫一次，返回收尾的账号数
func (s *Server) SweepDeletedUsers(ctx context.Context) (int, error) {
	if s.Graph == nil || s.Cfg.PGAdminDSN == "" {
		return 0, nil
	}
	if !sweepMu.TryLock() {
		return 0, errSweepRunning
	}
	defer sweepMu.Unlock()
	conn, err := pgx.Connect(ctx, s.Cfg.PGAdminDSN)
	if err != nil {
		return 0, err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		return 0, err
	}
	ids, apps, err := backfill.DeletedCandidates(ctx, conn)
	if err != nil {
		return 0, fmt.Errorf("读主库账号：%w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// 整个应用删掉以后主库里查不到这个应用了，从风控流水里补上出现过的应用
	if s.CH != nil {
		var seen []struct {
			AppID int64 `ch:"app_id"`
		}
		if err := s.CH.Conn().Select(ctx, &seen, `SELECT DISTINCT app_id FROM decisions`); err == nil {
			known := map[int64]bool{}
			for _, a := range apps {
				known[a] = true
			}
			for _, r := range seen {
				if r.AppID > 0 && !known[r.AppID] {
					apps = append(apps, r.AppID)
					known[r.AppID] = true
				}
			}
		}
	}
	total := 0
	var errs []error
	for _, appID := range apps {
		present, err := s.Graph.PresentUsers(appID, ids)
		if err != nil {
			errs = append(errs, fmt.Errorf("应用 %d：%w", appID, err))
			continue
		}
		if len(present) == 0 {
			continue
		}
		// 删点之前再核对一次主库：空缺号算出来以后才注册的账号不能误删
		live, err := backfill.Existing(ctx, conn, present)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var gone []int64
		for _, uid := range present {
			if !live[uid] {
				gone = append(gone, uid)
			}
		}
		for i := 0; i < len(gone); i += 20 {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			chunk := gone[i:min(i+20, len(gone))]
			nb := s.collectNeighborhood(ctx, appID, chunk)
			closed, removed, problems := s.finishDeleted(ctx, nb, "system-sweep", "账号已在主服务删除")
			total += len(chunk)
			s.Store.Audit(ctx, "system-sweep", "subjects.sweep", fmt.Sprintf("app %d users %v", appID, chunk), map[string]any{
				"casesClosed": closed, "graphRemoved": removed, "countersCleared": nb.countersCleared, "problems": nonNilSlice(problems)})
			if len(problems) > 0 {
				s.logf("已删除账号清扫：应用 %d 账号 %v 收尾有问题：%v", appID, chunk, problems)
			}
		}
	}
	if total > 0 {
		s.logf("已删除账号清扫：%d 个账号已从关系图与计数中收尾", total)
	}
	return total, errors.Join(errs...)
}
