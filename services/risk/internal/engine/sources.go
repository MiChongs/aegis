package engine

import (
	"context"
	"strconv"
)

// ListHit 一次名单命中
type ListHit struct {
	ListType string `json:"listType"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	ListKey  string `json:"listKey,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// ListQuery 本次请求要查的名单取值（按名单类型）
type ListQuery struct {
	AppID  int64
	Scene  string
	Values map[string]string // 名单类型 → 取值；cidr 用 IP、ua_keyword 用 UA
}

// VelocityQuery 一个滑窗计数特征
type VelocityQuery struct {
	Feature       string
	Key           string // 已拼好主体与作用域的计数键（不含前缀）
	WindowSeconds int
	Distinct      string // 非空时数去重的成员
}

// LookupQuery 流式特征：读 RisingWave 写回的值
type LookupQuery struct {
	Feature string
	Key     string
}

// GraphQuery 图特征
type GraphQuery struct {
	Feature    string
	Template   string
	Entity     string
	AppID      int64
	Value      string
	WithinDays int
	CacheTTL   int
}

// VectorQuery 向量特征
type VectorQuery struct {
	Feature    string
	Collection string
	AppID      int64
	TopK       int
	MinScore   float64
	Output     string
	Self       string
}

// Sources 决策要用的外部数据源。每个方法各自在调用方给的 ctx 截止前返回，
// 出错或超时只让这一类特征降级为默认值，不影响整次决策。
type Sources interface {
	CheckLists(ctx context.Context, q ListQuery) ([]ListHit, error)
	Velocity(ctx context.Context, q []VelocityQuery) (map[string]float64, error)
	Stream(ctx context.Context, q []LookupQuery) (map[string]float64, error)
	Graph(ctx context.Context, q []GraphQuery) (map[string]float64, error)
	Vector(ctx context.Context, q []VectorQuery, device Device) (map[string]float64, error)
}

// NopSources 什么都不查（单元测试、离线校验）
type NopSources struct{}

func (NopSources) CheckLists(context.Context, ListQuery) ([]ListHit, error) { return nil, nil }
func (NopSources) Velocity(context.Context, []VelocityQuery) (map[string]float64, error) {
	return nil, nil
}
func (NopSources) Stream(context.Context, []LookupQuery) (map[string]float64, error) { return nil, nil }
func (NopSources) Graph(context.Context, []GraphQuery) (map[string]float64, error)   { return nil, nil }
func (NopSources) Vector(context.Context, []VectorQuery, Device) (map[string]float64, error) {
	return nil, nil
}

// FixedSources 模拟器用：特征值与名单命中由调用方给定
type FixedSources struct {
	Features map[string]float64
	Lists    []ListHit
}

func (f FixedSources) CheckLists(context.Context, ListQuery) ([]ListHit, error) { return f.Lists, nil }

func (f FixedSources) pick(keys []string) map[string]float64 {
	out := map[string]float64{}
	for _, k := range keys {
		if v, ok := f.Features[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (f FixedSources) Velocity(_ context.Context, q []VelocityQuery) (map[string]float64, error) {
	keys := make([]string, len(q))
	for i := range q {
		keys[i] = q[i].Feature
	}
	return f.pick(keys), nil
}

func (f FixedSources) Stream(_ context.Context, q []LookupQuery) (map[string]float64, error) {
	keys := make([]string, len(q))
	for i := range q {
		keys[i] = q[i].Feature
	}
	return f.pick(keys), nil
}

func (f FixedSources) Graph(_ context.Context, q []GraphQuery) (map[string]float64, error) {
	keys := make([]string, len(q))
	for i := range q {
		keys[i] = q[i].Feature
	}
	return f.pick(keys), nil
}

func (f FixedSources) Vector(_ context.Context, q []VectorQuery, _ Device) (map[string]float64, error) {
	keys := make([]string, len(q))
	for i := range q {
		keys[i] = q[i].Feature
	}
	return f.pick(keys), nil
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
