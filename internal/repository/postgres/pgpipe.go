package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgPipe 把互不依赖的语句攒成一批，一次往返发出去（pgx 的批量管道：语句按入队顺序在同一条连接上执行，
// 结果按顺序交给各自的回调）。
//
// 回调里返回错误会让这一批后面的回调都不再执行，所以「查不到」这类正常情况要在回调里自己记下来、返回 nil；
// 只有真正的错误才返回。语句本身出错（在事务里）时整个事务进入中止状态，send 返回第一个错误。
type pgPipe struct {
	batch pgx.Batch
}

// batchSender 能发批次的：事务、保存点、连接池。
type batchSender interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

func (p *pgPipe) exec(sql string, args ...any) {
	p.batch.Queue(sql, args...).Exec(func(pgconn.CommandTag) error { return nil })
}

func (p *pgPipe) execTag(fn func(pgconn.CommandTag) error, sql string, args ...any) {
	p.batch.Queue(sql, args...).Exec(fn)
}

func (p *pgPipe) row(scan func(pgx.Row) error, sql string, args ...any) {
	p.batch.Queue(sql, args...).QueryRow(scan)
}

func (p *pgPipe) rows(scan func(pgx.Rows) error, sql string, args ...any) {
	p.batch.Queue(sql, args...).Query(scan)
}

// len 已入队的语句数。
func (p *pgPipe) len() int { return p.batch.Len() }

// send 一次发出全部语句并等全部结果；之后可以继续入队、再发。
func (p *pgPipe) send(ctx context.Context, to batchSender) error {
	if p.batch.Len() == 0 {
		return nil
	}
	err := to.SendBatch(ctx, &p.batch).Close()
	p.batch = pgx.Batch{}
	return err
}
