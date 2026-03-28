package service

import (
	"bytes"
	"sync"
	"sync/atomic"
)

// PoolStats 对象池统计
type PoolStats struct {
	Name     string `json:"name"`
	Gets     int64  `json:"gets"`     // 总获取次数
	Puts     int64  `json:"puts"`     // 总归还次数
	Hits     int64  `json:"hits"`     // 从池中命中次数（非新建）
	Misses   int64  `json:"misses"`   // 未命中次数（新建）
	HitRate  float64 `json:"hitRate"` // 命中率
}

// TrackedPool 带统计的 sync.Pool 封装
type TrackedPool struct {
	name    string
	pool    sync.Pool
	gets    atomic.Int64
	puts    atomic.Int64
	hits    atomic.Int64
	misses  atomic.Int64
}

func newTrackedPool(name string, newFunc func() any) *TrackedPool {
	tp := &TrackedPool{name: name}
	tp.pool.New = func() any {
		tp.misses.Add(1)
		return newFunc()
	}
	return tp
}

func (tp *TrackedPool) Get() any {
	tp.gets.Add(1)
	obj := tp.pool.Get()
	// 如果 New 未被调用，说明是命中
	currentMisses := tp.misses.Load()
	currentGets := tp.gets.Load()
	expectedHits := currentGets - currentMisses
	tp.hits.Store(expectedHits)
	return obj
}

func (tp *TrackedPool) Put(obj any) {
	tp.puts.Add(1)
	tp.pool.Put(obj)
}

func (tp *TrackedPool) Stats() PoolStats {
	gets := tp.gets.Load()
	misses := tp.misses.Load()
	hits := gets - misses
	if hits < 0 {
		hits = 0
	}
	var hitRate float64
	if gets > 0 {
		hitRate = float64(hits) / float64(gets) * 100
	}
	return PoolStats{
		Name:    tp.name,
		Gets:    gets,
		Puts:    tp.puts.Load(),
		Hits:    hits,
		Misses:  misses,
		HitRate: hitRate,
	}
}

// MemoryPools 管理所有对象池
type MemoryPools struct {
	// JSON 序列化缓冲区（初始 4KB）
	JSONBuffer *TrackedPool
	// HTTP 响应缓冲区（初始 2KB）
	HTTPBuffer *TrackedPool
	// 小 slice（512B）
	SmallSlice *TrackedPool
	// 中 slice（4KB）
	MediumSlice *TrackedPool
	// 大 slice（32KB）
	LargeSlice *TrackedPool
}

func NewMemoryPools() *MemoryPools {
	return &MemoryPools{
		JSONBuffer: newTrackedPool("json_buffer", func() any {
			buf := bytes.NewBuffer(make([]byte, 0, 4096))
			return buf
		}),
		HTTPBuffer: newTrackedPool("http_buffer", func() any {
			buf := bytes.NewBuffer(make([]byte, 0, 2048))
			return buf
		}),
		SmallSlice: newTrackedPool("small_slice_512b", func() any {
			b := make([]byte, 512)
			return &b
		}),
		MediumSlice: newTrackedPool("medium_slice_4kb", func() any {
			b := make([]byte, 4096)
			return &b
		}),
		LargeSlice: newTrackedPool("large_slice_32kb", func() any {
			b := make([]byte, 32768)
			return &b
		}),
	}
}

// GetJSONBuffer 获取 JSON 缓冲区（用完后调用 PutJSONBuffer 归还）
func (p *MemoryPools) GetJSONBuffer() *bytes.Buffer {
	buf := p.JSONBuffer.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutJSONBuffer 归还 JSON 缓冲区
func (p *MemoryPools) PutJSONBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// 防止过大的 buffer 回池污染（超过 1MB 丢弃）
	if buf.Cap() > 1024*1024 {
		return
	}
	p.JSONBuffer.Put(buf)
}

// GetHTTPBuffer 获取 HTTP 响应缓冲区
func (p *MemoryPools) GetHTTPBuffer() *bytes.Buffer {
	buf := p.HTTPBuffer.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutHTTPBuffer 归还 HTTP 响应缓冲区
func (p *MemoryPools) PutHTTPBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	if buf.Cap() > 1024*1024 {
		return
	}
	p.HTTPBuffer.Put(buf)
}

// GetSmallSlice 获取 512B 切片
func (p *MemoryPools) GetSmallSlice() *[]byte {
	return p.SmallSlice.Get().(*[]byte)
}

// PutSmallSlice 归还小切片
func (p *MemoryPools) PutSmallSlice(b *[]byte) {
	if b == nil {
		return
	}
	p.SmallSlice.Put(b)
}

// GetMediumSlice 获取 4KB 切片
func (p *MemoryPools) GetMediumSlice() *[]byte {
	return p.MediumSlice.Get().(*[]byte)
}

// PutMediumSlice 归还中切片
func (p *MemoryPools) PutMediumSlice(b *[]byte) {
	if b == nil {
		return
	}
	p.MediumSlice.Put(b)
}

// GetLargeSlice 获取 32KB 切片
func (p *MemoryPools) GetLargeSlice() *[]byte {
	return p.LargeSlice.Get().(*[]byte)
}

// PutLargeSlice 归还大切片
func (p *MemoryPools) PutLargeSlice(b *[]byte) {
	if b == nil {
		return
	}
	p.LargeSlice.Put(b)
}

// AllStats 返回所有池的统计
func (p *MemoryPools) AllStats() []PoolStats {
	return []PoolStats{
		p.JSONBuffer.Stats(),
		p.HTTPBuffer.Stats(),
		p.SmallSlice.Stats(),
		p.MediumSlice.Stats(),
		p.LargeSlice.Stats(),
	}
}
