package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/platform/postgres"
	"transportation-platform/internal/platform/tenancy"
)

var (
	ErrForbidden                    = errors.New("administrative action is not authorized")
	ErrInvalidInput                 = errors.New("invalid administrative input")
	ErrNotFound                     = errors.New("administrative resource not found")
	ErrConflict                     = errors.New("administrative resource conflict")
	ErrIdentityAttachRequiresFlow   = errors.New("attaching an existing global identity requires the approved identity flow")
	ErrPlatformMutationNotSupported = errors.New("platform mutation is not exposed by this service")
)

type Service struct {
	Pool  *pgxpool.Pool
	Clock func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{Pool: pool, Clock: func() time.Time { return time.Now().UTC() }}
}

type CompanyInput struct {
	Slug        string  `json:"slug"`
	LegalName   string  `json:"legal_name"`
	DisplayName *string `json:"display_name"`
}

type CompanyUpdateInput struct {
	Slug        *string `json:"slug"`
	LegalName   *string `json:"legal_name"`
	DisplayName *string `json:"display_name"`
	Status      *string `json:"status"`
}

type OperationView struct {
	ID         uuid.UUID  `json:"id"`
	CompanyID  uuid.UUID  `json:"company_id"`
	TypeCode   string     `json:"operation_type"`
	Status     string     `json:"status"`
	EnabledAt  time.Time  `json:"enabled_at"`
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
}

type OperationInput struct {
	OperationType string `json:"operation_type"`
}

type MembershipView struct {
	ID         uuid.UUID  `json:"id"`
	CompanyID  uuid.UUID  `json:"company_id"`
	PersonID   uuid.UUID  `json:"person_id"`
	Status     string     `json:"status"`
	ValidFrom  time.Time  `json:"valid_from"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
}

type OperationMembershipInput struct {
	OperationID uuid.UUID `json:"operation_id"`
	PersonID    uuid.UUID `json:"person_id"`
	Status      string    `json:"status"`
}

type RoleAssignmentInput struct {
	PersonID           uuid.UUID  `json:"person_id"`
	RoleCode           string     `json:"role_code"`
	ScopeType          string     `json:"scope_type"`
	CompanyID          *uuid.UUID `json:"company_id,omitempty"`
	CompanyOperationID *uuid.UUID `json:"company_operation_id,omitempty"`
	ValidFrom          time.Time  `json:"valid_from"`
	ValidUntil         *time.Time `json:"valid_until,omitempty"`
}

type PermissionGrantInput struct {
	PersonID           uuid.UUID  `json:"person_id"`
	PermissionCode     string     `json:"permission_code"`
	ScopeType          string     `json:"scope_type"`
	CompanyID          *uuid.UUID `json:"company_id,omitempty"`
	CompanyOperationID *uuid.UUID `json:"company_operation_id,omitempty"`
	ValidFrom          time.Time  `json:"valid_from"`
	ValidUntil         *time.Time `json:"valid_until,omitempty"`
}

type AuditView struct {
	ID           uuid.UUID      `json:"id"`
	OccurredAt   time.Time      `json:"occurred_at"`
	ActorAccount *uuid.UUID     `json:"actor_user_account_id,omitempty"`
	CompanyID    *uuid.UUID     `json:"company_id,omitempty"`
	OperationID  *uuid.UUID     `json:"company_operation_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   *string        `json:"resource_id,omitempty"`
	RequestID    *string        `json:"request_id,omitempty"`
	Details      map[string]any `json:"details"`
}

func (s *Service) GetCompany(ctx context.Context, principal postgres.Principal, companyID uuid.UUID) (postgres.CompanyRecord, error) {
	var result postgres.CompanyRecord
	err := s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "company.read",
	}, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		result, err = (postgres.CompanyRepository{}).Get(ctx, tx, companyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return result, err
}

