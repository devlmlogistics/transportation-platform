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
	// This function only establishes the transaction-local RLS routing context.
	// It does not authenticate the actor or authorize the requested resource.
	// Administrative services must evaluate AuthorizationRepository.Authorize
	// before invoking a mutating repository.
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

// WithAuthorizedTenantTx is the boundary intended for future administrative
// services. It evaluates the server-side principal and exact authorization
// request inside the same transaction that receives the RLS context. A denied
// request never invokes fn and is rolled back.
func WithAuthorizedTenantTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	access tenancy.Context,
	request AuthorizationRequest,
	fn func(context.Context, pgx.Tx) error,
) (AuthorizationDecision, error) {
	if pool == nil {
		return AuthorizationDecision{}, errors.New("postgres pool is required")
	}
	if fn == nil {
		return AuthorizationDecision{}, errors.New("transaction function is required")
	}
	if err := access.Validate(); err != nil {
		return AuthorizationDecision{}, err
	}
	if err := validateAuthorizedContext(access, request); err != nil {
		return AuthorizationDecision{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocalContext(ctx, tx, access); err != nil {
		return AuthorizationDecision{}, err
	}
	decision, err := (AuthorizationRepository{}).Authorize(ctx, tx, request)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	if !decision.Allowed {
		return decision, nil
	}
	if err := fn(ctx, tx); err != nil {
		return decision, err
	}
	if err := tx.Commit(ctx); err != nil {
		return decision, err
	}
	return decision, nil
}

func validateAuthorizedContext(access tenancy.Context, request AuthorizationRequest) error {
	if access.Mode == tenancy.AccessModePlatform {
		if request.ScopeType != "PLATFORM" {
			return errors.New("platform transaction requires platform authorization scope")
		}
		return nil
	}
	if request.ScopeType == "PLATFORM" || request.CompanyID == nil || access.TenantID == nil || *request.CompanyID != *access.TenantID {
		return errors.New("tenant transaction does not match authorization scope")
	}
	return nil
}
