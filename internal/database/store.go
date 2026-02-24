package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"runtime"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	Querier
	Close() error
	UpdateSeriesMetadata(ctx context.Context, arg UpdateSeriesMetadataParams) (Series, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
}

type sqlStore struct {
	*Queries
	db *sql.DB
}

func NewStore(dbPath string) (Store, error) {
	// 加载现代 SQLite 对于千兆以上规模及大量随机读取极其友好的调教参数。
	// mmap_size=30000000000 (允许超过系统内存约30GB的超大内存隐射加快搜索页的读取，极大地减轻冷启动延迟)
	// cache_size=-500000  (单独为SQLite划定高达并超过 500MB 的专用查询热缓存页)
	// busy_timeout=15000  (防止在长列表遍历且伴随有并发写入时的死锁退出报错)
	// temp_store=2        (MEMORY：所有临时聚合、ORDER BY 操作与临时表将完全使用内存而非耗损SSD)
	dsn := dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=mmap_size=30000000000&_pragma=cache_size=-500000&_pragma=busy_timeout=15000&_pragma=temp_store=2"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// 开启连接池支持
	// 对于现代无并发 CGO 限制的 purely go sqlite，我们设置并行度
	maxConns := runtime.NumCPU() * 2
	if maxConns < 8 {
		maxConns = 8
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)

	return &sqlStore{
		Queries: New(db),
		db:      db,
	}, nil
}

func (s *sqlStore) Close() error {
	return s.db.Close()
}

// ExecTx 提供一个事务包裹器以进行批量执行，这对防止 SQLite 并发锁极为关键
func (s *sqlStore) ExecTx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	q := s.Queries.WithTx(tx)
	if err := fn(q); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

// 供启动时执行迁移
func Migrate(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(schemaSQL)
	return err
}
