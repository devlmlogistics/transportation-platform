package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"transportation-platform/internal/application/admin"
	"transportation-platform/internal/platform/postgres"
)

// PrincipalResolver is the explicit authentication boundary. Production must
// provide a real implementation in a later phase; this package never derives
// identity from HTTP headers.
type PrincipalResolver interface {
	Resolve(context.Context, *http.Request) (postgres.Principal, error)
}

type AdminService interface {
	GetCompany(context.Context, postgres.Principal, uuid.UUID) (postgres.CompanyRecord, error)
	CreateCompany(context.Context, postgres.Principal, admin.CompanyInput) (postgres.CompanyRecord, error)
	UpdateCompany(context.Context, postgres.Principal, uuid.UUID, admin.CompanyUpdateInput) error
	ListOperations(context.Context, postgres.Principal, uuid.UUID) ([]admin.OperationView, error)
	EnableOperation(context.Context, postgres.Principal, uuid.UUID, string) (admin.OperationView, error)
	SetOperationStatus(context.Context, postgres.Principal, uuid.UUID, uuid.UUID, string) error
	ListMemberships(context.Context, postgres.Principal, uuid.UUID) ([]admin.MembershipView, error)
	SetMembershipStatus(context.Context, postgres.Principal, uuid.UUID, uuid.UUID, string) error
	SetOperationMembership(context.Context, postgres.Principal, uuid.UUID, uuid.UUID, uuid.UUID, string) error
	AssignRole(context.Context, postgres.Principal, admin.RoleAssignmentInput) error
	RevokeRole(context.Context, postgres.Principal, uuid.UUID, uuid.UUID) error
	GrantPermission(context.Context, postgres.Principal, admin.PermissionGrantInput) error
	RevokePermission(context.Context, postgres.Principal, uuid.UUID, uuid.UUID) error
	ListAudit(context.Context, postgres.Principal, uuid.UUID) ([]admin.AuditView, error)
}

type AdminHandler struct {
	Service  AdminService
	Resolver PrincipalResolver
}

func NewAdminHandler(service AdminService, resolver PrincipalResolver) (*AdminHandler, error) {
	if service == nil || resolver == nil {
		return nil, errors.New("admin handler requires service and principal resolver")
	}
	return &AdminHandler{Service: service, Resolver: resolver}, nil
}

// ClosedAdminHandler is the production default until a real authentication
// provider is configured. It intentionally ignores every identity-like header.
func ClosedAdminHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Administrative authentication is not configured.")
	})
}

