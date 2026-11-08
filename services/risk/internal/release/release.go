// Package release 发布包经 etcd 下发。
//
// risk-admin 发布时把 bundle.Release 写到 /aegis/risk/release/scenes/<场景>；决策实例启动时整前缀读一遍，
// 之后从那个修订号起 watch。新版本先编译，编译通过才换上（失败保留旧版本并记在状态里），
// 换下来的旧程序过宽限期再释放，在途请求不受影响。
package release

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/engine"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Prefix etcd 里发布指针的前缀
const Prefix = "/aegis/risk/release/scenes/"

// Key 场景的发布键
func Key(scene string) string { return Prefix + scene }

// Publish 写发布指针
func Publish(ctx context.Context, cli *clientv3.Client, r bundle.Release) (int64, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	resp, err := cli.Put(ctx, Key(r.Scene), string(raw))
	if err != nil {
		return 0, err
	}
	return resp.Header.Revision, nil
}

// Withdraw 下线一个场景
func Withdraw(ctx context.Context, cli *clientv3.Client, scene string) error {
	_, err := cli.Delete(ctx, Key(scene))
	return err
}

// Active 一个场景当前生效的程序
type Active struct {
	Scene     string
	Primary   *engine.Program
	Candidate *engine.Program
	Canary    int
	Shadow    bool
	LoadedAt  time.Time
	Error     string // 最近一次加载失败的原因（旧版本仍在服务）
}

// Pick 按主体哈希决定这次用正式还是候选版本；第二个返回值是要不要额外跑影子评估
func (a *Active) Pick(subject string) (*engine.Program, *engine.Program) {
	if a.Candidate == nil {
		return a.Primary, nil
	}
	if a.Shadow {
		return a.Primary, a.Candidate
	}
	if a.Canary > 0 {
		h := fnv.New32a()
		_, _ = h.Write([]byte(a.Scene + ":" + subject))
		if int(h.Sum32()%100) < a.Canary {
			return a.Candidate, nil
		}
	}
	return a.Primary, nil
}

// Registry 全部场景的生效程序
type Registry struct {
	cli    *clientv3.Client
	models engine.ModelLoader
	logf   func(string, ...any)

	mu     sync.RWMutex
	scenes map[string]*Active
	ready  chan struct{}
	once   sync.Once
}

// NewRegistry 新建
func NewRegistry(cli *clientv3.Client, models engine.ModelLoader, logf func(string, ...any)) *Registry {
	return &Registry{cli: cli, models: models, logf: logf, scenes: map[string]*Active{}, ready: make(chan struct{})}
}

// Get 取场景
func (r *Registry) Get(scene string) *Active {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.scenes[scene]
}

// All 全部场景（状态接口）
func (r *Registry) All() []*Active {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Active, 0, len(r.scenes))
	for _, a := range r.scenes {
		out = append(out, a)
	}
	return out
}

// Ready 首次全量加载完成
func (r *Registry) Ready() <-chan struct{} { return r.ready }

// Run 全量加载后持续 watch，直到 ctx 结束
func (r *Registry) Run(ctx context.Context) error {
	for {
		rev, err := r.loadAll(ctx)
		if err == nil {
			r.once.Do(func() { close(r.ready) })
			err = r.watch(ctx, rev+1)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.logf != nil {
			r.logf("发布 watch 中断，2 秒后重连：%v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (r *Registry) loadAll(ctx context.Context) (int64, error) {
	resp, err := r.cli.Get(ctx, Prefix, clientv3.WithPrefix())
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, kv := range resp.Kvs {
		scene := strings.TrimPrefix(string(kv.Key), Prefix)
		seen[scene] = true
		r.apply(ctx, scene, kv.Value)
	}
	r.mu.Lock()
	for scene, a := range r.scenes {
		if !seen[scene] {
			delete(r.scenes, scene)
			retire(a)
		}
	}
	r.mu.Unlock()
	return resp.Header.Revision, nil
}

func (r *Registry) watch(ctx context.Context, from int64) error {
	wctx, cancel := context.WithCancel(clientv3.WithRequireLeader(ctx))
	defer cancel()
	for wr := range r.cli.Watch(wctx, Prefix, clientv3.WithPrefix(), clientv3.WithRev(from)) {
		if err := wr.Err(); err != nil {
			return err
		}
		for _, ev := range wr.Events {
			scene := strings.TrimPrefix(string(ev.Kv.Key), Prefix)
			if ev.Type == clientv3.EventTypeDelete {
				r.mu.Lock()
				if a := r.scenes[scene]; a != nil {
					delete(r.scenes, scene)
					retire(a)
				}
				r.mu.Unlock()
				continue
			}
			r.apply(ctx, scene, ev.Kv.Value)
		}
	}
	return errors.New("watch 通道关闭")
}

// apply 编译一个场景的新发布；失败时旧版本继续服务
func (r *Registry) apply(ctx context.Context, scene string, raw []byte) {
	var rel bundle.Release
	fail := func(err error) {
		r.mu.Lock()
		if a := r.scenes[scene]; a != nil {
			a.Error = err.Error()
		} else {
			r.scenes[scene] = &Active{Scene: scene, Error: err.Error(), LoadedAt: time.Now()}
		}
		r.mu.Unlock()
		if r.logf != nil {
			r.logf("场景 %s 发布加载失败：%v", scene, err)
		}
	}
	if err := json.Unmarshal(raw, &rel); err != nil {
		fail(err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cur := r.Get(scene)
	primary, err := r.reuseOrCompile(cctx, cur, rel.Primary)
	if err != nil {
		fail(err)
		return
	}
	var candidate *engine.Program
	if rel.Candidate != nil {
		candidate, err = r.reuseOrCompile(cctx, cur, *rel.Candidate)
		if err != nil {
			fail(err)
			return
		}
	}
	next := &Active{Scene: scene, Primary: primary, Candidate: candidate, Canary: rel.CanaryPercent, Shadow: rel.Shadow, LoadedAt: time.Now()}
	r.mu.Lock()
	old := r.scenes[scene]
	r.scenes[scene] = next
	r.mu.Unlock()
	if old != nil {
		for _, p := range []*engine.Program{old.Primary, old.Candidate} {
			if p != nil && p != primary && p != candidate {
				p.DisposeLater(30 * time.Second)
			}
		}
	}
	if r.logf != nil {
		r.logf("场景 %s 已加载版本 %s", scene, primary.Bundle.Version)
	}
}

// reuseOrCompile 内容摘要没变就沿用已编译的程序（只改灰度比例时不重编）
func (r *Registry) reuseOrCompile(ctx context.Context, cur *Active, b bundle.Bundle) (*engine.Program, error) {
	sum := b.Checksum
	if sum == "" {
		sum = b.ComputeChecksum()
	}
	if cur != nil {
		for _, p := range []*engine.Program{cur.Primary, cur.Candidate} {
			if p != nil && p.Bundle.Version == b.Version && (p.Bundle.Checksum == sum || p.Bundle.ComputeChecksum() == sum) {
				return p, nil
			}
		}
	}
	b.Checksum = sum
	return engine.Compile(ctx, b, r.models)
}

func retire(a *Active) {
	for _, p := range []*engine.Program{a.Primary, a.Candidate} {
		if p != nil {
			p.DisposeLater(30 * time.Second)
		}
	}
}