func (s *Service) CreateCompany(ctx context.Context, principal postgres.Principal, input CompanyInput) (postgres.CompanyRecord, error) {
	if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.LegalName) == "" {
		return postgres.CompanyRecord{}, ErrInvalidInput
	}
	company := postgres.CompanyRecord{ID: uuid.New(), Slug: input.Slug, LegalName: input.LegalName, DisplayName: input.DisplayName, Status: "ACTIVE"}
	err := s.run(ctx, tenancy.Platform(), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "PLATFORM", PermissionCode: "platform.company.create",
	}, func(ctx context.Context, tx pgx.Tx) error {
		if err := (postgres.CompanyRepository{}).Create(ctx, tx, company); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		return writeAdminAudit(ctx, tx, principal, nil, nil, "company.create", "company", company.ID.String())
	})
	if err != nil {
		return postgres.CompanyRecord{}, err
	}
	return company, nil
}

func (s *Service) UpdateCompany(ctx context.Context, principal postgres.Principal, companyID uuid.UUID, input CompanyUpdateInput) error {
	fields := 0
	if input.Slug != nil || input.LegalName != nil || input.DisplayName != nil {
		fields++
	}
	if input.Status != nil {
		fields++
	}
	if fields != 1 {
		return ErrInvalidInput
	}
	permission, action := "company.update", "company.update"
	if input.Status != nil {
		permission, action = "company.status.manage", "company.status.manage"
	}
	return s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: permission,
	}, func(ctx context.Context, tx pgx.Tx) error {
		var command string
		var args []any
		switch {
		case input.Slug != nil:
			command, args = `UPDATE platform.company SET slug = $1, updated_at = now() WHERE id = $2`, []any{strings.ToLower(strings.TrimSpace(*input.Slug)), companyID}
		case input.LegalName != nil:
			command, args = `UPDATE platform.company SET legal_name = $1, updated_at = now() WHERE id = $2`, []any{strings.TrimSpace(*input.LegalName), companyID}
		case input.DisplayName != nil:
			command, args = `UPDATE platform.company SET display_name = $1, updated_at = now() WHERE id = $2`, []any{*input.DisplayName, companyID}
		case input.Status != nil:
			command, args = `UPDATE platform.company SET status = $1, updated_at = now() WHERE id = $2`, []any{strings.ToUpper(strings.TrimSpace(*input.Status)), companyID}
		}
		result, err := tx.Exec(ctx, command, args...)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrNotFound
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, nil, action, "company", companyID.String())
	})
}

func (s *Service) ListOperations(ctx context.Context, principal postgres.Principal, companyID uuid.UUID) ([]OperationView, error) {
	var result []OperationView
	err := s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "company.operation.read",
	}, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, company_id, operation_type_code, status, enabled_at, disabled_at
			FROM platform.company_operation
			WHERE company_id = $1 ORDER BY operation_type_code
		`, companyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item OperationView
			if err := rows.Scan(&item.ID, &item.CompanyID, &item.TypeCode, &item.Status, &item.EnabledAt, &item.DisabledAt); err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

func (s *Service) EnableOperation(ctx context.Context, principal postgres.Principal, companyID uuid.UUID, operationType string) (OperationView, error) {
	operationType = strings.ToUpper(strings.TrimSpace(operationType))
	if operationType == "" {
		return OperationView{}, ErrInvalidInput
	}
	var result OperationView
	err := s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "company.operation.manage",
	}, func(ctx context.Context, tx pgx.Tx) error {
		var catalogStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM platform.operation_type WHERE code = $1`, operationType).Scan(&catalogStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if catalogStatus != "ACTIVE" {
			return ErrConflict
		}
		result.ID = uuid.New()
		result.CompanyID = companyID
		result.TypeCode = operationType
		result.Status = "ACTIVE"
		result.EnabledAt = s.now()
		if _, err := tx.Exec(ctx, `
			INSERT INTO platform.company_operation (id, company_id, operation_type_code, status, enabled_at)
			VALUES ($1, $2, $3, 'ACTIVE', $4)
		`, result.ID, companyID, operationType, result.EnabledAt); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, &result.ID, "company.operation.enable", "company_operation", result.ID.String())
	})
	return result, err
}