func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, err := h.Resolver.Resolve(r.Context(), r)
	if err != nil || principal.UserAccountID == uuid.Nil || principal.PersonID == uuid.Nil || !principal.Active {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	if principal.SessionPolicy == "DRIVER" || principal.SessionPolicy == "DRIVER_ACTIVATION" {
		writeError(w, r, http.StatusForbidden, "administrative_session_required", "An administrative session is required.")
		return
	}
	parts := splitAdminPath(r.URL.Path)
	if len(parts) == 0 {
		writeError(w, r, http.StatusNotFound, "not_found", "Administrative endpoint not found.")
		return
	}
	if isMutationMethod(r.Method) && r.Header.Get("Idempotency-Key") != "" {
		// No durable idempotency store exists in this phase. Rejecting the
		// header prevents callers from assuming replay safety we cannot provide.
		writeError(w, r, http.StatusUnprocessableEntity, "idempotency_not_supported", "Idempotency-Key is not supported by this administrative contract yet.")
		return
	}

	var serviceErr error
	switch {
	case len(parts) == 1 && parts[0] == "companies" && r.Method == http.MethodPost:
		var input admin.CompanyInput
		if err := decodeJSON(w, r, &input); err != nil {
			return
		}
		var result postgres.CompanyRecord
		result, serviceErr = h.Service.CreateCompany(r.Context(), principal, input)
		if serviceErr == nil {
			writeJSON(w, http.StatusCreated, result)
			return
		}
	case len(parts) == 2 && parts[0] == "companies":
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		switch r.Method {
		case http.MethodGet:
			var result postgres.CompanyRecord
			result, serviceErr = h.Service.GetCompany(r.Context(), principal, companyID)
			if serviceErr == nil {
				writeJSON(w, http.StatusOK, result)
				return
			}
		case http.MethodPatch:
			var input admin.CompanyUpdateInput
			if err := decodeJSON(w, r, &input); err != nil {
				return
			}
			serviceErr = h.Service.UpdateCompany(r.Context(), principal, companyID, input)
			if serviceErr == nil {
				writeNoContent(w)
				return
			}
		}
	case len(parts) == 3 && parts[0] == "companies" && parts[2] == "operations" && r.Method == http.MethodGet:
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		var result []admin.OperationView
		result, serviceErr = h.Service.ListOperations(r.Context(), principal, companyID)
		if serviceErr == nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
	case len(parts) == 3 && parts[0] == "companies" && parts[2] == "operations" && r.Method == http.MethodPost:
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		var input admin.OperationInput
		if err := decodeJSON(w, r, &input); err != nil {
			return
		}
		var result admin.OperationView
		result, serviceErr = h.Service.EnableOperation(r.Context(), principal, companyID, input.OperationType)
		if serviceErr == nil {
			writeJSON(w, http.StatusCreated, result)
			return
		}
	case len(parts) == 4 && parts[0] == "companies" && parts[2] == "operations" && r.Method == http.MethodPatch:
		companyID, operationID, ok := parseTwoUUIDs(w, r, parts[1], parts[3])
		if !ok {
			return
		}
		var input struct {
			Status string `json:"status"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			return
		}
		serviceErr = h.Service.SetOperationStatus(r.Context(), principal, companyID, operationID, input.Status)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 3 && parts[0] == "companies" && parts[2] == "memberships" && r.Method == http.MethodGet:
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		var result []admin.MembershipView
		result, serviceErr = h.Service.ListMemberships(r.Context(), principal, companyID)
		if serviceErr == nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
	case len(parts) == 4 && parts[0] == "companies" && parts[2] == "memberships" && r.Method == http.MethodPatch:
		companyID, membershipID, ok := parseTwoUUIDs(w, r, parts[1], parts[3])
		if !ok {
			return
		}
		var input struct {
			Status string `json:"status"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			return
		}
		serviceErr = h.Service.SetMembershipStatus(r.Context(), principal, companyID, membershipID, input.Status)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 6 && parts[0] == "companies" && parts[2] == "operations" && parts[4] == "members" && (r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete):
		companyID, operationID, ok := parseTwoUUIDs(w, r, parts[1], parts[3])
		if !ok {
			return
		}
		personID, err := uuid.Parse(parts[5])
		if err != nil {
			writeError(w, r, http.StatusUnprocessableEntity, "invalid_uuid", "Invalid person identifier.")
			return
		}
		status := "REVOKED"
		if r.Method != http.MethodDelete {
			var input struct {
				Status string `json:"status"`
			}
			if err := decodeJSON(w, r, &input); err != nil {
				return
			}
			status = input.Status
		}
		serviceErr = h.Service.SetOperationMembership(r.Context(), principal, companyID, operationID, personID, status)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 3 && parts[0] == "companies" && parts[2] == "role-assignments" && r.Method == http.MethodPost:
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		var input admin.RoleAssignmentInput
		if err := decodeJSON(w, r, &input); err != nil {
			return
		}
		if !validateCompanyScopedAssignment(w, r, companyID, input.ScopeType, input.CompanyID, input.CompanyOperationID) {
			return
		}
		input.CompanyID = &companyID
		serviceErr = h.Service.AssignRole(r.Context(), principal, input)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 4 && parts[0] == "companies" && parts[2] == "role-assignments" && r.Method == http.MethodDelete:
		companyID, assignmentID, ok := parseTwoUUIDs(w, r, parts[1], parts[3])
		if !ok {
			return
		}
		serviceErr = h.Service.RevokeRole(r.Context(), principal, companyID, assignmentID)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 3 && parts[0] == "companies" && parts[2] == "permission-grants" && r.Method == http.MethodPost:
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		var input admin.PermissionGrantInput
		if err := decodeJSON(w, r, &input); err != nil {
			return
		}
		if !validateCompanyScopedAssignment(w, r, companyID, input.ScopeType, input.CompanyID, input.CompanyOperationID) {
			return
		}
		input.CompanyID = &companyID
		serviceErr = h.Service.GrantPermission(r.Context(), principal, input)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 4 && parts[0] == "companies" && parts[2] == "permission-grants" && r.Method == http.MethodDelete:
		companyID, grantID, ok := parseTwoUUIDs(w, r, parts[1], parts[3])
		if !ok {
			return
		}
		serviceErr = h.Service.RevokePermission(r.Context(), principal, companyID, grantID)
		if serviceErr == nil {
			writeNoContent(w)
			return
		}
	case len(parts) == 3 && parts[0] == "companies" && parts[2] == "audit-events" && r.Method == http.MethodGet:
		companyID, ok := parseUUIDOrError(w, r, parts[1])
		if !ok {
			return
		}
		var result []admin.AuditView
		result, serviceErr = h.Service.ListAudit(r.Context(), principal, companyID)
		if serviceErr == nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "Administrative endpoint not found.")
		return
	}
	writeAdminServiceError(w, r, serviceErr)
}

