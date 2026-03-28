package service

import (
	"fmt"
	"sync"
	"time"
)

// LeakIndicator 泄漏指标类型
type LeakIndicator struct {
	Name           string    `json:"name"`
	Trending       string    `json:"trending"`       // rising / stable / declining
	Samples        int       `json:"samples"`         // 当前采样点数
	ConsecutiveUp  int       `json:"consecutiveUp"`   // 连续上升次数
	LatestValue    float64   `json:"latestValue"`
	DeltaPercent   float64   `json:"deltaPercent"`    // 首尾变化率（%）
	SuspectedLeak  bool      `json:"suspectedLeak"`
	AlertMessage   string    `json:"alertMessage,omitempty"`
	LastCheckedAt  time.Time `json:"lastCheckedAt"`
}

// LeakReport 泄漏检测完整报告
type LeakReport struct {
	CheckedAt   time.Time       `json:"checkedAt"`
	Suspicious  bool            `json:"suspicious"`  // 是否存在可疑泄漏
	Indicators  []LeakIndicator `json:"indicators"`
	Summary     string          `json:"summary"`
}

// LeakDetector 通过分析指标趋势检测潜在内存泄漏
type LeakDetector struct {
	mu         sync.RWMutex
	windowSize int // 滑动窗口大小（采样点数）
	threshold  int // 连续上升多少次判定为可疑泄漏

	// 各指标的历史采样（环形缓冲）
	heapSamples      []float64
	goroutineSamples []float64
	objectSamples    []float64
	sysSamples       []float64
	writeIdx         int
	sampleCount      int
}

func NewLeakDetector(windowSize int) *LeakDetector {
	if windowSize <= 0 {
		windowSize = 20
	}
	// 连续上升阈值 = 窗口大小的 60%（至少 5）
	threshold := windowSize * 60 / 100
	if threshold < 5 {
		threshold = 5
	}
	return &LeakDetector{
		windowSize:       windowSize,
		threshold:        threshold,
		heapSamples:      make([]float64, windowSize),
		goroutineSamples: make([]float64, windowSize),
		objectSamples:    make([]float64, windowSize),
		sysSamples:       make([]float64, windowSize),
	}
}

// RecordSample 记录一组采样值
func (d *LeakDetector) RecordSample(heapAlloc, heapObjects, sys float64, goroutines int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.heapSamples[d.writeIdx] = heapAlloc
	d.goroutineSamples[d.writeIdx] = float64(goroutines)
	d.objectSamples[d.writeIdx] = heapObjects
	d.sysSamples[d.writeIdx] = sys

	d.writeIdx = (d.writeIdx + 1) % d.windowSize
	if d.sampleCount < d.windowSize {
		d.sampleCount++
	}
}

// Report 生成泄漏检测报告
func (d *LeakDetector) Report() LeakReport {
	d.mu.RLock()
	defer d.mu.RUnlock()

	now := time.Now().UTC()
	indicators := []LeakIndicator{
		d.analyzeIndicator("heap_alloc", d.heapSamples, "堆内存分配", now),
		d.analyzeIndicator("goroutines", d.goroutineSamples, "Goroutine 数", now),
		d.analyzeIndicator("heap_objects", d.objectSamples, "堆对象数", now),
		d.analyzeIndicator("sys_memory", d.sysSamples, "系统保留内存", now),
	}

	suspicious := false
	suspiciousNames := make([]string, 0)
	for i := range indicators {
		if indicators[i].SuspectedLeak {
			suspicious = true
			suspiciousNames = append(suspiciousNames, indicators[i].Name)
		}
	}

	summary := "所有指标正常，未检测到泄漏趋势。"
	if suspicious {
		summary = fmt.Sprintf("检测到 %d 项可疑泄漏指标: %v", len(suspiciousNames), suspiciousNames)
	}

	return LeakReport{
		CheckedAt:  now,
		Suspicious: suspicious,
		Indicators: indicators,
		Summary:    summary,
	}
}

func (d *LeakDetector) analyzeIndicator(name string, samples []float64, displayName string, now time.Time) LeakIndicator {
	indicator := LeakIndicator{
		Name:          name,
		Samples:       d.sampleCount,
		Trending:      "stable",
		LastCheckedAt: now,
	}

	if d.sampleCount < 3 {
		indicator.AlertMessage = "采样点不足，暂无法判定趋势。"
		return indicator
	}

	// 获取有效的有序采样值
	ordered := d.orderedSamples(samples)
	if len(ordered) == 0 {
		return indicator
	}

	indicator.LatestValue = ordered[len(ordered)-1]

	// 计算连续上升次数
	consecutiveUp := 0
	maxConsecutiveUp := 0
	for i := 1; i < len(ordered); i++ {
		if ordered[i] > ordered[i-1] {
			consecutiveUp++
			if consecutiveUp > maxConsecutiveUp {
				maxConsecutiveUp = consecutiveUp
			}
		} else {
			consecutiveUp = 0
		}
	}
	indicator.ConsecutiveUp = maxConsecutiveUp

	// 计算首尾变化率
	first := ordered[0]
	last := ordered[len(ordered)-1]
	if first > 0 {
		indicator.DeltaPercent = (last - first) / first * 100
	}

	// 判定趋势
	if maxConsecutiveUp >= d.threshold {
		indicator.Trending = "rising"
		indicator.SuspectedLeak = true
		indicator.AlertMessage = fmt.Sprintf(
			"%s 连续上升 %d 次（阈值 %d），变化率 %.1f%%，可能存在泄漏。",
			displayName, maxConsecutiveUp, d.threshold, indicator.DeltaPercent,
		)
	} else if indicator.DeltaPercent > 50 && maxConsecutiveUp >= d.threshold/2 {
		indicator.Trending = "rising"
		indicator.SuspectedLeak = true
		indicator.AlertMessage = fmt.Sprintf(
			"%s 变化率 %.1f%%（连续上升 %d 次），增长较快，需关注。",
			displayName, indicator.DeltaPercent, maxConsecutiveUp,
		)
	} else if indicator.DeltaPercent < -10 {
		indicator.Trending = "declining"
	}

	return indicator
}

// orderedSamples 按写入顺序返回环形缓冲中的有效样本
func (d *LeakDetector) orderedSamples(ring []float64) []float64 {
	if d.sampleCount == 0 {
		return nil
	}
	result := make([]float64, 0, d.sampleCount)
	if d.sampleCount < d.windowSize {
		// 未满，从 0 开始
		result = append(result, ring[:d.sampleCount]...)
	} else {
		// 已满，从 writeIdx 开始（最旧的点）
		result = append(result, ring[d.writeIdx:]...)
		result = append(result, ring[:d.writeIdx]...)
	}
	return result
}
