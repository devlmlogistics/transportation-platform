package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CompanyRecord struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	LegalName   string    `json:"legal_name"`
	DisplayName *string   `json:"display_name,omitempty"`
	Status      string    `json:"status"`
}

type CompanyRepository struct{}

func (CompanyRepository) Create(ctx context.Context, tx pgx.Tx, company CompanyRecord) error {
	if company.ID == uuid.Nil || strings.TrimSpace(company.Slug) == "" || strings.TrimSpace(company.LegalName) == "" {
		return errors.New("company id, slug and legal name are required")
	}
	status := company.Status
	if status == "" {
		status = "ACTIVE"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.company (id, slug, legal_name, display_name, status)
		VALUES ($1, $2, $3, $4, $5)
	`, company.ID, strings.ToLower(strings.TrimSpace(company.Slug)), strings.TrimSpace(company.LegalName), company.DisplayName, status)
	return err
}

func (CompanyRepository) Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (CompanyRecord, error) {
	var company CompanyRecord
	err := tx.QueryRow(ctx, `
		SELECT id, slug, legal_name, display_name, status
		FROM platform.company
		WHERE id = $1
	`, id).Scan(&company.ID, &company.Slug, &company.LegalName, &company.DisplayName, &company.Status)
	return company, err
}

type Principal struct {
	UserAccountID       uuid.UUID
	PersonID            uuid.UUID
	Active              bool
	SessionID           uuid.UUID
	SessionEpoch        int64
	ContextVersion      int64
	ActiveCompanyID     *uuid.UUID
	ActiveOperationID   *uuid.UUID
	CSRFSecretHash      []byte
	AuthenticationLevel string
	StepUpExpiresAt     *time.Time
}

type AuthorizationRequest struct {
	Principal          Principal
	ScopeType          string
	CompanyID          *uuid.UUID
	CompanyOperationID *uuid.UUID
	PermissionCode     string
	EvaluationTime     time.Time
}

type AuthorizationDecision struct {
	Allowed    bool
	ReasonCode string
}

type AuthorizationRepository struct{}

func (AuthorizationRepository) Authorize(ctx context.Context, tx pgx.Tx, request AuthorizationRequest) (AuthorizationDecision, error) {
	if request.Principal.PersonID == uuid.Nil || !request.Principal.Active {
		return AuthorizationDecision{ReasonCode: "principal_inactive"}, nil
	}
	if request.PermissionCode == "" {
		return AuthorizationDecision{ReasonCode: "permission_missing"}, nil
	}
	if err := validateScope(request.ScopeType, request.CompanyID, request.CompanyOperationID); err != nil {
		return AuthorizationDecision{ReasonCode: "scope_invalid"}, err
	}
	if request.Principal.UserAccountID == uuid.Nil {
		return AuthorizationDecision{ReasonCode: "account_missing"}, nil
	}
	evaluationTime := request.EvaluationTime
	if evaluationTime.IsZero() {
		evaluationTime = time.Now().UTC()
	}

	var accountActive bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM platform.user_account ua
			JOIN platform.person p ON p.id = ua.person_id
			WHERE ua.id = $1
			  AND ua.person_id = $2
			  AND ua.status = 'ACTIVE'
			  AND p.status = 'ACTIVE'
		)
	`, request.Principal.UserAccountID, request.Principal.PersonID).Scan(&accountActive)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	if !accountActive {
		return AuthorizationDecision{ReasonCode: "account_inactive"}, nil
	}

	if request.ScopeType != "PLATFORM" {
		if request.CompanyID == nil {
			return AuthorizationDecision{ReasonCode: "company_missing"}, nil
		}
		var companyMembership bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM platform.company_membership cm
				JOIN platform.company c ON c.id = cm.company_id
				WHERE cm.company_id = $1
				  AND cm.person_id = $2
				  AND cm.status = 'ACTIVE'
				  AND cm.valid_from <= $3
				  AND (cm.valid_until IS NULL OR cm.valid_until > $3)
				  AND c.status = 'ACTIVE'
			)
		`, *request.CompanyID, request.Principal.PersonID, evaluationTime).Scan(&companyMembership)
		if err != nil {
			return AuthorizationDecision{}, err
		}
		if !companyMembership {
			return AuthorizationDecision{ReasonCode: "company_membership_missing"}, nil
		}
	}

	if request.ScopeType == "OPERATION" {
		if request.CompanyOperationID == nil {
			return AuthorizationDecision{ReasonCode: "operation_missing"}, nil
		}
		var operationMembership bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM platform.operation_membership om
				JOIN platform.company_operation co
				  ON co.company_id = om.company_id AND co.id = om.company_operation_id
				WHERE om.company_id = $1
				  AND om.company_operation_id = $2
				  AND om.person_id = $3
				  AND om.status = 'ACTIVE'
				  AND om.valid_from <= $4
				  AND (om.valid_until IS NULL OR om.valid_until > $4)
				  AND co.status = 'ACTIVE'
			)
		`, *request.CompanyID, *request.CompanyOperationID, request.Principal.PersonID, evaluationTime).Scan(&operationMembership)
		if err != nil {
			return AuthorizationDecision{}, err
		}
		if !operationMembership {
			return AuthorizationDecision{ReasonCode: "operation_membership_missing"}, nil
		}
	}

	companyArg, operationArg := uuidArgument(request.CompanyID), uuidArgument(request.CompanyOperationID)
	var granted bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM platform.role_assignment ra
			JOIN platform.role_permission rp ON rp.role_code = ra.role_code
			JOIN platform.permission_scope ps
			  ON ps.permission_code = rp.permission_code
			 AND ps.scope_type = ra.scope_type
			WHERE ra.person_id = $1
			  AND ra.scope_type = $2
			  AND ra.company_id IS NOT DISTINCT FROM $3::uuid
			  AND ra.company_operation_id IS NOT DISTINCT FROM $4::uuid
			  AND rp.permission_code = $5
			  AND ra.lifecycle_status = 'ACTIVE'
			  AND ra.valid_from <= $6
			  AND (ra.valid_until IS NULL OR ra.valid_until > $6)
		)
		OR EXISTS (
			SELECT 1
			FROM platform.permission_grant pg
			JOIN platform.permission_scope ps
			  ON ps.permission_code = pg.permission_code
			 AND ps.scope_type = pg.scope_type
			WHERE pg.person_id = $1
			  AND pg.scope_type = $2
			  AND pg.company_id IS NOT DISTINCT FROM $3::uuid
			  AND pg.company_operation_id IS NOT DISTINCT FROM $4::uuid
			  AND pg.permission_code = $5
			  AND pg.lifecycle_status = 'ACTIVE'
			  AND pg.valid_from <= $6
			  AND (pg.valid_until IS NULL OR pg.valid_until > $6)
		)
	`, request.Principal.PersonID, request.ScopeType, companyArg, operationArg, request.PermissionCode, evaluationTime).Scan(&granted)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	if !granted {
		return AuthorizationDecision{ReasonCode: "permission_denied"}, nil
	}
	return AuthorizationDecision{Allowed: true, ReasonCode: "permission_granted"}, nil
}

func validateScope(scope string, companyID, operationID *uuid.UUID) error {
	switch scope {
	case "PLATFORM":
		if companyID != nil || operationID != nil {
			return errors.New("platform scope cannot contain tenant ids")
		}
	case "COMPANY":
		if companyID == nil || operationID != nil {
			return errors.New("company scope requires only company id")
		}
	case "OPERATION":
		if companyID == nil || operationID == nil {
			return errors.New("operation scope requires company and operation ids")
		}
	default:
		return errors.New("unsupported authorization scope")
	}
	return nil
}

func uuidArgument(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

type AuditEvent struct {
	ID                 uuid.UUID
	ActorUserAccountID *uuid.UUID
	CompanyID          *uuid.UUID
	CompanyOperationID *uuid.UUID
	Action             string
	ResourceType       string
	ResourceID         *string
	RequestID          *string
	RemoteIP           *string
	Details            []byte
}

func WriteAuditEvent(ctx context.Context, tx pgx.Tx, event AuditEvent) error {
	if event.ID == uuid.Nil || strings.TrimSpace(event.Action) == "" || strings.TrimSpace(event.ResourceType) == "" {
		return errors.New("audit event id, action and resource type are required")
	}
	if len(event.Details) == 0 {
		event.Details = []byte(`{}`)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.admin_audit_event (
			id, actor_user_account_id, company_id, company_operation_id,
			action, resource_type, resource_id, request_id, remote_ip, details
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::inet, $10::jsonb)
	`, event.ID, event.ActorUserAccountID, event.CompanyID, event.CompanyOperationID,
		event.Action, event.ResourceType, event.ResourceID, event.RequestID, stringValue(event.RemoteIP), event.Details)
	return err
}

