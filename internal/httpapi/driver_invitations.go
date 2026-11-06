package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
)

type DriverInvitationHTTPService interface {
	Create(context.Context, []byte, applicationauth.AuthenticatedSession, applicationauth.DriverEnrollmentInvitationInput) (applicationauth.IssuedDriverEnrollmentInvitation, error)
	List(context.Context, []byte, applicationauth.AuthenticatedSession) ([]applicationauth.DriverEnrollmentInvitation, error)
	Candidates(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, *uuid.UUID) ([]applicationauth.DriverEnrollmentCandidate, error)
	Revoke(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, string) error
	Resolve(context.Context, []byte) (applicationauth.DriverEnrollmentInvitationPreview, error)
	Consume(context.Context, applicationauth.DriverEnrollmentConsumeInput) (applicationauth.DriverEnrollmentActivation, error)
}

type DriverInvitationHandler struct {
	Service  DriverInvitationHTTPService
	Security *AuthHandler
	Public   bool
}

func NewDriverInvitationHandler(service DriverInvitationHTTPService, security *AuthHandler, public bool) (*DriverInvitationHandler, error) {
	if service == nil || security == nil {
		return nil, errors.New("driver invitation handler requires service and security")
	}
	return &DriverInvitationHandler{Service: service, Security: security, Public: public}, nil
}

func (h *DriverInvitationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Public {
		h.servePublic(w, r)
		return
	}
	h.serveSupervisor(w, r)
}

func (h *DriverInvitationHandler) serveSupervisor(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/webauthn/driver-invitations")
	switch {
	case path == "" || path == "/":
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
	case path == "/candidates" && r.Method == http.MethodGet:
		h.candidates(w, r)
	case strings.HasPrefix(path, "/") && strings.HasSuffix(path, "/revoke") && r.Method == http.MethodPost:
		h.revoke(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "Driver invitation endpoint not found.")
	}
}

func (h *DriverInvitationHandler) servePublic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/driver/invitations")
	if r.Method != http.MethodPost || (path != "/validate" && path != "/consume") {
		writeError(w, r, http.StatusNotFound, "not_found", "Driver invitation endpoint not found.")
		return
	}
	if !h.Security.checkOrigin(w, r) {
		return
	}
	if path == "/validate" {
		h.validate(w, r)
		return
	}
	h.consume(w, r)
}

func (h *DriverInvitationHandler) create(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	var input struct {
		PersonID           string `json:"person_id"`
		CompanyID          string `json:"company_id"`
		CompanyOperationID string `json:"company_operation_id"`
		ExpiresSeconds     int    `json:"expires_seconds"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	personID, err := uuid.Parse(input.PersonID)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid driver is required.")
		return
	}
	companyID, err := uuid.Parse(input.CompanyID)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid company is required.")
		return
	}
	var operationID *uuid.UUID
	if strings.TrimSpace(input.CompanyOperationID) != "" {
		parsed, parseErr := uuid.Parse(input.CompanyOperationID)
		if parseErr != nil {
			writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid operation is required.")
			return
		}
		operationID = &parsed
	}
	issued, err := h.Service.Create(r.Context(), token, session, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: personID, CompanyID: companyID, CompanyOperationID: operationID, ExpiresSeconds: input.ExpiresSeconds,
	})
	if err != nil {
		if errors.Is(err, applicationauth.ErrDriverInvitationConflict) {
			writeError(w, r, http.StatusConflict, "invitation_exists", "An active invitation already exists.")
			return
		}
		writeError(w, r, http.StatusForbidden, "invitation_not_created", "The driver invitation could not be created.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"invitation_id":     issued.Invitation.ID,
		"expires_at":        issued.Invitation.ExpiresAt,
		"token":             base64.RawURLEncoding.EncodeToString(issued.Token),
		"confirmation_code": issued.Code,
	})
}

func (h *DriverInvitationHandler) list(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.session(w, r)
	if !ok {
		return
	}
	items, err := h.Service.List(r.Context(), token, session)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "invitations_unavailable", "Driver invitations are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *DriverInvitationHandler) candidates(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.session(w, r)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(r.URL.Query().Get("company_id"))
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid company is required.")
		return
	}
	var operationID *uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("company_operation_id")); raw != "" {
		parsed, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid operation is required.")
			return
		}
		operationID = &parsed
	}
	items, err := h.Service.Candidates(r.Context(), token, session, companyID, operationID)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "candidates_unavailable", "Driver candidates are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *DriverInvitationHandler) revoke(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid invitation is required.")
		return
	}
	id, err := uuid.Parse(parts[len(parts)-2])
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", "A valid invitation is required.")
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	if err := h.Service.Revoke(r.Context(), token, session, id, input.Reason); err != nil {
		writeError(w, r, http.StatusConflict, "invitation_not_revoked", "The driver invitation was not revoked.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "REVOKED"})
}

func (h *DriverInvitationHandler) validate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	token, err := decodeInvitationToken(input.Token)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	preview, err := h.Service.Resolve(r.Context(), token)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"invitation_id": preview.ID, "identifier": preview.Identifier, "legal_name": preview.LegalName,
		"company_id": preview.CompanyID, "company_name": preview.CompanyName,
		"company_operation_id": preview.CompanyOperationID, "operation_name": preview.OperationName,
		"expires_at": preview.ExpiresAt,
	})
}

func (h *DriverInvitationHandler) consume(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token                 string `json:"token"`
		ConfirmationCode      string `json:"confirmation_code"`
		InstallationID        string `json:"installation_id"`
		InstallationPublicKey string `json:"installation_public_key"`
		KeyAlgorithm          string `json:"key_algorithm"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	token, err := decodeInvitationToken(input.Token)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	installationID, err := uuid.Parse(input.InstallationID)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(input.InstallationPublicKey)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	activation, err := h.Service.Consume(r.Context(), applicationauth.DriverEnrollmentConsumeInput{
		Token: token, ConfirmationCode: input.ConfirmationCode, InstallationID: installationID,
		InstallationPublicKey: publicKey, KeyAlgorithm: input.KeyAlgorithm,
		UserAgent: r.UserAgent(), RemoteAddr: r.RemoteAddr,
	})
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	h.Security.setCookies(w, activation.Token, activation.CSRFToken)
	writeJSON(w, http.StatusCreated, map[string]any{
		"request_id":           activation.Request.ID,
		"correlation_code":     activation.Request.CorrelationCode,
		"company_id":           activation.Request.CompanyID,
		"company_operation_id": activation.Request.CompanyOperationID,
		"installation_id":      activation.Request.InstallationID,
		"status":               activation.Request.Status,
		"expires_at":           activation.Request.ExpiresAt,
	})
}

func decodeInvitationToken(raw string) ([]byte, error) {
	token, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(token) != 32 {
		return nil, errors.New("invalid invitation token")
	}
	return token, nil
}