func (s *Service) SetOperationStatus(ctx context.Context, principal postgres.Principal, companyID, operationID uuid.UUID, status string) error {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "ACTIVE" && status != "SUSPENDED" && status != "ARCHIVED" {
		return ErrInvalidInput
	}
	return s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "company.operation.manage",
	}, func(ctx context.Context, tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE platform.company_operation
			SET status = $1, disabled_at = CASE WHEN $1 = 'ACTIVE' THEN NULL ELSE COALESCE(disabled_at, now()) END, updated_at = now()
			WHERE id = $2 AND company_id = $3
		`, status, operationID, companyID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrNotFound
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, &operationID, "company.operation.status.manage", "company_operation", operationID.String())
	})
}

func (s *Service) ListMemberships(ctx context.Context, principal postgres.Principal, companyID uuid.UUID) ([]MembershipView, error) {
	var result []MembershipView
	err := s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "membership.read",
	}, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, company_id, person_id, status, valid_from, valid_until
			FROM platform.company_membership
			WHERE company_id = $1 ORDER BY id
		`, companyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item MembershipView
			if err := rows.Scan(&item.ID, &item.CompanyID, &item.PersonID, &item.Status, &item.ValidFrom, &item.ValidUntil); err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

func (s *Service) SetMembershipStatus(ctx context.Context, principal postgres.Principal, companyID, membershipID uuid.UUID, status string) error {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "INVITED" && status != "ACTIVE" && status != "SUSPENDED" && status != "REVOKED" {
		return ErrInvalidInput
	}
	return s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "membership.manage",
	}, func(ctx context.Context, tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `UPDATE platform.company_membership SET status = $1, updated_at = now() WHERE id = $2 AND company_id = $3`, status, membershipID, companyID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrNotFound
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, nil, "membership.status.manage", "company_membership", membershipID.String())
	})
}

