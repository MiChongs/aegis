// Package vector 设备标识向量（Milvus）。
//
// 只看设备码本身，不看品牌、型号、系统版本：客户端的设备码由硬件信号加盐哈希得到（重装不变），
// 正常设备之间毫无相似之处；同型号的手机在型号层面一模一样，按型号比只会把正常用户都算成「设备农场」。
// 真正可疑的是脚本批量生成或改机工具改出来的设备码：同一个模板、只差末尾几位计数、或在真码上改一两个字符。
//
// 做法：每台设备一行（主键 应用:设备码），向量是设备码的字符三元组与「第 i 位是什么字符」做带符号的特征哈希
// （256 维，L2 归一，COSINE），只用来召回候选；候选再按两个设备码的编辑距离精确打分：
// 分数 = 1 − 编辑距离 ÷ 较长的长度。32 位设备码差 1 个字符得 0.969，64 位差 1 个得 0.984、差 3 个得 0.953，
// 互不相干的随机码在 0.1 以下。特征阈值（0.97 / 0.95）因此就是「只差一两个字符」。
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

	"aegis/services/risk/events"
	"aegis/services/risk/internal/config"

	"github.com/milvus-io/milvus/client/v3/entity"
	"github.com/milvus-io/milvus/client/v3/index"
	"github.com/milvus-io/milvus/client/v3/milvusclient"
)

// Dim 向量维数
const Dim = 256

// CollectionDevices 设备集合（按设备码建向量）
const CollectionDevices = "risk_device_ids"

// legacyCollections 以前按型号要素建的集合，迁移时删掉
var legacyCollections = []string{"risk_devices"}

// normalize 比较用的形式：去空白、小写；不是设备码的值（占位值、UA 摘要）得到空串
func normalize(id string) string {
	return strings.ToLower(events.NormalizeDeviceID(id))
}

// Embed 设备码 → 召回向量。不是有效设备码时返回全零向量（见 Empty）。
func Embed(deviceID string) []float32 {
	id := normalize(deviceID)
	out := make([]float32, Dim)
	if id == "" {
		return out
	}
	vec := make([]float64, Dim)
	add := func(token string, w float64) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		sum := h.Sum64()
		sign := 1.0
		if (sum>>63)&1 == 1 {
			sign = -1
		}
		vec[int(sum%Dim)] += sign * w
	}
	rs := []rune(id)
	for i, r := range rs {
		add(fmt.Sprintf("p%d=%c", i, r), 1)
	}
	for i := 0; i+3 <= len(rs); i++ {
		add("g="+string(rs[i:i+3]), 1)
	}
	add(fmt.Sprintf("len=%d", len(rs)), 1)
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return out
	}
	norm = math.Sqrt(norm)
	for i, v := range vec {
		out[i] = float32(v / norm)
	}
	return out
}

// Empty 向量全零（不是有效设备码）
func Empty(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// Similarity 两个设备码的相似度：1 − 编辑距离 ÷ 较长的长度，0–1
func Similarity(a, b string) float32 {
	x, y := []rune(normalize(a)), []rune(normalize(b))
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	n := len(x)
	if len(y) > n {
		n = len(y)
	}
	return float32(1 - float64(levenshtein(x, y))/float64(n))
}

func levenshtein(a, b []rune) int {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
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
	for _, old := range legacyCollections {
		had, err := c.cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(old))
		if err != nil {
			return err
		}
		if had {
			if err := c.cli.DropCollection(ctx, milvusclient.NewDropCollectionOption(old)); err != nil {
				return err
			}
		}
	}
	has, err := c.cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(CollectionDevices))
	if err != nil {
		return err
	}
	if !has {
		schema := entity.NewSchema().WithName(CollectionDevices).WithDescription("风控设备标识向量").
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

// Similar 同应用里设备码与 self 最相近的设备（排除自己），按相似度从高到低。
// 先按向量召回较多的候选，再逐个按编辑距离打分重排；self 不是有效设备码时没有结果。
func (c *Client) Similar(ctx context.Context, appID int64, self string, topK int) ([]Neighbor, error) {
	vec := Embed(self)
	if Empty(vec) || topK <= 0 {
		return nil, nil
	}
	recall := topK * 4
	if recall < 40 {
		recall = 40
	} else if recall > 200 {
		recall = 200
	}
	rs, err := c.cli.Search(ctx, milvusclient.NewSearchOption(CollectionDevices, recall, []entity.Vector{entity.FloatVector(vec)}).
		WithANNSField("vec").
		WithFilter("app_id == {app} && key != {self}").WithTemplateParam("app", appID).WithTemplateParam("self", Key(appID, strings.TrimSpace(self))).
		WithOutputFields("device_id", "user_id", "banned").
		WithConsistencyLevel(entity.ClBounded))
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 || rs[0].ResultCount == 0 {
		return nil, nil
	}
	r := rs[0]
	devCol, userCol, banCol := r.GetColumn("device_id"), r.GetColumn("user_id"), r.GetColumn("banned")
	if devCol == nil || userCol == nil || banCol == nil {
		return nil, errors.New("milvus 结果缺列")
	}
	me := normalize(self)
	out := make([]Neighbor, 0, r.ResultCount)
	for i := 0; i < r.ResultCount; i++ {
		d, _ := devCol.GetAsString(i)
		if normalize(d) == me {
			continue
		}
		u, _ := userCol.GetAsInt64(i)
		b, _ := banCol.GetAsBool(i)
		out = append(out, Neighbor{DeviceID: d, UserID: u, Banned: b, Score: Similarity(self, d)})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
