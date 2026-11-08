// Package vector 设备指纹向量（Milvus）。
//
// 每台设备一行：主键 应用:设备，向量由指纹要素做带符号的特征哈希（128 维，L2 归一，COSINE）。
// 同一批改机 / 模拟器出来的设备，指纹要素几乎一样，向量余弦接近 1；同型号的正常设备一般在 0.6–0.85。
// 决策时按应用（分区键）检索 TopK，数相似度过阈值的设备、其中已封禁的设备。
package vector

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"aegis/services/risk/internal/config"

	"github.com/milvus-io/milvus/client/v3/entity"
	"github.com/milvus-io/milvus/client/v3/index"
	"github.com/milvus-io/milvus/client/v3/milvusclient"
)

// Dim 向量维数
const Dim = 128

// CollectionDevices 设备集合
const CollectionDevices = "risk_devices"

var weights = map[string]float64{
	"brand": 1.0, "model": 2.0, "brand_model": 2.0, "screen": 1.5, "platform": 0.8, "os": 1.0, "os_major": 0.8,
	"locale": 0.8, "timezone": 0.8, "app_major": 0.5, "emulator": 1.5, "rooted": 1.2,
}

// Embed 指纹要素 → 向量。attrs 的键与 engine.Device.Attrs 一致。
func Embed(attrs map[string]string) []float32 {
	vec := make([]float64, Dim)
	add := func(token string, w float64) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		sum := h.Sum64()
		idx := int(sum % Dim)
		sign := 1.0
		if (sum>>63)&1 == 1 {
			sign = -1
		}
		vec[idx] += sign * w
	}
	get := func(k string) string { return strings.ToLower(strings.TrimSpace(attrs[k])) }
	for _, k := range []string{"brand", "model", "screen", "platform", "locale", "timezone"} {
		if v := get(k); v != "" {
			add(k+"="+v, weights[k])
		}
	}
	if b, m := get("brand"), get("model"); b != "" || m != "" {
		add("brand_model="+b+"|"+m, weights["brand_model"])
	}
	if os := get("os_version"); os != "" {
		add("os="+os, weights["os"])
		major, _, _ := strings.Cut(os, ".")
		add("os_major="+major, weights["os_major"])
	}
	if app := get("app_version"); app != "" {
		major, _, _ := strings.Cut(app, ".")
		add("app_major="+major, weights["app_major"])
	}
	for _, k := range []string{"emulator", "rooted"} {
		if get(k) == "true" {
			add(k, weights[k])
		}
	}
	// 其余要素（字体哈希、传感器、WebGL 渲染器……）
	var extra []string
	for k := range attrs {
		if strings.HasPrefix(k, "x.") {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		if v := get(k); v != "" {
			add(k+"="+v, 0.7)
		}
	}
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	out := make([]float32, Dim)
	if norm == 0 {
		return out
	}
	norm = math.Sqrt(norm)
	for i, v := range vec {
		out[i] = float32(v / norm)
	}
	return out
}

// Empty 向量全零（没有任何指纹要素）
func Empty(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// Client Milvus
type Client struct {
	cli *milvusclient.Client
}

// Open 连接
func Open(ctx context.Context, cfg config.Config) (*Client, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cli, err := milvusclient.New(cctx, &milvusclient.ClientConfig{Address: cfg.MilvusAddr})
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli}, nil
}

// Close 关闭
func (c *Client) Close(ctx context.Context) {
	if c != nil && c.cli != nil {
		_ = c.cli.Close(ctx)
	}
}

