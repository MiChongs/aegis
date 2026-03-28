package service

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// CacheEntry 缓存条目
type cacheEntry struct {
	key       string
	value     any
	expiresAt time.Time
	element   *list.Element
}

// CacheStats 缓存统计
type CacheStats struct {
	MaxEntries int     `json:"maxEntries"`
	Size       int     `json:"size"`
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	Evictions  int64   `json:"evictions"`
	Expired    int64   `json:"expired"`
	HitRate    float64 `json:"hitRate"`
}

// MemoryCache 本地 LRU + TTL 内存缓存
type MemoryCache struct {
	mu         sync.RWMutex
	maxEntries int
	defaultTTL time.Duration
	items      map[string]*cacheEntry
	lru        *list.List // 最近使用列表（Front = 最新）
	hits       atomic.Int64
	misses     atomic.Int64
	evictions  atomic.Int64
	expired    atomic.Int64
}

func NewMemoryCache(maxEntries int, defaultTTL time.Duration) *MemoryCache {
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}
	c := &MemoryCache{
		maxEntries: maxEntries,
		defaultTTL: defaultTTL,
		items:      make(map[string]*cacheEntry, maxEntries),
		lru:        list.New(),
	}
	// 启动后台过期清理（每 30s 扫描一次）
	go c.cleanupLoop()
	return c
}

// Get 获取缓存条目，未命中或已过期返回 nil, false
func (c *MemoryCache) Get(key string) (any, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	// 检查过期
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		c.removeEntry(entry)
		c.mu.Unlock()
		c.expired.Add(1)
		c.misses.Add(1)
		return nil, false
	}
	// 提升到 LRU 前端
	c.mu.Lock()
	c.lru.MoveToFront(entry.element)
	c.mu.Unlock()
	c.hits.Add(1)
	return entry.value, true
}

// Set 写入缓存条目（使用默认 TTL）
func (c *MemoryCache) Set(key string, value any) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL 写入缓存条目（自定义 TTL）
func (c *MemoryCache) SetWithTTL(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已存在，更新
	if existing, ok := c.items[key]; ok {
		existing.value = value
		existing.expiresAt = time.Now().Add(ttl)
		c.lru.MoveToFront(existing.element)
		return
	}

	// 淘汰最旧的条目（如果满了）
	for len(c.items) >= c.maxEntries {
		c.evictOldest()
	}

	// 新建条目
	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	entry.element = c.lru.PushFront(entry)
	c.items[key] = entry
}

// Delete 删除指定 key
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.items[key]; ok {
		c.removeEntry(entry)
	}
}

// Flush 清空所有缓存
func (c *MemoryCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*cacheEntry, c.maxEntries)
	c.lru.Init()
}

// Size 返回当前条目数
func (c *MemoryCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Stats 返回缓存统计
func (c *MemoryCache) Stats() CacheStats {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()

	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}
	return CacheStats{
		MaxEntries: c.maxEntries,
		Size:       size,
		Hits:       hits,
		Misses:     misses,
		Evictions:  c.evictions.Load(),
		Expired:    c.expired.Load(),
		HitRate:    hitRate,
	}
}

func (c *MemoryCache) evictOldest() {
	el := c.lru.Back()
	if el == nil {
		return
	}
	entry := el.Value.(*cacheEntry)
	c.removeEntry(entry)
	c.evictions.Add(1)
}

func (c *MemoryCache) removeEntry(entry *cacheEntry) {
	c.lru.Remove(entry.element)
	delete(c.items, entry.key)
}

func (c *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.cleanupExpired()
	}
}

func (c *MemoryCache) cleanupExpired() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	// 从 LRU 尾部开始扫描（最旧的优先检查）
	var next *list.Element
	for el := c.lru.Back(); el != nil; el = next {
		next = el.Prev()
		entry := el.Value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			c.removeEntry(entry)
			c.expired.Add(1)
		}
	}
}
