// Package backfill 从主库把历史关系回灌进关系图（NebulaGraph）与设备向量（Milvus）。
//
// 风控平台上线前的注册、登录、设备、第三方绑定、邀请、支付等记录都只在主库里，图上只有上线后
// 实时事件写进来的那一点。这里按来源逐个读主库（只读、在 SQL 里先聚合），写成与实时事件同一套点与边：
// 首次时间取最小、最近时间取最大、次数取较大值，所以可以反复执行，不会把次数越加越多。
package backfill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/proxyip"
	"aegis/services/risk/internal/vector"

	"github.com/jackc/pgx/v5"
)

// Step 一个来源的进度
type Step struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
	Status  string `json:"status"` // pending / running / done / skipped / failed
	Rows    int64  `json:"rows"`
	Error   string `json:"error,omitempty"`
	Seconds int64  `json:"seconds"`
}

// Progress 一次回灌的整体进度
type Progress struct {
	Status     string           `json:"status"` // running / done / failed
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt *time.Time       `json:"finishedAt,omitempty"`
	Steps      []Step           `json:"steps"`
	Written    map[string]int64 `json:"written"` // 按点类型 / 边类型统计写入条数
	Users      int64            `json:"users"`
	Devices    int64            `json:"devices"`
	IPs        int64            `json:"ips"`
	Vectors    int64            `json:"vectors"`
	ProxyIPs   int64            `json:"proxyIps"`   // 从图里删掉的代理 / CDN 地址数
	HostingIPs int64            `json:"hostingIps"` // 标成机房 / 云主机出口的 IP 数
	SharedIPs  int64            `json:"sharedIps"`  // 标成共享出口的 IP 数
	Error      string           `json:"error,omitempty"`
}

// Runner 执行一次回灌
type Runner struct {
	MainDSN string
	Graph   *graph.Client
	Vector  *vector.Client // 可为 nil：不写设备向量
	Logf    func(string, ...any)
	// OnProgress 每个来源开始 / 结束时回调（拷贝）
	OnProgress func(Progress)

	mu   sync.Mutex
	prog Progress

	batch   *graph.Batch
	ips     map[string]*ipInfo
	devices map[string]*deviceInfo // app:device
	proxies map[string]bool        // 遇到的代理 / CDN / 内网地址
}

type ipInfo struct {
	users                           map[string]struct{} // 用过它的不同用户（应用:用户）
	first, last                     int64
	country, province, city, isp    string
	asn                             int64
	proxy, vpn, tor, dc, hasRiskRow bool
}

type deviceInfo struct {
	appID, userID int64
	id            string
	attrs         map[string]string
	first, last   int64
	banned        bool
}

// source 一个来源
type source struct {
	key, label, detail string
	run                func(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error)
}

// Source 一个来源的说明（控制台展示用）
type Source interface {
	Info() (key, label, detail string)
}

// Info 来源的键、名称与说明
func (s source) Info() (string, string, string) { return s.key, s.label, s.detail }

// Sources 全部来源（顺序即执行顺序；控制台按这个列表展示）
func Sources() []Source {
	out := make([]Source, len(sources))
	for i, s := range sources {
		out[i] = s
	}
	return out
}

// Run 执行全部来源。单个来源失败（比如某张表在这个部署里不存在）记在该步骤上，继续下一个。
func (r *Runner) Run(ctx context.Context) (Progress, error) {
	if r.Graph == nil {
		return Progress{}, graph.ErrNotReady
	}
	if r.MainDSN == "" {
		return Progress{}, errors.New("没有配置主库连接（RISK_PG_ADMIN_DSN）")
	}
	conn, err := pgx.Connect(ctx, r.MainDSN)
	if err != nil {
		return Progress{}, err
	}
	defer conn.Close(ctx)
	// 只读事务级别的保护：回灌绝不写主库
	if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		return Progress{}, err
	}

	r.batch = r.Graph.NewBatch(graph.MaxStatements)
	r.ips = map[string]*ipInfo{}
	r.devices = map[string]*deviceInfo{}
	r.proxies = map[string]bool{}
	r.prog = Progress{Status: "running", StartedAt: time.Now(), Written: map[string]int64{}}
	for _, s := range sources {
		r.prog.Steps = append(r.prog.Steps, Step{Key: s.key, Label: s.label, Detail: s.detail, Status: "pending"})
	}
	r.emit()

	failed := 0
	for i, s := range sources {
		if err := ctx.Err(); err != nil {
			return r.finish(err)
		}
		started := time.Now()
		r.update(func(p *Progress) { p.Steps[i].Status = "running" })
		n, err := s.run(ctx, conn, r)
		if err == nil {
			err = r.batch.Flush()
		}
		r.update(func(p *Progress) {
			st := &p.Steps[i]
			st.Rows, st.Seconds = n, int64(time.Since(started).Seconds())
			switch {
			case errors.Is(err, errMissingTable):
				st.Status, st.Error = "skipped", "这个部署里没有这张表"
			case err != nil:
				st.Status, st.Error = "failed", err.Error()
				failed++
			default:
				st.Status = "done"
			}
			for k, v := range r.batch.Written {
				p.Written[k] = int64(v)
			}
		})
		if err != nil && !errors.Is(err, errMissingTable) && r.Logf != nil {
			r.Logf("关系图回灌：%s 失败：%v", s.label, err)
		}
	}
	if failed > 0 {
		return r.finish(fmt.Errorf("%d 个来源失败", failed))
	}
	return r.finish(nil)
}

