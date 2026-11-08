package admin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"aegis/services/risk/internal/backfill"

	"github.com/cloudwego/hertz/pkg/app"
)

// backfillState 正在跑的回灌（同一时间只跑一个）
type backfillState struct {
	mu      sync.Mutex
	running bool
	id      int64
	current backfill.Progress
}

var bf backfillState

type sourceInfo struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

func sourceList() []sourceInfo {
	var out []sourceInfo
	for _, src := range backfill.Sources() {
		k, l, d := src.Info()
		out = append(out, sourceInfo{Key: k, Label: l, Detail: d})
	}
	return out
}

// getBackfill 回灌状态：正在跑的实时进度，否则最近一次的记录
func (s *Server) getBackfill(ctx context.Context, _ *app.RequestContext) (any, error) {
	bf.mu.Lock()
	running, id, cur := bf.running, bf.id, bf.current
	bf.mu.Unlock()
	out := map[string]any{"running": running, "sources": sourceList()}
	if running {
		out["run"] = map[string]any{"id": id, "status": "running", "progress": cur, "startedAt": cur.StartedAt}
		return out, nil
	}
	last, err := s.Store.LastBackfill(ctx)
	if err != nil {
		return nil, err
	}
	if last != nil {
		var p backfill.Progress
		_ = json.Unmarshal(last.Progress, &p)
		out["run"] = map[string]any{"id": last.ID, "status": last.Status, "operator": last.Operator, "progress": p, "startedAt": last.StartedAt, "finishedAt": last.FinishedAt}
	}
	return out, nil
}

// startBackfill 开始一次回灌（已在跑时直接返回当前这次）
func (s *Server) startBackfill(ctx context.Context, c *app.RequestContext) (any, error) {
	id, started, err := s.StartBackfill(ctx, operator(c))
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "started": started}, nil
}

// StartBackfill 在后台跑一次回灌；返回本次（或正在跑的那次）ID 与是否新开
func (s *Server) StartBackfill(ctx context.Context, op string) (int64, bool, error) {
	if s.Graph == nil {
		return 0, false, bad("关系图未就绪")
	}
	bf.mu.Lock()
	if bf.running {
		id := bf.id
		bf.mu.Unlock()
		return id, false, nil
	}
	id, err := s.Store.StartBackfill(ctx, op)
	if err != nil {
		bf.mu.Unlock()
		return 0, false, err
	}
	bf.running, bf.id, bf.current = true, id, backfill.Progress{Status: "running", StartedAt: time.Now()}
	bf.mu.Unlock()
	s.Store.Audit(ctx, op, "graph.backfill", "", map[string]any{"id": id})

	go func() {
		bg := context.Background()
		lastSave := time.Time{}
		r := &backfill.Runner{MainDSN: s.Cfg.PGAdminDSN, Graph: s.Graph, Vector: s.Vector, Logf: s.Logf,
			OnProgress: func(p backfill.Progress) {
				bf.mu.Lock()
				bf.current = p
				bf.mu.Unlock()
				if time.Since(lastSave) > 3*time.Second {
					lastSave = time.Now()
					_ = s.Store.SaveBackfill(bg, id, "running", p, false)
				}
			}}
		p, err := r.Run(bg)
		status := "done"
		if err != nil {
			status = "failed"
			if p.Error == "" {
				p.Error = err.Error()
			}
			if s.Logf != nil {
				s.Logf("关系图回灌 #%d 结束：%v", id, err)
			}
		} else if s.Logf != nil {
			s.Logf("关系图回灌 #%d 完成：%d 位用户、%d 个 IP、%d 台设备", id, p.Users, p.IPs, p.Devices)
		}
		p.Status = status
		_ = s.Store.SaveBackfill(bg, id, status, p, true)
		bf.mu.Lock()
		bf.running, bf.current = false, p
		bf.mu.Unlock()
	}()
	return id, true, nil
}

// AutoBackfill 启动时：从没成功回灌过就自动跑一次（新部署 / 新加的数据来源）
func (s *Server) AutoBackfill(ctx context.Context) {
	if s.Graph == nil || s.Cfg.PGAdminDSN == "" {
		return
	}
	_ = s.Store.AbandonBackfills(ctx)
	done, err := s.Store.BackfillDone(ctx)
	if err != nil || done {
		return
	}
	if _, _, err := s.StartBackfill(ctx, "system"); err != nil && s.Logf != nil {
		s.Logf("关系图自动回灌没有启动：%v", err)
	}
}

// DailyBackfill 每天凌晨 4 点重跑一次回灌：实时写入只能当场认出机房出口，
// 一个 IP 是不是共享出口（被多少个不同用户用过）要按全量统计，回灌顺带刷新；回灌本身可重复执行。
func (s *Server) DailyBackfill(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if now.Hour() != 4 {
				continue
			}
			last, err := s.Store.LastBackfill(ctx)
			if err != nil {
				continue
			}
			y, m, d := now.Date()
			today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			if last != nil && !last.StartedAt.Before(today) {
				continue
			}
			if _, _, err := s.StartBackfill(ctx, "system-daily"); err != nil && s.Logf != nil {
				s.Logf("每日关系图回灌没有启动：%v", err)
			}
		}
	}
}
