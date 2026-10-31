package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolFactory is the connection boundary for the isolated platform database.
// Runtime callers must provide the tp_runtime role and never a migrator role.
type PoolFactory func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error)

type ReadOnlyAdapter struct {
	pool *pgxpool.Pool
}

func NewReadOnlyAdapter(pool *pgxpool.Pool) *ReadOnlyAdapter {
	return &ReadOnlyAdapter{pool: pool}
}

func (adapter *ReadOnlyAdapter) Enabled() bool {
	return adapter != nil && adapter.pool != nil
}

func (adapter *ReadOnlyAdapter) Pool() *pgxpool.Pool {
	if adapter == nil {
		return nil
	}
	return adapter.pool
}
