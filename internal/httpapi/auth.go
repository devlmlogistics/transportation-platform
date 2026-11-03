package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/postgres"
)

const (
	sessionCookieName    = "tp_session"
	csrfCookieName       = "tp_csrf"
	csrfCookiePath       = "/"
	legacyCSRFCookiePath = "/api/v1"
	csrfHeaderName       = "X-CSRF-Token"
)

type OnlineAuthHTTPService interface {
	Login(context.Context, string, string, string, string) (applicationauth.LoginResult, error)
	ResolveSession(context.Context, []byte) (applicationauth.AuthenticatedSession, error)
	Logout(context.Context, []byte, uuid.UUID) error
	ListSessions(context.Context, []byte) ([]applicationauth.SessionSummary, error)
	RevokeAll(context.Context, []byte) error
	ChangeContext(context.Context, []byte, *uuid.UUID, *uuid.UUID) (int64, error)
	AcceptInvitation(context.Context, []byte, string) (uuid.UUID, uuid.UUID, error)
}

// ClosedAuthHandler preserves the pre-authentication behavior when no real
// authentication boundary is configured.
func ClosedAuthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Online authentication is not configured.")
	})
}

type AuthHandler struct {
	Service        OnlineAuthHTTPService
	AllowedOrigins map[string]struct{}
	CookieSecure   bool
}

func NewAuthHandler(service OnlineAuthHTTPService, allowedOrigins []string, cookieSecure bool) (*AuthHandler, error) {
	if service == nil {
		return nil, errors.New("authentication service is required")
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if value := strings.TrimSpace(origin); value != "" {
			origins[value] = struct{}{}
		}
	}
	if len(origins) == 0 {
		return nil, errors.New("at least one allowed origin is required")
	}
	return &AuthHandler{Service: service, AllowedOrigins: origins, CookieSecure: cookieSecure}, nil
}

func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth")
	switch {
	case path == "/login" && r.Method == http.MethodPost:
		h.login(w, r)
	case path == "/me" && r.Method == http.MethodGet:
		h.me(w, r)
	case path == "/logout" && r.Method == http.MethodPost:
		h.logout(w, r)
	case path == "/sessions" && r.Method == http.MethodGet:
		h.sessions(w, r)
	case path == "/revoke-all" && r.Method == http.MethodPost:
		h.revokeAll(w, r)
	case path == "/context" && r.Method == http.MethodPost:
		h.context(w, r)
	case path == "/invitations/accept" && r.Method == http.MethodPost:
		h.acceptInvitation(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "Authentication endpoint not found.")
	}
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	if !h.checkOrigin(w, r) {
		return
	}
	var input struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	result, err := h.Service.Login(r.Context(), input.Identifier, input.Password, r.RemoteAddr, r.UserAgent())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_failed", "Invalid credentials.")
		return
	}
	h.setCookies(w, result.Token, result.CSRFToken)
	writeJSON(w, http.StatusOK, map[string]any{
		"user_account_id": result.Session.UserAccountID,
		"session_id":      result.Session.ID,
		"expires_at":      result.Session.AbsoluteExpiresAt,
	})
}

func (h *AuthHandler) me(w http.ResponseWriter, r *http.Request) {
	_, session, ok := h.session(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_account_id":     session.UserAccountID,
		"person_id":           session.PersonID,
		"session_id":          session.ID,
		"session_policy":      session.SessionPolicy,
		"context_version":     session.ContextVersion,
		"active_company_id":   session.ActiveCompanyID,
		"active_operation_id": session.ActiveOperationID,
		"expires_at":          session.AbsoluteExpiresAt,
	})
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	token, _, ok := h.requireMutationSession(w, r)
	if !ok {
		return
	}
	session, err := h.Service.ResolveSession(r.Context(), token)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	if err := h.Service.Logout(r.Context(), token, session.ID); err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	h.clearCookies(w)
	writeNoContent(w)
}

func (h *AuthHandler) sessions(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.session(w, r)
	if !ok {
		return
	}
	if session.SessionPolicy == applicationauth.DriverActivationSession {
		writeError(w, r, http.StatusForbidden, "activation_session_limited", "This session is limited to device enrollment.")
		return
	}
	result, err := h.Service.ListSessions(r.Context(), token)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) revokeAll(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.requireMutationSession(w, r)
	if !ok {
		return
	}
	if session.SessionPolicy == applicationauth.DriverActivationSession {
		writeError(w, r, http.StatusForbidden, "activation_session_limited", "This session is limited to device enrollment.")
		return
	}
	if err := h.Service.RevokeAll(r.Context(), token); err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	h.clearCookies(w)
	writeNoContent(w)
}