type DeniedReasonCode string

const (
	DeniedReasonPrincipalInactive          DeniedReasonCode = "principal_inactive"
	DeniedReasonAccountMissing             DeniedReasonCode = "account_missing"
	DeniedReasonAccountInactive            DeniedReasonCode = "account_inactive"
	DeniedReasonCompanyMissing             DeniedReasonCode = "company_missing"
	DeniedReasonCompanyMembershipMissing   DeniedReasonCode = "company_membership_missing"
	DeniedReasonOperationMissing           DeniedReasonCode = "operation_missing"
	DeniedReasonOperationMembershipMissing DeniedReasonCode = "operation_membership_missing"
	DeniedReasonScopeInvalid               DeniedReasonCode = "scope_invalid"
	DeniedReasonPermissionMissing          DeniedReasonCode = "permission_missing"
	DeniedReasonPermissionDenied           DeniedReasonCode = "permission_denied"
	DeniedReasonResourceDenied             DeniedReasonCode = "resource_denied"
)

// DeniedAttempt can only be built through NewDeniedAttempt. Its fields are
// intentionally private so handlers cannot manufacture actor/scope data from
// an arbitrary HTTP payload and pass it to the security writer.
type DeniedAttempt struct {
	id                   uuid.UUID
	actorUserAccountID   *uuid.UUID
	requestedCompanyID   *uuid.UUID
	requestedOperationID *uuid.UUID
	action               string
	resourceType         string
	reasonCode           DeniedReasonCode
	requestID            *string
	remoteIP             *string
	details              []byte
}

