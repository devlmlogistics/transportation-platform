package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
)

type DriverAuthHTTPService interface {
	LoginDriver(context.Context, string, string, string, string) (applicationauth.LoginResult, error)
	BeginLogin(context.Context, string, uuid.UUID) (uuid.UUID, *protocol.CredentialAssertion, error)
	FinishLogin(context.Context, uuid.UUID, *http.Request, []byte, string, string) (applicationauth.LoginResult, error)
	ResolveSession(context.Context, []byte) (applicationauth.AuthenticatedSession, error)
	GetDriverActivationRequestStatus(context.Context, []byte, uuid.UUID, uuid.UUID) (applicationauth.DriverActivationRequestStatus, error)
	Logout(context.Context, []byte, uuid.UUID) error
	ListAuthorizedContexts(context.Context, []byte) ([]applicationauth.DriverAuthorizedContext, error)
}

type DriverAuthHandler struct {
	Service  DriverAuthHTTPService
	Security *AuthHandler
}

func NewDriverAuthHandler(service DriverAuthHTTPService, security *AuthHandler) (*DriverAuthHandler, error) {
	if service == nil || security == nil {
		return nil, errInvalidRouter("driver authentication dependencies are required")
	}
	return &DriverAuthHandler{Service: service, Security: security}, nil
}

func (h *DriverAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/driver/auth")
	switch {
	case path == "/activation/login" && r.Method == http.MethodPost:
		h.activationLogin(w, r)
	case path == "/activation/status" && r.Method == http.MethodGet:
		h.activationStatus(w, r)
	case path == "/webauthn/options" && r.Method == http.MethodPost:
		h.options(w, r)
	case path == "/webauthn/finish" && r.Method == http.MethodPost:
		h.finish(w, r)
	case path == "/me" && r.Method == http.MethodGet:
		h.me(w, r)
	case path == "/contexts" && r.Method == http.MethodGet:
		h.contexts(w, r)
	case path == "/logout" && r.Method == http.MethodPost:
		h.logout(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "Driver authentication endpoint not found.")
	}
}

func (h *DriverAuthHandler) activationStatus(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.session(w, r)
	if !ok {
		return
	}
	if session.SessionPolicy != applicationauth.DriverActivationSession {
		writeError(w, r, http.StatusForbidden, "activation_status_unavailable", "Activation status is unavailable.")
		return
	}
	requestID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("device_request_id")))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid device request is required.")
		return
	}
	installationID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("installation_id")))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid installation is required.")
		return
	}
	status, err := h.Service.GetDriverActivationRequestStatus(r.Context(), token, requestID, installationID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "activation_status_unavailable", "Activation status is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":           status.RequestID,
		"correlation_code":     status.CorrelationCode,
		"company_id":           status.CompanyID,
		"company_operation_id": status.CompanyOperationID,
		"installation_id":      status.InstallationID,
		"status":               status.Status,
		"expires_at":           status.ExpiresAt,
		"authorization_status": status.AuthorizationStatus,
	})
}

func (h *DriverAuthHandler) activationLogin(w http.ResponseWriter, r *http.Request) {
	if !h.Security.checkOrigin(w, r) {
		return
	}
	var input struct{ Identifier, Password string }
	if decodeJSON(w, r, &input) != nil {
		return
	}
	result, err := h.Service.LoginDriver(r.Context(), input.Identifier, input.Password, r.RemoteAddr, r.UserAgent())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_failed", "Invalid credentials.")
		return
	}
	h.Security.setCookiesWithMaxAge(w, result.Token, result.CSRFToken, sessionCookieMaxAge(result.Session.AbsoluteExpiresAt))
	writeJSON(w, http.StatusOK, map[string]any{"session_id": result.Session.ID, "expires_at": result.Session.AbsoluteExpiresAt})
}

func (h *DriverAuthHandler) options(w http.ResponseWriter, r *http.Request) {
	if !h.Security.checkOrigin(w, r) {
		return
	}
	var input struct {
		Identifier     string `json:"identifier"`
		InstallationID string `json:"installation_id"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	installationID, err := uuid.Parse(input.InstallationID)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "driver_auth_rejected", "Driver authentication was rejected.")
		return
	}
	id, options, err := h.Service.BeginLogin(r.Context(), input.Identifier, installationID)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "driver_auth_rejected", "Driver authentication was rejected.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"challenge_id": id, "options": options})
}

func (h *DriverAuthHandler) finish(w http.ResponseWriter, r *http.Request) {
	if !h.Security.checkOrigin(w, r) {
		return
	}
	var envelope struct {
		ChallengeID         string          `json:"challenge_id"`
		Credential          json.RawMessage `json:"credential"`
		PossessionSignature string          `json:"possession_signature"`
	}
	if decodeJSON(w, r, &envelope) != nil {
		return
	}
	challengeID, err := uuid.Parse(envelope.ChallengeID)
	if err != nil || len(envelope.Credential) == 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "driver_auth_rejected", "Driver authentication was rejected.")
		return
	}
	signature, err := base64.RawURLEncoding.DecodeString(envelope.PossessionSignature)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "driver_auth_rejected", "Driver authentication was rejected.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(envelope.Credential))
	r.ContentLength = int64(len(envelope.Credential))
	result, err := h.Service.FinishLogin(r.Context(), challengeID, r, signature, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "driver_auth_rejected", "Driver authentication was rejected.")
		return
	}
	h.Security.setCookiesWithMaxAge(w, result.Token, result.CSRFToken, sessionCookieMaxAge(result.Session.AbsoluteExpiresAt))
	writeJSON(w, http.StatusOK, map[string]any{"session_id": result.Session.ID, "expires_at": result.Session.AbsoluteExpiresAt})
}

func sessionCookieMaxAge(expiresAt time.Time) int {
	seconds := int(time.Until(expiresAt).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (h *DriverAuthHandler) me(w http.ResponseWriter, r *http.Request) {
	_, session, ok := h.Security.session(w, r)
	if !ok || session.SessionPolicy != applicationauth.DriverSession {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_account_id": session.UserAccountID, "person_id": session.PersonID,
		"session_id": session.ID, "session_policy": session.SessionPolicy, "company_id": session.ActiveCompanyID,
		"company_operation_id": session.ActiveOperationID, "expires_at": session.AbsoluteExpiresAt})
}

func (h *DriverAuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok || session.SessionPolicy != applicationauth.DriverSession {
		return
	}
	if err := h.Service.Logout(r.Context(), token, session.ID); err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	h.Security.clearCookies(w)
	writeNoContent(w)
}

func (h *DriverAuthHandler) contexts(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.session(w, r)
	if !ok || session.SessionPolicy != applicationauth.DriverSession {
		return
	}
	contexts, err := h.Service.ListAuthorizedContexts(r.Context(), token)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	writeJSON(w, http.StatusOK, contexts)
}
