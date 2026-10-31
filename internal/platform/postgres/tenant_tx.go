package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/platform/tenancy"
)

func WithTenantTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	access tenancy.Context,
	fn func(context.Context, pgx.Tx) error,
) error {
	if pool == nil {
		return errors.New("postgres pool is required")
	}
	if fn == nil {
		return errors.New("transaction function is required")
	}
	if err := access.Validate(); err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocalContext(ctx, tx, access); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func setLocalContext(ctx context.Context, tx pgx.Tx, access tenancy.Context) error {
	tenantID := ""
	if access.TenantID != nil {
		tenantID = access.TenantID.String()
	}
	_, err := tx.Exec(ctx, `
		SELECT
			set_config('app.access_mode', $1, true),
			set_config('app.tenant_id', $2, true)
	`, string(access.Mode), tenantID)
	return err
}
