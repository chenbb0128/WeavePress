package database

import (
	"context"
	"database/sql"
	"errors"
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Transactor[S any] struct {
	db       *sql.DB
	newStore func(DBTX) S
}

func NewTransactor[S any](db *sql.DB, newStore func(DBTX) S) *Transactor[S] {
	return &Transactor[S]{db: db, newStore: newStore}
}

func (t *Transactor[S]) WithinTx(ctx context.Context, opts *sql.TxOptions, fn func(context.Context, S) error) (err error) {
	tx, err := t.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	store := t.newStore(tx)
	if err = fn(ctx, store); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
