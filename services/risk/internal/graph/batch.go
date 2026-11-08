package graph

import (
	"fmt"
	"strings"
)

// Batch 聚合写入（全量回灌用）。与事件写入的区别：每条边带的是汇总后的首次 / 最近时间与次数，
// 首次取最小、最近取最大、次数取较大值——重复回灌不会把次数越加越多，与实时事件的累加也不冲突。
type Batch struct {
	c       *Client
	b       strings.Builder
	n       int
	maxStmt int
	// Written 已写出的语句数（按点类型 / 边类型）
	Written map[string]int
}

// MaxStatements 一次提交的语句上限。Nebula 把分号连起来的语句编成一棵执行计划树，
// 深度超过 512 层报 "The depth of plan tree has exceeded the max 512 level"；每条 UPSERT 占几层，80 条留足余量。
const MaxStatements = 80

// NewBatch 每满 maxStmt 条语句提交一次（不超过 MaxStatements）
func (c *Client) NewBatch(maxStmt int) *Batch {
	if maxStmt <= 0 || maxStmt > MaxStatements {
		maxStmt = MaxStatements
	}
	return &Batch{c: c, maxStmt: maxStmt, Written: map[string]int{}}
}

func span(first, last int64) (int64, int64) {
	if first <= 0 {
		first = last
	}
	if last < first {
		last = first
	}
	return first, last
}

// Vertex 写一个点：首次 / 最近时间取极值，sets 是要覆盖的属性（setStr 等拼好的 "k = v"）
func (bt *Batch) Vertex(tag, vid string, first, last int64, sets ...string) error {
	if vid == "" {
		return nil
	}
	first, last = span(first, last)
	all := sets
	if last > 0 {
		all = append([]string{
			fmt.Sprintf("first_seen = CASE WHEN first_seen == 0 OR first_seen > %d THEN %d ELSE first_seen END", first, first),
			fmt.Sprintf("last_seen = CASE WHEN last_seen < %d THEN %d ELSE last_seen END", last, last),
		}, sets...)
	}
	if len(all) == 0 {
		return nil
	}
	fmt.Fprintf(&bt.b, "UPSERT VERTEX ON %s %s SET %s;", tag, Quote(vid), strings.Join(all, ", "))
	bt.Written[tag]++
	return bt.tick()
}

// Edge 写一条边（汇总值）
func (bt *Batch) Edge(edge, src, dst string, first, last, count int64) error {
	if src == "" || dst == "" || src == dst {
		return nil
	}
	first, last = span(first, last)
	if count <= 0 {
		count = 1
	}
	fmt.Fprintf(&bt.b, "UPSERT EDGE ON %s %s -> %s SET first_seen = CASE WHEN first_seen == 0 OR first_seen > %d THEN %d ELSE first_seen END, last_seen = CASE WHEN last_seen < %d THEN %d ELSE last_seen END, count = CASE WHEN count < %d THEN %d ELSE count END;",
		edge, Quote(src), Quote(dst), first, first, last, last, count, count)
	bt.Written[edge]++
	return bt.tick()
}

func (bt *Batch) tick() error {
	bt.n++
	if bt.n >= bt.maxStmt || bt.b.Len() > 512<<10 {
		return bt.Flush()
	}
	return nil
}

// Flush 提交缓冲的语句
func (bt *Batch) Flush() error {
	if bt.n == 0 {
		return nil
	}
	stmt := bt.b.String()
	bt.b.Reset()
	bt.n = 0
	_, err := bt.c.Exec(stmt)
	return err
}

// Set 拼一个字符串属性赋值（空值返回空串，调用方过滤）
func Set(k, v string) string {
	if v == "" {
		return ""
	}
	return setStr(k, v)
}

// SetInt 整数属性
func SetInt(k string, v int64) string { return fmt.Sprintf("%s = %d", k, v) }

// SetBool 布尔属性
func SetBool(k string, v bool) string { return fmt.Sprintf("%s = %t", k, v) }

// Sets 去掉空赋值
func Sets(xs ...string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