func splitAdminPath(path string) []string {
	path = strings.TrimPrefix(path, "/api/v1/admin/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func isMutationMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func validateCompanyScopedAssignment(w http.ResponseWriter, r *http.Request, companyID uuid.UUID, scope string, payloadCompanyID, operationID *uuid.UUID) bool {
	if strings.EqualFold(strings.TrimSpace(scope), "PLATFORM") {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_scope", "Platform assignments are not accepted on company endpoints.")
		return false
	}
	if payloadCompanyID != nil && *payloadCompanyID != companyID {
		writeError(w, r, http.StatusUnprocessableEntity, "company_scope_mismatch", "Payload company_id must match the URL company.")
		return false
	}
	if strings.EqualFold(strings.TrimSpace(scope), "COMPANY") && operationID != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_scope", "Company assignments cannot include company_operation_id.")
		return false
	}
	if strings.EqualFold(strings.TrimSpace(scope), "OPERATION") && operationID == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_scope", "Operation assignments require company_operation_id.")
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_json", "Invalid request body.")
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_json", "Request body must contain one JSON object.")
		return errors.New("multiple JSON values")
	}
	return nil
}

func parseUUIDOrError(w http.ResponseWriter, r *http.Request, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_uuid", "Invalid identifier.")
		return uuid.Nil, false
	}
	return id, true
}

func parseTwoUUIDs(w http.ResponseWriter, r *http.Request, left, right string) (uuid.UUID, uuid.UUID, bool) {
	leftID, ok := parseUUIDOrError(w, r, left)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	rightID, ok := parseUUIDOrError(w, r, right)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return leftID, rightID, true
}

func writeAdminServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, admin.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "permission_denied", "You are not authorized for this resource.")
	case errors.Is(err, admin.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(err, admin.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "Resource state conflicts with the requested change.")
	case errors.Is(err, admin.ErrInvalidInput):
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "Invalid administrative input.")
	case errors.Is(err, admin.ErrIdentityAttachRequiresFlow):
		writeError(w, r, http.StatusForbidden, "identity_flow_required", "The approved identity flow is required.")
	case errors.Is(err, admin.ErrPlatformMutationNotSupported):
		writeError(w, r, http.StatusForbidden, "platform_mutation_not_supported", "This platform mutation is not exposed by the administrative API.")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error.")
	}
}

func writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