func (h *AuthHandler) context(w http.ResponseWriter, r *http.Request) {
	token, session, ok := h.requireMutationSession(w, r)
	if !ok {
		return
	}
	if session.SessionPolicy == applicationauth.DriverActivationSession {
		writeError(w, r, http.StatusForbidden, "activation_session_limited", "This session is limited to device enrollment.")
		return
	}
	var input struct {
		CompanyID   *uuid.UUID `json:"company_id"`
		OperationID *uuid.UUID `json:"company_operation_id"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	if input.OperationID != nil && input.CompanyID == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_context", "An operation requires a company context.")
		return
	}
	version, err := h.Service.ChangeContext(r.Context(), token, input.CompanyID, input.OperationID)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "context_not_allowed", "The requested context is not available.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"context_version": version, "company_id": input.CompanyID, "company_operation_id": input.OperationID})
}

func (h *AuthHandler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.checkOrigin(w, r) {
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	token, err := base64.RawURLEncoding.DecodeString(input.Token)
	if err != nil || len(token) != 32 {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	if _, _, err := h.Service.AcceptInvitation(r.Context(), token, input.Password); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_invitation", "The invitation is invalid or expired.")
		return
	}
	writeNoContent(w)
}

func (h *AuthHandler) session(w http.ResponseWriter, r *http.Request) ([]byte, applicationauth.AuthenticatedSession, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(token) != 32 {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	session, err := h.Service.ResolveSession(r.Context(), token)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	return token, session, true
}

func (h *AuthHandler) requireMutationSession(w http.ResponseWriter, r *http.Request) ([]byte, applicationauth.AuthenticatedSession, bool) {
	if !h.checkOrigin(w, r) {
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	token, session, ok := h.session(w, r)
	if !ok {
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	csrfCookie, err := r.Cookie(csrfCookieName)
	header := r.Header.Get(csrfHeaderName)
	if err != nil || header == "" || csrfCookie.Value == "" || subtle.ConstantTimeCompare([]byte(header), []byte(csrfCookie.Value)) != 1 {
		writeError(w, r, http.StatusForbidden, "csrf_failed", "CSRF validation failed.")
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	csrfRaw, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil || len(csrfRaw) != 32 {
		writeError(w, r, http.StatusForbidden, "csrf_failed", "CSRF validation failed.")
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	hash := sha256.Sum256(csrfRaw)
	if subtle.ConstantTimeCompare(hash[:], session.CSRFSecretHash) != 1 {
		writeError(w, r, http.StatusForbidden, "csrf_failed", "CSRF validation failed.")
		return nil, applicationauth.AuthenticatedSession{}, false
	}
	return token, session, true
}

func (h *AuthHandler) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		writeError(w, r, http.StatusForbidden, "origin_required", "A trusted origin is required.")
		return false
	}
	if _, ok := h.AllowedOrigins[origin]; !ok {
		writeError(w, r, http.StatusForbidden, "origin_denied", "The request origin is not allowed.")
		return false
	}
	return true
}

func (h *AuthHandler) setCookies(w http.ResponseWriter, token, csrf []byte) {
	h.setCookiesWithMaxAge(w, token, csrf, 8*60*60)
}

func (h *AuthHandler) setCookiesWithMaxAge(w http.ResponseWriter, token, csrf []byte, maxAge int) {
	if maxAge < 1 {
		maxAge = 1
	}
	secureToken := base64.RawURLEncoding.EncodeToString(token)
	secureCSRF := base64.RawURLEncoding.EncodeToString(csrf)
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: secureToken, Path: "/api/v1", HttpOnly: true, Secure: h.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	h.expireCSRFCookie(w, legacyCSRFCookiePath)
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: secureCSRF, Path: csrfCookiePath, HttpOnly: false, Secure: h.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (h *AuthHandler) clearCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/api/v1", HttpOnly: true, Secure: h.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	h.expireCSRFCookie(w, legacyCSRFCookiePath)
	h.expireCSRFCookie(w, csrfCookiePath)
}

func (h *AuthHandler) expireCSRFCookie(w http.ResponseWriter, path string) {
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: "", Path: path, HttpOnly: false, Secure: h.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func protectAdministrativeMutations(next http.Handler, authHandler *AuthHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutationMethod(r.Method) {
			if _, _, ok := authHandler.requireMutationSession(w, r); !ok {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SessionPrincipalResolver is the only production adapter from HTTP session
// state to the existing administrative authorization contract.
type SessionPrincipalResolver struct {
	Service OnlineAuthHTTPService
}

func (r SessionPrincipalResolver) Resolve(ctx context.Context, request *http.Request) (postgres.Principal, error) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return postgres.Principal{}, applicationauth.ErrSessionNotFound
	}
	token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(token) != 32 {
		return postgres.Principal{}, applicationauth.ErrSessionNotFound
	}
	session, err := r.Service.ResolveSession(ctx, token)
	if err != nil {
		return postgres.Principal{}, err
	}
	return postgres.Principal{
		UserAccountID:       session.UserAccountID,
		PersonID:            session.PersonID,
		Active:              true,
		SessionID:           session.ID,
		SessionEpoch:        session.SessionEpoch,
		ContextVersion:      session.ContextVersion,
		ActiveCompanyID:     session.ActiveCompanyID,
		ActiveOperationID:   session.ActiveOperationID,
		CSRFSecretHash:      session.CSRFSecretHash,
		AuthenticationLevel: session.AuthenticationLevel,
		StepUpExpiresAt:     session.StepUpExpiresAt,
		SessionPolicy:       string(session.SessionPolicy),
	}, nil
}

func parseSessionCookie(request *http.Request) ([]byte, error) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(cookie.Value)
}