// NewDeniedAttempt derives actor and requested scope from the server-side
// principal and authorization request. Action/resource/reason must come from
// a server route and authorization result, never directly from HTTP JSON.
func NewDeniedAttempt(
	id uuid.UUID,
	principal Principal,
	request AuthorizationRequest,
	action string,
	resourceType string,
	reason DeniedReasonCode,
	requestID *string,
	remoteIP *string,
	details []byte,
) (DeniedAttempt, error) {
	if id == uuid.Nil {
		return DeniedAttempt{}, errors.New("denied attempt id is required")
	}
	if strings.TrimSpace(action) == "" || strings.TrimSpace(resourceType) == "" {
		return DeniedAttempt{}, errors.New("denied attempt action and resource are required")
	}
	if !validDeniedReason(reason) {
		return DeniedAttempt{}, fmt.Errorf("unsupported denied reason %q", reason)
	}
	if reason != DeniedReasonScopeInvalid {
		if err := validateScope(request.ScopeType, request.CompanyID, request.CompanyOperationID); err != nil {
			return DeniedAttempt{}, err
		}
	}
	if details == nil {
		details = []byte(`{}`)
	}
	var object map[string]any
	if err := json.Unmarshal(details, &object); err != nil || object == nil {
		return DeniedAttempt{}, errors.New("denied attempt details must be a JSON object")
	}

	var actorID *uuid.UUID
	if principal.UserAccountID != uuid.Nil {
		actor := principal.UserAccountID
		actorID = &actor
	}
	requestedCompanyID := request.CompanyID
	requestedOperationID := request.CompanyOperationID
	if requestedCompanyID == nil {
		requestedOperationID = nil
	}
	return DeniedAttempt{
		id:                   id,
		actorUserAccountID:   actorID,
		requestedCompanyID:   requestedCompanyID,
		requestedOperationID: requestedOperationID,
		action:               strings.TrimSpace(action),
		resourceType:         strings.TrimSpace(resourceType),
		reasonCode:           reason,
		requestID:            requestID,
		remoteIP:             remoteIP,
		details:              details,
	}, nil
}

func validDeniedReason(reason DeniedReasonCode) bool {
	switch reason {
	case DeniedReasonPrincipalInactive,
		DeniedReasonAccountMissing,
		DeniedReasonAccountInactive,
		DeniedReasonCompanyMissing,
		DeniedReasonCompanyMembershipMissing,
		DeniedReasonOperationMissing,
		DeniedReasonOperationMembershipMissing,
		DeniedReasonScopeInvalid,
		DeniedReasonPermissionMissing,
		DeniedReasonPermissionDenied,
		DeniedReasonResourceDenied:
		return true
	default:
		return false
	}
}

func RecordDeniedAttempt(ctx context.Context, pool *pgxpool.Pool, event DeniedAttempt) error {
	if pool == nil {
		return errors.New("postgres pool is required")
	}
	if event.id == uuid.Nil {
		return errors.New("denied attempt is not initialized by NewDeniedAttempt")
	}
	_, err := pool.Exec(ctx, `
		SELECT platform.record_denied_attempt(
			$1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::inet, $10::jsonb
		)
	`, event.id, event.actorUserAccountID, event.requestedCompanyID, event.requestedOperationID,
		event.action, event.resourceType, event.reasonCode, event.requestID,
		stringValue(event.remoteIP), event.details)
	return err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r AuthorizationRequest) String() string {
	return fmt.Sprintf("%s/%s", r.ScopeType, r.PermissionCode)
}