// Migrate 建集合与索引并加载（可重复执行）
func (c *Client) Migrate(ctx context.Context) error {
	has, err := c.cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(CollectionDevices))
	if err != nil {
		return err
	}
	if !has {
		schema := entity.NewSchema().WithName(CollectionDevices).WithDescription("风控设备指纹向量").
			WithField(entity.NewField().WithName("key").WithDataType(entity.FieldTypeVarChar).WithMaxLength(160).WithIsPrimaryKey(true)).
			WithField(entity.NewField().WithName("app_id").WithDataType(entity.FieldTypeInt64).WithIsPartitionKey(true)).
			WithField(entity.NewField().WithName("device_id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(128)).
			WithField(entity.NewField().WithName("user_id").WithDataType(entity.FieldTypeInt64)).
			WithField(entity.NewField().WithName("banned").WithDataType(entity.FieldTypeBool)).
			WithField(entity.NewField().WithName("updated_at").WithDataType(entity.FieldTypeInt64)).
			WithField(entity.NewField().WithName("vec").WithDataType(entity.FieldTypeFloatVector).WithDim(Dim))
		err := c.cli.CreateCollection(ctx, milvusclient.NewCreateCollectionOption(CollectionDevices, schema).
			WithIndexOptions(milvusclient.NewCreateIndexOption(CollectionDevices, "vec", index.NewAutoIndex(entity.COSINE)).WithIndexName("vec")))
		if err != nil {
			return err
		}
	}
	task, err := c.cli.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(CollectionDevices))
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

// Device 一行设备向量
type Device struct {
	AppID     int64
	DeviceID  string
	UserID    int64
	Banned    bool
	UpdatedAt int64
	Vec       []float32
}

// Key 主键
func Key(appID int64, deviceID string) string {
	k := fmt.Sprintf("%d:%s", appID, deviceID)
	if len(k) > 160 {
		k = k[:160]
	}
	return k
}

// Upsert 批量写（按主键幂等）
func (c *Client) Upsert(ctx context.Context, rows []Device) error {
	if len(rows) == 0 {
		return nil
	}
	n := len(rows)
	keys, apps, devs, users, banned, updated := make([]string, n), make([]int64, n), make([]string, n), make([]int64, n), make([]bool, n), make([]int64, n)
	vecs := make([][]float32, n)
	for i, r := range rows {
		keys[i], apps[i], devs[i], users[i], banned[i], updated[i], vecs[i] = Key(r.AppID, r.DeviceID), r.AppID, truncate(r.DeviceID, 128), r.UserID, r.Banned, r.UpdatedAt, r.Vec
	}
	_, err := c.cli.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(CollectionDevices).
		WithVarcharColumn("key", keys).WithInt64Column("app_id", apps).WithVarcharColumn("device_id", devs).
		WithInt64Column("user_id", users).WithBoolColumn("banned", banned).WithInt64Column("updated_at", updated).
		WithFloatVectorColumn("vec", Dim, vecs))
	return err
}

// SetBanned 改封禁标记：部分更新只改这一列。设备还没有向量（从没带指纹评估过）时没有可改的行，
// 之后第一次写入向量时 risk-ingest 会按名单带上封禁标记。
func (c *Client) SetBanned(ctx context.Context, appID int64, deviceID string, banned bool) error {
	_, err := c.cli.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(CollectionDevices).
		WithVarcharColumn("key", []string{Key(appID, deviceID)}).WithInt64Column("app_id", []int64{appID}).
		WithBoolColumn("banned", []bool{banned}).WithPartialUpdate(true))
	if err != nil && strings.Contains(err.Error(), "does not exist") {
		return nil
	}
	return err
}

// Neighbor 一个相似设备
type Neighbor struct {
	DeviceID string  `json:"deviceId"`
	UserID   int64   `json:"userId"`
	Banned   bool    `json:"banned"`
	Score    float32 `json:"score"`
}

// Similar 同应用里与 vec 最相似的设备（排除自己）
func (c *Client) Similar(ctx context.Context, appID int64, self string, vec []float32, topK int) ([]Neighbor, error) {
	if Empty(vec) {
		return nil, nil
	}
	rs, err := c.cli.Search(ctx, milvusclient.NewSearchOption(CollectionDevices, topK+1, []entity.Vector{entity.FloatVector(vec)}).
		WithANNSField("vec").
		WithFilter("app_id == {app} && key != {self}").WithTemplateParam("app", appID).WithTemplateParam("self", Key(appID, self)).
		WithOutputFields("device_id", "user_id", "banned").
		WithConsistencyLevel(entity.ClBounded))
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, nil
	}
	r := rs[0]
	if r.ResultCount == 0 {
		return nil, nil
	}
	devCol, userCol, banCol := r.GetColumn("device_id"), r.GetColumn("user_id"), r.GetColumn("banned")
	if devCol == nil || userCol == nil || banCol == nil {
		return nil, errors.New("milvus 结果缺列")
	}
	out := make([]Neighbor, 0, r.ResultCount)
	for i := 0; i < r.ResultCount && len(out) < topK; i++ {
		d, _ := devCol.GetAsString(i)
		u, _ := userCol.GetAsInt64(i)
		b, _ := banCol.GetAsBool(i)
		out = append(out, Neighbor{DeviceID: d, UserID: u, Banned: b, Score: r.Scores[i]})
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
