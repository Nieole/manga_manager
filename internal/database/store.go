package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	Querier
	Close() error
	ExecTx(ctx context.Context, fn func(*Queries) error) error
}

type sqlStore struct {
	*Queries
	db *sql.DB
}

func NewStore(dbPath string) (Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

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
