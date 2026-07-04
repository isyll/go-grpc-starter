// Package store is the pgx + sqlc data-access layer. It owns the connection
// pool and runs every operation inside a transaction that applies the
// row-level-security GUCs the schema's audit triggers and RLS policies read.
package store

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/authz"
	"github.com/isyll/go-grpc-starter/internal/persistence"
)

// Store wraps a pgx pool and the sqlc queries.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: db.New(pool)}
}

// Pool exposes the underlying pool for health checks and shutdown.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// InTx reports whether ctx already carries an open store transaction.
func InTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(*db.Queries)
	return ok
}

type (
	txKey    struct{}
	txRawKey struct{}
)

// Run executes fn with sqlc queries bound to the ambient transaction if one is
// present in ctx, otherwise inside a fresh RLS-scoped transaction.
func (s *Store) Run(
	ctx context.Context,
	fn func(ctx context.Context, q *db.Queries) error,
) error {
	if q, ok := ctx.Value(txKey{}).(*db.Queries); ok {
		persistence.IncrQueryCounter(ctx)
		return fn(ctx, q)
	}
	return s.WithTx(ctx, func(ctx context.Context) error {
		persistence.IncrQueryCounter(ctx)
		return fn(ctx, ctx.Value(txKey{}).(*db.Queries))
	})
}

// WithTx runs fn inside a single transaction. Repositories that call Run within
// fn share that transaction, so a service can compose several writes (and an
// outbox publish) atomically. RLS GUCs are applied once at the start.
func (s *Store) WithTx(
	ctx context.Context,
	fn func(ctx context.Context) error,
) error {
	if _, ok := ctx.Value(txKey{}).(*db.Queries); ok {
		return fn(ctx) // already inside a transaction; join it
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := applyRLS(ctx, tx); err != nil {
		return err
	}

	ctx = context.WithValue(ctx, txKey{}, s.q.WithTx(tx))
	ctx = context.WithValue(ctx, txRawKey{}, tx)
	if err := fn(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetChangeReason sets the app.change_reason GUC on the ambient transaction so
// the status/suspension audit triggers record it. No-op outside a transaction.
func SetChangeReason(ctx context.Context, reason string) error {
	tx, ok := ctx.Value(txRawKey{}).(pgx.Tx)
	if !ok {
		return nil
	}
	_, err := tx.Exec(ctx, "SELECT set_config('app.change_reason', $1, true)", reason)
	return err
}

// applyRLS sets the per-request GUCs the schema reads: app.current_user_id
// (recorded as changed_by by audit triggers) and app.current_role.
func applyRLS(ctx context.Context, tx pgx.Tx) error {
	s := authz.From(ctx)
	userID := "0"
	role := string(authz.RoleAnonymous)
	switch {
	case s.IsAdmin:
		role = string(authz.RoleAdmin)
	case s.UserID > 0:
		userID = strconv.FormatInt(s.UserID, 10)
		if s.Role != "" {
			role = string(s.Role)
		}
	}
	_, err := tx.Exec(
		ctx,
		`SELECT set_config('app.current_user_id', $1, true),
		        set_config('app.current_role', $2, true)`,
		userID, role,
	)
	return err
}