func (s *Service) SetOperationMembership(ctx context.Context, principal postgres.Principal, companyID, operationID, personID uuid.UUID, status string) error {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "ACTIVE" && status != "SUSPENDED" && status != "REVOKED" {
		return ErrInvalidInput
	}
	return s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "operation.membership.manage",
	}, func(ctx context.Context, tx pgx.Tx) error {
		var operationStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM platform.company_operation WHERE id = $1 AND company_id = $2`, operationID, companyID).Scan(&operationStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if operationStatus != "ACTIVE" && status == "ACTIVE" {
			return ErrConflict
		}
		result, err := tx.Exec(ctx, `
			INSERT INTO platform.operation_membership (
				id, company_id, company_operation_id, person_id, status, created_by_user_account_id
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (company_operation_id, person_id)
			DO UPDATE SET status = EXCLUDED.status, updated_at = now()
		`, uuid.New(), companyID, operationID, personID, status, principal.UserAccountID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrNotFound
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, &operationID, "operation.membership.status.manage", "operation_membership", personID.String())
	})
}

func (s *Service) AssignRole(ctx context.Context, principal postgres.Principal, input RoleAssignmentInput) error {
	input.ScopeType = strings.ToUpper(strings.TrimSpace(input.ScopeType))
	input.RoleCode = strings.ToUpper(strings.TrimSpace(input.RoleCode))
	if input.RoleCode == "GLOBAL_ADMIN" && input.ScopeType != "PLATFORM" {
		return ErrForbidden
	}
	if principal.PersonID == input.PersonID {
		return ErrForbidden
	}
	if err := validateAssignmentScope(input.ScopeType, input.CompanyID, input.CompanyOperationID); err != nil {
		return err
	}
	companyID, operationID, access, scope, permission := assignmentScope(input)
	return s.run(ctx, access, principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: scope, CompanyID: companyID, CompanyOperationID: operationID, PermissionCode: permission,
	}, func(ctx context.Context, tx pgx.Tx) error {
		if err := ensureRoleDelegation(ctx, tx, principal, input); err != nil {
			return err
		}
		if err := ensureTargetScope(ctx, tx, input.PersonID, input.ScopeType, input.CompanyID, input.CompanyOperationID); err != nil {
			return err
		}
		if input.ValidFrom.IsZero() {
			input.ValidFrom = s.now()
		}
		if err := postgres.CreateRoleAssignment(ctx, tx, postgres.RoleAssignmentRecord{
			ID: uuid.New(), PersonID: input.PersonID, RoleCode: input.RoleCode, ScopeType: input.ScopeType,
			CompanyID: input.CompanyID, CompanyOperationID: input.CompanyOperationID, ValidFrom: input.ValidFrom,
			ValidUntil: input.ValidUntil, GrantedByAccountID: ptrUUID(principal.UserAccountID),
		}); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		return writeAdminAudit(ctx, tx, principal, companyID, operationID, "role.assignment.create", "role_assignment", input.PersonID.String())
	})
}

func (s *Service) GrantPermission(ctx context.Context, principal postgres.Principal, input PermissionGrantInput) error {
	input.ScopeType = strings.ToUpper(strings.TrimSpace(input.ScopeType))
	input.PermissionCode = strings.TrimSpace(input.PermissionCode)
	if principal.PersonID == input.PersonID {
		return ErrForbidden
	}
	if err := validateAssignmentScope(input.ScopeType, input.CompanyID, input.CompanyOperationID); err != nil {
		return err
	}
	companyID, operationID, access, scope, permission := permissionScope(input)
	return s.run(ctx, access, principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: scope, CompanyID: companyID, CompanyOperationID: operationID, PermissionCode: permission,
	}, func(ctx context.Context, tx pgx.Tx) error {
		if err := ensurePermissionDelegation(ctx, tx, principal, input); err != nil {
			return err
		}
		if err := ensureTargetScope(ctx, tx, input.PersonID, input.ScopeType, input.CompanyID, input.CompanyOperationID); err != nil {
			return err
		}
		if input.ValidFrom.IsZero() {
			input.ValidFrom = s.now()
		}
		if err := postgres.CreatePermissionGrant(ctx, tx, postgres.PermissionGrantRecord{
			ID: uuid.New(), PersonID: input.PersonID, PermissionCode: input.PermissionCode, ScopeType: input.ScopeType,
			CompanyID: input.CompanyID, CompanyOperationID: input.CompanyOperationID, ValidFrom: input.ValidFrom,
			ValidUntil: input.ValidUntil, GrantedByAccountID: ptrUUID(principal.UserAccountID),
		}); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		return writeAdminAudit(ctx, tx, principal, companyID, operationID, "permission.grant.create", "permission_grant", input.PersonID.String())
	})
}

func (s *Service) RevokeRole(ctx context.Context, principal postgres.Principal, companyID, assignmentID uuid.UUID) error {
	return s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "role.assignment.manage",
	}, func(ctx context.Context, tx pgx.Tx) error {
		if err := postgres.RevokeRoleAssignment(ctx, tx, assignmentID, s.now()); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, nil, "role.assignment.revoke", "role_assignment", assignmentID.String())
	})
}

func (s *Service) RevokePermission(ctx context.Context, principal postgres.Principal, companyID, grantID uuid.UUID) error {
	return s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "permission.grant.manage",
	}, func(ctx context.Context, tx pgx.Tx) error {
		if err := postgres.RevokePermissionGrant(ctx, tx, grantID, s.now()); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return writeAdminAudit(ctx, tx, principal, &companyID, nil, "permission.grant.revoke", "permission_grant", grantID.String())
	})
}

func (s *Service) ListAudit(ctx context.Context, principal postgres.Principal, companyID uuid.UUID) ([]AuditView, error) {
	var result []AuditView
	err := s.run(ctx, tenancy.Tenant(companyID), principal, postgres.AuthorizationRequest{
		Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "audit.read",
	}, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, occurred_at, actor_user_account_id, company_id, company_operation_id,
			       action, resource_type, resource_id, request_id, details
			FROM platform.admin_audit_event
			WHERE company_id = $1 ORDER BY occurred_at DESC, id DESC LIMIT 200
		`, companyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item AuditView
			var raw []byte
			if err := rows.Scan(&item.ID, &item.OccurredAt, &item.ActorAccount, &item.CompanyID, &item.OperationID, &item.Action, &item.ResourceType, &item.ResourceID, &item.RequestID, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &item.Details); err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

func (s *Service) run(ctx context.Context, access tenancy.Context, principal postgres.Principal, request postgres.AuthorizationRequest, fn func(context.Context, pgx.Tx) error) error {
	if s == nil || s.Pool == nil {
		return errors.New("admin service requires postgres pool")
	}
	if requiresStepUp(request) && !hasRecentStepUp(principal, s.now()) {
		if denied, buildErr := postgres.NewDeniedAttempt(uuid.New(), principal, request, request.PermissionCode, "admin-api", postgres.DeniedReasonCode("step_up_required"), nil, nil, []byte(`{"source":"admin-api","reason":"step_up_required"}`)); buildErr == nil {
			if auditErr := postgres.RecordDeniedAttempt(ctx, s.Pool, denied); auditErr != nil {
				return fmt.Errorf("step-up denied and denial audit failed: %w", auditErr)
			}
		}
		return fmt.Errorf("%w: step_up_required", ErrForbidden)
	}
	decision, err := postgres.WithAuthorizedTenantTx(ctx, s.Pool, access, request, fn)
	if err != nil {
		return mapDatabaseError(err)
	}
	if !decision.Allowed {
		if denied, buildErr := postgres.NewDeniedAttempt(
			uuid.New(), principal, request, request.PermissionCode, "admin_api",
			postgres.DeniedReasonCode(decision.ReasonCode), nil, nil,
			[]byte(`{"source":"admin-api"}`),
		); buildErr == nil {
			// The authorization transaction has already rolled back before this
			// separate SECURITY DEFINER write, so a denial is not lost and a
			// pool with a single connection cannot deadlock here.
			if auditErr := postgres.RecordDeniedAttempt(ctx, s.Pool, denied); auditErr != nil {
				return fmt.Errorf("authorization denied and denial audit failed: %w", auditErr)
			}
		} else {
			return fmt.Errorf("authorization denied and denial event invalid: %w", buildErr)
		}
		return fmt.Errorf("%w: %s", ErrForbidden, decision.ReasonCode)
	}
	return nil
}

func requiresStepUp(request postgres.AuthorizationRequest) bool {
	return request.ScopeType == "PLATFORM" || request.PermissionCode == "platform.role_assignment.manage" ||
		request.PermissionCode == "authenticator.manage" || request.PermissionCode == "device.enrollment.revoke"
}

func hasRecentStepUp(principal postgres.Principal, now time.Time) bool {
	return principal.AuthenticationLevel == "WEBAUTHN_STEP_UP" && principal.StepUpExpiresAt != nil && now.Before(*principal.StepUpExpiresAt)
}

func (s *Service) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func assignmentScope(input RoleAssignmentInput) (*uuid.UUID, *uuid.UUID, tenancy.Context, string, string) {
	scope := input.ScopeType
	if scope == "PLATFORM" {
		return nil, nil, tenancy.Platform(), scope, "platform.role_assignment.manage"
	}
	if scope == "OPERATION" {
		return input.CompanyID, input.CompanyOperationID, tenancy.Tenant(derefUUID(input.CompanyID)), scope, "role.assignment.manage"
	}
	return input.CompanyID, nil, tenancy.Tenant(derefUUID(input.CompanyID)), scope, "role.assignment.manage"
}

func validateAssignmentScope(scope string, companyID, operationID *uuid.UUID) error {
	switch scope {
	case "PLATFORM":
		if companyID != nil || operationID != nil {
			return ErrInvalidInput
		}
	case "COMPANY":
		if companyID == nil || *companyID == uuid.Nil || operationID != nil {
			return ErrInvalidInput
		}
	case "OPERATION":
		if companyID == nil || *companyID == uuid.Nil || operationID == nil || *operationID == uuid.Nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// Delegation is intentionally narrower than ordinary permission evaluation.
// A caller may have a management permission and still be unable to grant a
// privileged role or permission unless this explicit policy allows it.
func ensureRoleDelegation(ctx context.Context, tx pgx.Tx, principal postgres.Principal, input RoleAssignmentInput) error {
	switch input.ScopeType {
	case "PLATFORM":
		// GLOBAL_ADMIN is provisioned outside this API. Only read-only platform
		// AUDITOR delegation is exposed here.
		if input.RoleCode != "AUDITOR" || !actorHasRole(ctx, tx, principal.PersonID, "GLOBAL_ADMIN", "PLATFORM", nil, nil) {
			return ErrForbidden
		}
	case "COMPANY":
		if !isCompanyRoleDelegable(input.RoleCode) || !actorHasRole(ctx, tx, principal.PersonID, "COMPANY_ADMIN", "COMPANY", input.CompanyID, nil) {
			return ErrForbidden
		}
	case "OPERATION":
		// No operation-level delegator is defined in the current catalog.
		return ErrForbidden
	default:
		return ErrInvalidInput
	}
	return nil
}

func ensurePermissionDelegation(ctx context.Context, tx pgx.Tx, principal postgres.Principal, input PermissionGrantInput) error {
	if input.ScopeType != "COMPANY" || !isCompanyPermissionDelegable(input.PermissionCode) {
		return ErrForbidden
	}
	if !actorHasRole(ctx, tx, principal.PersonID, "COMPANY_ADMIN", "COMPANY", input.CompanyID, nil) {
		return ErrForbidden
	}
	return nil
}

func isCompanyRoleDelegable(roleCode string) bool {
	switch roleCode {
	case "ADMIN_OPERATOR", "MANAGEMENT", "AUDITOR":
		return true
	default:
		return false
	}
}

func isCompanyPermissionDelegable(permissionCode string) bool {
	switch permissionCode {
	case "company.read", "company.operation.read", "person.read", "membership.read", "operation.membership.read", "audit.read":
		return true
	default:
		return false
	}
}

func actorHasRole(ctx context.Context, tx pgx.Tx, personID uuid.UUID, roleCode, scope string, companyID, operationID *uuid.UUID) bool {
	var found bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM platform.role_assignment
			WHERE person_id = $1
			  AND role_code = $2
			  AND scope_type = $3
			  AND company_id IS NOT DISTINCT FROM $4::uuid
			  AND company_operation_id IS NOT DISTINCT FROM $5::uuid
			  AND lifecycle_status = 'ACTIVE'
			  AND valid_from <= now()
			  AND (valid_until IS NULL OR valid_until > now())
		)
	`, personID, roleCode, scope, uuidValue(companyID), uuidValue(operationID)).Scan(&found)
	return err == nil && found
}

func ensureTargetScope(ctx context.Context, tx pgx.Tx, personID uuid.UUID, scope string, companyID, operationID *uuid.UUID) error {
	if personID == uuid.Nil {
		return ErrInvalidInput
	}
	var personActive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.person WHERE id = $1 AND status = 'ACTIVE')`, personID).Scan(&personActive); err != nil {
		return err
	}
	if !personActive {
		return ErrNotFound
	}
	if scope == "PLATFORM" {
		return nil
	}
	var companyMember bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM platform.company_membership
			WHERE company_id = $1 AND person_id = $2 AND status = 'ACTIVE'
			  AND valid_from <= now() AND (valid_until IS NULL OR valid_until > now())
		)
	`, *companyID, personID).Scan(&companyMember); err != nil {
		return err
	}
	if !companyMember {
		return ErrConflict
	}
	if scope == "OPERATION" {
		var operationMember bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM platform.operation_membership
				WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3
				  AND status = 'ACTIVE' AND valid_from <= now()
				  AND (valid_until IS NULL OR valid_until > now())
			)
		`, *companyID, *operationID, personID).Scan(&operationMember); err != nil {
			return err
		}
		if !operationMember {
			return ErrConflict
		}
	}
	return nil
}

