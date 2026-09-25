package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB 封装 pgx 连接池，提供与 Prisma 等价的常用查询能力。
type DB struct {
	Pool *pgxpool.Pool
}

// New 创建连接池。DATABASE_URL 为空时返回错误。
func New(ctx context.Context, databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, errMissingDSN
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (d *DB) Close() { d.Pool.Close() }

// QueryRowToStruct 查询单行并映射到结构体（字段按 `db` tag 匹配列名）。
func QueryRowToStruct[T any](ctx context.Context, d *DB, sql string, args ...any) (*T, error) {
	rows, err := d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	v, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// QueryToStructs 查询多行并映射到结构体切片。
func QueryToStructs[T any](ctx context.Context, d *DB, sql string, args ...any) ([]T, error) {
	rows, err := d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[T])
}

// Exec 执行写操作。
func (d *DB) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := d.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// IsNotFound 判断是否为 pgx.ErrNoRows。
func IsNotFound(err error) bool { return err == pgx.ErrNoRows }
