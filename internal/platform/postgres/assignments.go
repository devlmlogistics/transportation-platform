package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RoleAssignmentRecord struct {
	ID                 uuid.UUID
	PersonID           uuid.UUID
	RoleCode           string
	ScopeType          string
	CompanyID          *uuid.UUID
	CompanyOperationID *uuid.UUID
	ValidFrom          time.Time
	ValidUntil         *time.Time
	GrantedByAccountID *uuid.UUID
}

type PermissionGrantRecord struct {
	ID                 uuid.UUID
	PersonID           uuid.UUID
	PermissionCode     string
	ScopeType          string
	CompanyID          *uuid.UUID
	CompanyOperationID *uuid.UUID
	ValidFrom          time.Time
	ValidUntil         *time.Time
	GrantedByAccountID *uuid.UUID
}

func CreateRoleAssignment(ctx context.Context, tx pgx.Tx, assignment RoleAssignmentRecord) error {
	if assignment.ID == uuid.Nil || assignment.PersonID == uuid.Nil || assignment.RoleCode == "" {
		return errors.New("role assignment identity is incomplete")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.role_assignment (
			id, person_id, role_code, scope_type, company_id, company_operation_id,
			valid_from, valid_until, granted_by_user_account_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, assignment.ID, assignment.PersonID, assignment.RoleCode, assignment.ScopeType,
		assignment.CompanyID, assignment.CompanyOperationID, assignment.ValidFrom,
		assignment.ValidUntil, assignment.GrantedByAccountID)
	return err
}

func RevokeRoleAssignment(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	var lifecycleStatus string
	if err := tx.QueryRow(ctx, `
		SELECT lifecycle_status
		FROM platform.role_assignment
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&lifecycleStatus); err != nil {
		return err
	}
	if lifecycleStatus != "ACTIVE" {
		return pgx.ErrNoRows
	}
	result, err := tx.Exec(ctx, `
		UPDATE platform.role_assignment
		SET lifecycle_status = 'REVOKED', revoked_at = $2
		WHERE id = $1
	`, id, at)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func ExpireRoleAssignment(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	var lifecycleStatus string
	var validUntil *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT lifecycle_status, valid_until
		FROM platform.role_assignment
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&lifecycleStatus, &validUntil); err != nil {
		return err
	}
	if lifecycleStatus != "ACTIVE" || validUntil == nil || validUntil.After(at) {
		return pgx.ErrNoRows
	}
	result, err := tx.Exec(ctx, `
		UPDATE platform.role_assignment
		SET lifecycle_status = 'EXPIRED', expired_at = $2
		WHERE id = $1
	`, id, at)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func CreatePermissionGrant(ctx context.Context, tx pgx.Tx, grant PermissionGrantRecord) error {
	if grant.ID == uuid.Nil || grant.PersonID == uuid.Nil || grant.PermissionCode == "" {
		return errors.New("permission grant identity is incomplete")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.permission_grant (
			id, person_id, permission_code, scope_type, company_id, company_operation_id,
			valid_from, valid_until, granted_by_user_account_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, grant.ID, grant.PersonID, grant.PermissionCode, grant.ScopeType,
		grant.CompanyID, grant.CompanyOperationID, grant.ValidFrom,
		grant.ValidUntil, grant.GrantedByAccountID)
	return err
}

func RevokePermissionGrant(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	var lifecycleStatus string
	if err := tx.QueryRow(ctx, `
		SELECT lifecycle_status
		FROM platform.permission_grant
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&lifecycleStatus); err != nil {
		return err
	}
	if lifecycleStatus != "ACTIVE" {
		return pgx.ErrNoRows
	}
	result, err := tx.Exec(ctx, `
		UPDATE platform.permission_grant
		SET lifecycle_status = 'REVOKED', revoked_at = $2
		WHERE id = $1
	`, id, at)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func ExpirePermissionGrant(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	var lifecycleStatus string
	var validUntil *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT lifecycle_status, valid_until
		FROM platform.permission_grant
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&lifecycleStatus, &validUntil); err != nil {
		return err
	}
	if lifecycleStatus != "ACTIVE" || validUntil == nil || validUntil.After(at) {
		return pgx.ErrNoRows
	}
	result, err := tx.Exec(ctx, `
		UPDATE platform.permission_grant
		SET lifecycle_status = 'EXPIRED', expired_at = $2
		WHERE id = $1
	`, id, at)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}