func permissionScope(input PermissionGrantInput) (*uuid.UUID, *uuid.UUID, tenancy.Context, string, string) {
	if input.ScopeType == "OPERATION" {
		return input.CompanyID, input.CompanyOperationID, tenancy.Tenant(derefUUID(input.CompanyID)), input.ScopeType, "permission.grant.manage"
	}
	return input.CompanyID, nil, tenancy.Tenant(derefUUID(input.CompanyID)), input.ScopeType, "permission.grant.manage"
}

func writeAdminAudit(ctx context.Context, tx pgx.Tx, principal postgres.Principal, companyID, operationID *uuid.UUID, action, resourceType, resourceID string) error {
	return postgres.WriteAuditEvent(ctx, tx, postgres.AuditEvent{
		ID: uuid.New(), ActorUserAccountID: ptrUUID(principal.UserAccountID), CompanyID: companyID, CompanyOperationID: operationID,
		Action: action, ResourceType: resourceType, ResourceID: &resourceID, Details: []byte(`{"source":"admin-api"}`),
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505": // unique_violation
		return ErrConflict
	case "23503", "23514", "23502": // FK, CHECK, NOT NULL
		return ErrInvalidInput
	case "42501": // insufficient_privilege
		return ErrForbidden
	default:
		return err
	}
}

func ptrUUID(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	return &value
}

func derefUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

func uuidValue(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}