func (r *Runner) finish(err error) (Progress, error) {
	now := time.Now()
	r.update(func(p *Progress) {
		p.FinishedAt = &now
		p.Status = "done"
		if err != nil {
			p.Status, p.Error = "failed", err.Error()
		}
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.copyLocked(), err
}

func (r *Runner) update(fn func(*Progress)) {
	r.mu.Lock()
	fn(&r.prog)
	r.prog.IPs, r.prog.Devices = int64(len(r.ips)), int64(len(r.devices))
	r.mu.Unlock()
	r.emit()
}

func (r *Runner) emit() {
	if r.OnProgress == nil {
		return
	}
	r.mu.Lock()
	p := r.copyLocked()
	r.mu.Unlock()
	r.OnProgress(p)
}

func (r *Runner) copyLocked() Progress {
	p := r.prog
	p.Steps = append([]Step(nil), r.prog.Steps...)
	p.Written = make(map[string]int64, len(r.prog.Written))
	for k, v := range r.prog.Written {
		p.Written[k] = v
	}
	return p
}

var errMissingTable = errors.New("missing table")

// query 跑一条查询逐行处理；表不存在返回 errMissingTable
func query(ctx context.Context, conn *pgx.Conn, sql string, each func(pgx.Rows) error) (int64, error) {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") && strings.Contains(err.Error(), "relation") {
			return 0, errMissingTable
		}
		return 0, err
	}
	defer rows.Close()
	var n int64
	for rows.Next() {
		if err := each(rows); err != nil {
			return n, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		if strings.Contains(err.Error(), "does not exist") && strings.Contains(err.Error(), "relation") {
			return 0, errMissingTable
		}
		return n, err
	}
	return n, nil
}

// ── 写入辅助 ──

func (r *Runner) user(appID, uid, first, last int64, sets ...string) error {
	if appID <= 0 || uid <= 0 {
		return nil
	}
	all := append([]string{graph.SetInt("app_id", appID), graph.SetInt("uid", uid)}, sets...)
	return r.batch.Vertex(graph.TagUser, graph.UserVID(appID, uid), first, last, graph.Sets(all...)...)
}

// useIP 用户用过某个 IP（记下 IP，最后统一写 IP 点与网段）
func (r *Runner) useIP(edge string, appID, uid int64, ip string, first, last, count int64) error {
	ip = r.clientIP(ip)
	if ip == "" || appID <= 0 || uid <= 0 {
		return nil
	}
	r.noteIP(ip, first, last).addUser(appID, uid)
	return r.batch.Edge(edge, graph.UserVID(appID, uid), graph.IPVID(ip), first, last, count)
}

func (i *ipInfo) addUser(appID, uid int64) {
	if appID <= 0 || uid <= 0 {
		return
	}
	if i.users == nil {
		i.users = map[string]struct{}{}
	}
	i.users[fmt.Sprintf("%d:%d", appID, uid)] = struct{}{}
}

func (r *Runner) noteIP(ip string, first, last int64) *ipInfo {
	info := r.ips[ip]
	if info == nil {
		info = &ipInfo{first: first, last: last}
		r.ips[ip] = info
	}
	if first > 0 && (info.first == 0 || first < info.first) {
		info.first = first
	}
	if last > info.last {
		info.last = last
	}
	return info
}

// useDevice 用户用过某台设备（设备属性累积起来，最后统一写设备点与向量）
func (r *Runner) useDevice(edge string, appID, uid int64, deviceID string, first, last, count int64, attrs map[string]string) error {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || appID <= 0 {
		return nil
	}
	r.noteDevice(appID, uid, deviceID, first, last, attrs)
	if uid <= 0 {
		return nil
	}
	return r.batch.Edge(edge, graph.UserVID(appID, uid), graph.DeviceVID(deviceID), first, last, count)
}

func (r *Runner) noteDevice(appID, uid int64, deviceID string, first, last int64, attrs map[string]string) *deviceInfo {
	key := fmt.Sprintf("%d:%s", appID, deviceID)
	d := r.devices[key]
	if d == nil {
		d = &deviceInfo{appID: appID, id: deviceID, attrs: map[string]string{}, first: first, last: last}
		r.devices[key] = d
	}
	if uid > 0 && (d.userID == 0 || last >= d.last) {
		d.userID = uid
	}
	if first > 0 && (d.first == 0 || first < d.first) {
		d.first = first
	}
	if last > d.last {
		d.last = last
	}
	for k, v := range attrs {
		if v = strings.TrimSpace(v); v != "" && d.attrs[k] == "" {
			d.attrs[k] = v
		}
	}
	return d
}

func cleanIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if i := strings.IndexByte(ip, '/'); i > 0 { // inet 文本可能带掩码
		ip = ip[:i]
	}
	if engine.Subnet(ip) == "" {
		return ""
	}
	return ip
}

// clientIP 用户自己的出口 IP；代理 / CDN / 内网地址记下来（最后从图里删掉）并返回空
func (r *Runner) clientIP(ip string) string {
	ip = cleanIP(ip)
	if ip == "" {
		return ""
	}
	if proxyip.IsProxy(ip) {
		r.proxies[ip] = true
		return ""
	}
	return ip
}

// mask 付款账号这类标识的打码显示
func mask(v string) string {
	r := []rune(v)
	switch {
	case len(r) <= 2:
		return "**"
	case len(r) <= 6:
		return string(r[:1]) + "****" + string(r[len(r)-1:])
	}
	return string(r[:3]) + "****" + string(r[len(r)-3:])
}

func lowerPlatform(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch {
	case strings.Contains(p, "android"):
		return "android"
	case strings.Contains(p, "ios"), strings.Contains(p, "iphone"), strings.Contains(p, "ipad"):
		return "ios"
	case strings.Contains(p, "windows"):
		return "windows"
	case strings.Contains(p, "mac"):
		return "macos"
	case strings.Contains(p, "linux"):
		return "linux"
	case strings.Contains(p, "harmony"):
		return "harmonyos"
	}
	return p
}
