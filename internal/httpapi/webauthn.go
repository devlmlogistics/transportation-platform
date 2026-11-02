package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
)

type WebAuthnHTTPService interface {
	BeginRegistration(context.Context, []byte, applicationauth.AuthenticatedSession, string, *uuid.UUID) (uuid.UUID, *protocol.CredentialCreation, error)
	FinishRegistration(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, *http.Request, string) error
	BeginStepUp(context.Context, []byte, applicationauth.AuthenticatedSession) (uuid.UUID, *protocol.CredentialAssertion, error)
	FinishStepUp(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, *http.Request) (time.Time, error)
	ListCredentials(context.Context, []byte) ([]applicationauth.AuthenticatorCredentialRecord, error)
	RevokeCredential(context.Context, []byte, uuid.UUID, string) error
	CreateDeviceEnrollmentRequest(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, *uuid.UUID, uuid.UUID, []byte, string) (applicationauth.DeviceEnrollmentRequest, error)
	ApproveDeviceEnrollmentRequest(context.Context, []byte, uuid.UUID, string) error
	RejectDeviceEnrollmentRequest(context.Context, []byte, uuid.UUID, string) error
	RevokeDeviceAuthorization(context.Context, []byte, uuid.UUID, string) error
}

type WebAuthnHandler struct {
	Auth     OnlineAuthHTTPService
	Service  WebAuthnHTTPService
	Security *AuthHandler
}

func ClosedWebAuthnHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "WebAuthn is not configured.")
	})
}

func NewWebAuthnHandler(authService OnlineAuthHTTPService, service WebAuthnHTTPService, origins []string, secure bool) (*WebAuthnHandler, error) {
	security, err := NewAuthHandler(authService, origins, secure)
	if err != nil || service == nil {
		return nil, errors.New("WebAuthn authentication service is required")
	}
	return &WebAuthnHandler{Auth: authService, Service: service, Security: security}, nil
}

func (h *WebAuthnHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/webauthn")
	switch {
	case path == "/registration/options" && r.Method == http.MethodPost:
		h.registrationOptions(w, r)
	case path == "/registration/finish" && r.Method == http.MethodPost:
		h.registrationFinish(w, r)
	case path == "/step-up/options" && r.Method == http.MethodPost:
		h.stepUpOptions(w, r)
	case path == "/step-up/finish" && r.Method == http.MethodPost:
		h.stepUpFinish(w, r)
	case path == "/credentials" && r.Method == http.MethodGet:
		h.credentials(w, r)
	case path == "/credentials/revoke" && r.Method == http.MethodPost:
		h.revokeCredential(w, r)
	case path == "/device-enrollment-requests" && r.Method == http.MethodPost:
		h.createDeviceRequest(w, r)
	case strings.HasPrefix(path, "/device-enrollment-requests/") && strings.HasSuffix(path, "/approve") && r.Method == http.MethodPost:
		h.changeDeviceRequest(w, r, true)
	case strings.HasPrefix(path, "/device-enrollment-requests/") && strings.HasSuffix(path, "/reject") && r.Method == http.MethodPost:
		h.changeDeviceRequest(w, r, false)
	case strings.HasPrefix(path, "/device-authorizations/") && strings.HasSuffix(path, "/revoke") && r.Method == http.MethodPost:
		h.revokeDevice(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "WebAuthn endpoint not found.")
	}
}

func (h *WebAuthnHandler) createDeviceRequest(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	var input struct {
		CompanyID             string `json:"company_id"`
		CompanyOperationID    string `json:"company_operation_id"`
		InstallationID        string `json:"installation_id"`
		InstallationPublicKey string `json:"installation_public_key"`
		KeyAlgorithm          string `json:"key_algorithm"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	companyID, err := uuid.Parse(input.CompanyID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid company_id is required.")
		return
	}
	installationID, err := uuid.Parse(input.InstallationID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid installation_id is required.")
		return
	}
	var operationID *uuid.UUID
	if strings.TrimSpace(input.CompanyOperationID) != "" {
		value, parseErr := uuid.Parse(input.CompanyOperationID)
		if parseErr != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid operation id.")
			return
		}
		operationID = &value
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(input.InstallationPublicKey)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid installation key.")
		return
	}
	result, err := h.Service.CreateDeviceEnrollmentRequest(r.Context(), token, session, companyID, operationID, installationID, publicKey, input.KeyAlgorithm)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "device_request_rejected", "The device enrollment request was rejected.")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *WebAuthnHandler) changeDeviceRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	token, _, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid request id.")
		return
	}
	id, err := uuid.Parse(parts[len(parts)-2])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid request id.")
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	if approve {
		err = h.Service.ApproveDeviceEnrollmentRequest(r.Context(), token, id, input.Reason)
	} else {
		err = h.Service.RejectDeviceEnrollmentRequest(r.Context(), token, id, input.Reason)
	}
	if err != nil {
		writeError(w, r, http.StatusForbidden, "device_request_rejected", "The device enrollment request was not changed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": map[bool]string{true: "APPROVED", false: "REJECTED"}[approve]})
}

func (h *WebAuthnHandler) revokeDevice(w http.ResponseWriter, r *http.Request) {
	token, _, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid authorization id.")
		return
	}
	id, err := uuid.Parse(parts[len(parts)-2])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid authorization id.")
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	if err := h.Service.RevokeDeviceAuthorization(r.Context(), token, id, input.Reason); err != nil {
		writeError(w, r, http.StatusForbidden, "device_not_revoked", "The device authorization was not revoked.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "REVOKED"})
}

func (h *WebAuthnHandler) registrationOptions(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	var input struct {
		DisplayName     string `json:"display_name"`
		DeviceRequestID string `json:"device_request_id"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	var deviceRequestID *uuid.UUID
	if strings.TrimSpace(input.DeviceRequestID) != "" {
		value, parseErr := uuid.Parse(input.DeviceRequestID)
		if parseErr != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "Invalid device request id.")
			return
		}
		deviceRequestID = &value
	}
	id, options, err := h.Service.BeginRegistration(r.Context(), token, session, input.DisplayName, deviceRequestID)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "webauthn_rejected", "WebAuthn registration is not available for this session.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"challenge_id": id, "options": options})
}

func (h *WebAuthnHandler) registrationFinish(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	var envelope ceremonyEnvelope
	if decodeJSON(w, r, &envelope) != nil {
		return
	}
	challengeID, err := uuid.Parse(envelope.ChallengeID)
	if err != nil || len(envelope.Credential) == 0 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid challenge and credential are required.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(envelope.Credential))
	r.ContentLength = int64(len(envelope.Credential))
	if err := h.Service.FinishRegistration(r.Context(), token, session, challengeID, r, envelope.DisplayName); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "webauthn_rejected", "WebAuthn registration was rejected.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (h *WebAuthnHandler) stepUpOptions(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	id, options, err := h.Service.BeginStepUp(r.Context(), token, session)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "webauthn_rejected", "A WebAuthn step-up is not available.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"challenge_id": id, "options": options})
}

func (h *WebAuthnHandler) stepUpFinish(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	var envelope ceremonyEnvelope
	if decodeJSON(w, r, &envelope) != nil {
		return
	}
	challengeID, err := uuid.Parse(envelope.ChallengeID)
	if err != nil || len(envelope.Credential) == 0 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid challenge and credential are required.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(envelope.Credential))
	r.ContentLength = int64(len(envelope.Credential))
	expires, err := h.Service.FinishStepUp(r.Context(), token, session, challengeID, r)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "webauthn_rejected", "WebAuthn step-up was rejected.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authentication_level": "WEBAUTHN_STEP_UP", "expires_at": expires})
}

func (h *WebAuthnHandler) credentials(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.session(w, r)
	if !ok {
		return
	}
	if session.SessionPolicy == applicationauth.DriverActivationSession {
		writeError(w, r, http.StatusForbidden, "activation_session_limited", "This session is limited to device enrollment.")
		return
	}
	credentials, err := h.Service.ListCredentials(r.Context(), token)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	result := make([]map[string]any, 0, len(credentials))
	for _, credential := range credentials {
		result = append(result, map[string]any{"id": credential.ID, "display_name": credential.DisplayName, "status": credential.Status,
			"backup_eligible": credential.BackupEligible, "backup_state": credential.BackupState, "user_verified_last": credential.UserVerifiedLast,
			"created_at": credential.CreatedAt, "last_used_at": credential.LastUsedAt, "revoked_at": credential.RevokedAt})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *WebAuthnHandler) revokeCredential(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.Security.requireMutationSession(w, r)
	if !ok {
		return
	}
	if session.SessionPolicy == applicationauth.DriverActivationSession {
		writeError(w, r, http.StatusForbidden, "activation_session_limited", "This session is limited to device enrollment.")
		return
	}
	var input struct {
		CredentialID string `json:"credential_id"`
		Reason       string `json:"reason"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	id, err := uuid.Parse(input.CredentialID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "A valid credential id is required.")
		return
	}
	if err := h.Service.RevokeCredential(r.Context(), token, id, input.Reason); err != nil {
		writeError(w, r, http.StatusConflict, "credential_not_revoked", "The credential could not be revoked.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

type ceremonyEnvelope struct {
	ChallengeID string          `json:"challenge_id"`
	DisplayName string          `json:"display_name"`
	Credential  json.RawMessage `json:"credential"`
}
