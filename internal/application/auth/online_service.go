package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrSessionNotFound      = errors.New("authentication session not found")
	ErrInvitationInvalid    = errors.New("invitation is invalid or expired")
	ErrBootstrapUnavailable = errors.New("bootstrap is unavailable")
)

type OnlineAuthService struct {
	Store OnlineAuthStore
	Now   func() time.Time
}

type LoginResult struct {
	Session   SessionRecord
	Token     []byte
	CSRFToken []byte
}

type IssuedInvitation struct {
	Invitation Invitation
	Token      []byte
}

func NewOnlineAuthService(store OnlineAuthStore) (*OnlineAuthService, error) {
	if store == nil {
		return nil, errors.New("online authentication store is required")
	}
	return &OnlineAuthService{Store: store, Now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *OnlineAuthService) Login(ctx context.Context, identifier, password, remoteAddr, userAgent string) (LoginResult, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(identifier) == "" || password == "" {
		return LoginResult{}, ErrAuthenticationFailed
	}
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	ipPrefix := privacyIPPrefix(remoteAddr)
	preKey := digestKey("login", normalized, ipPrefix)
	allowed, err := s.Store.ConsumeRateLimit(ctx, preKey, "LOGIN_IDENTIFIER_IP", 10, 60)
	if err != nil || !allowed {
		_ = s.recordLoginFailure(ctx, "RATE_LIMITED")
		return LoginResult{}, ErrAuthenticationFailed
	}
	credential, err := s.Store.ResolveLoginCredential(ctx, normalized)
	if err != nil || credential.KDFName != "argon2id" {
		_ = s.recordLoginFailure(ctx, "INVALID_CREDENTIALS")
		return LoginResult{}, ErrAuthenticationFailed
	}
	accountKey := digestKey("account", credential.UserAccountID.String(), ipPrefix)
	allowed, err = s.Store.ConsumeRateLimit(ctx, accountKey, "LOGIN_ACCOUNT_IP", 10, 60)
	if err != nil || !allowed {
		_ = s.recordLoginFailure(ctx, "RATE_LIMITED")
		return LoginResult{}, ErrAuthenticationFailed
	}
	valid, err := VerifyPassword(password, credential.PasswordHash)
	if err != nil || !valid {
		_ = s.recordLoginFailure(ctx, "INVALID_CREDENTIALS")
		return LoginResult{}, ErrAuthenticationFailed
	}

	grantRaw, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	grant := LoginGrant{
		ID:                    uuid.New(),
		GrantHash:             HashSessionToken(grantRaw),
		VerificationID:        credential.VerificationID,
		VerificationNonceHash: HashSessionToken(credential.VerificationNonce),
	}
	if err := s.Store.RecordVerifiedPasswordLogin(ctx, grant, 120); err != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	token, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	policy, _ := PolicyFor(AdministrativeSession)
	request := CreateSessionRequest{
		ID: uuid.New(), UserAccountID: credential.UserAccountID,
		TokenHash: HashSessionToken(token), CSRFSecretHash: HashSessionToken(csrfToken),
		Policy: policy, UserAgentHash: digestBytes(userAgent), IPPrefix: ipPrefix,
	}
	record, err := s.Store.IssueAuthSession(ctx, grant, request, uuid.New())
	if err != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	return LoginResult{Session: record, Token: token, CSRFToken: csrfToken}, nil
}

// LoginDriver is the bounded activation login. It is intentionally separate
// from the administrative login endpoint and issues only a short-lived
// DRIVER activation session. That session can request device enrolment, but
// it is not an operational device-authorized session.
func (s *OnlineAuthService) LoginDriver(ctx context.Context, identifier, password, remoteAddr, userAgent string) (LoginResult, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(identifier) == "" || password == "" {
		return LoginResult{}, ErrAuthenticationFailed
	}
	store, ok := s.Store.(DriverActivationStore)
	if !ok {
		return LoginResult{}, ErrAuthenticationFailed
	}
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	ipPrefix := privacyIPPrefix(remoteAddr)
	preKey := digestKey("driver-login", normalized, ipPrefix)
	allowed, err := s.Store.ConsumeRateLimit(ctx, preKey, "DRIVER_LOGIN_IDENTIFIER_IP", 10, 60)
	if err != nil || !allowed {
		_ = s.recordLoginFailure(ctx, "RATE_LIMITED")
		return LoginResult{}, ErrAuthenticationFailed
	}
	credential, err := s.Store.ResolveLoginCredential(ctx, normalized)
	if err != nil || credential.KDFName != "argon2id" {
		_ = s.recordLoginFailure(ctx, "INVALID_CREDENTIALS")
		return LoginResult{}, ErrAuthenticationFailed
	}
	valid, err := VerifyPassword(password, credential.PasswordHash)
	if err != nil || !valid {
		_ = s.recordLoginFailure(ctx, "INVALID_CREDENTIALS")
		return LoginResult{}, ErrAuthenticationFailed
	}
	grantRaw, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	grant := LoginGrant{ID: uuid.New(), GrantHash: HashSessionToken(grantRaw), VerificationID: credential.VerificationID, VerificationNonceHash: HashSessionToken(credential.VerificationNonce)}
	if err := s.Store.RecordVerifiedPasswordLogin(ctx, grant, 120); err != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	token, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	policy, _ := PolicyFor(DriverActivationSession)
	record, err := store.IssueDriverActivationSession(ctx, grant, CreateSessionRequest{
		ID: uuid.New(), UserAccountID: credential.UserAccountID,
		TokenHash: HashSessionToken(token), CSRFSecretHash: HashSessionToken(csrfToken),
		Policy: policy, UserAgentHash: digestBytes(userAgent), IPPrefix: ipPrefix,
	}, uuid.New())
	if err != nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	return LoginResult{Session: record, Token: token, CSRFToken: csrfToken}, nil
}

func (s *OnlineAuthService) ResolveSession(ctx context.Context, token []byte) (AuthenticatedSession, error) {
	if len(token) != 32 {
		return AuthenticatedSession{}, ErrSessionNotFound
	}
	result, err := s.Store.ResolveHTTPSession(ctx, HashSessionToken(token))
	if errors.Is(err, ErrInvalidSession) || errors.Is(err, ErrInactiveAccount) || errors.Is(err, ErrSessionNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return AuthenticatedSession{}, ErrSessionNotFound
	}
	if err != nil {
		return AuthenticatedSession{}, err
	}
	return result, nil
}

func PrincipalFromSession(session AuthenticatedSession) (Principal, error) {
	if session.ID == uuid.Nil || session.UserAccountID == uuid.Nil || session.PersonID == uuid.Nil || session.Status != "ACTIVE" {
		return Principal{}, ErrSessionNotFound
	}
	principal := Principal{
		UserAccountID: session.UserAccountID.String(),
		PersonID:      session.PersonID.String(),
		SessionID:     session.ID.String(), SessionEpoch: session.SessionEpoch,
		ContextVersion: session.ContextVersion, CSRFSecretHash: append([]byte(nil), session.CSRFSecretHash...),
		AuthenticationLevel: session.AuthenticationLevel, StepUpExpiresAt: session.StepUpExpiresAt,
	}
	if session.ActiveCompanyID != nil {
		principal.ActiveCompanyID = session.ActiveCompanyID.String()
	}
	if session.ActiveOperationID != nil {
		principal.ActiveOperationID = session.ActiveOperationID.String()
	}
	return principal, nil
}

func (s *OnlineAuthService) Logout(ctx context.Context, token []byte, sessionID uuid.UUID) error {
	if len(token) != 32 || sessionID == uuid.Nil {
		return ErrSessionNotFound
	}
	err := s.Store.RevokeSession(ctx, sessionID, HashSessionToken(token), "logout")
	if errors.Is(err, ErrInvalidSession) || errors.Is(err, ErrSessionNotFound) || errors.Is(err, ErrInactiveAccount) || errors.Is(err, ErrChallengeConsumed) {
		return ErrSessionNotFound
	}
	return err
}

func (s *OnlineAuthService) ListSessions(ctx context.Context, token []byte) ([]SessionSummary, error) {
	if len(token) != 32 {
		return nil, ErrSessionNotFound
	}
	return s.Store.ListAuthSessions(ctx, HashSessionToken(token))
}

func (s *OnlineAuthService) RevokeAll(ctx context.Context, token []byte) error {
	if len(token) != 32 {
		return ErrSessionNotFound
	}
	err := s.Store.RevokeAllSessions(ctx, HashSessionToken(token), "logout_all")
	if errors.Is(err, ErrInvalidSession) || errors.Is(err, ErrSessionNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return ErrSessionNotFound
	}
	return err
}

func (s *OnlineAuthService) ChangeContext(ctx context.Context, token []byte, companyID, operationID *uuid.UUID) (int64, error) {
	if len(token) != 32 {
		return 0, ErrSessionNotFound
	}
	version, err := s.Store.SetAuthSessionContext(ctx, HashSessionToken(token), uuid.New(), companyID, operationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSessionNotFound
	}
	return version, err
}

func (s *OnlineAuthService) IssueInvitation(ctx context.Context, sessionToken []byte, identifier, legalName string, expiresSeconds int) (IssuedInvitation, error) {
	if len(sessionToken) != 32 || strings.TrimSpace(identifier) == "" || strings.TrimSpace(legalName) == "" {
		return IssuedInvitation{}, ErrInvalidInput
	}
	raw, err := randomBytes(32)
	if err != nil {
		return IssuedInvitation{}, err
	}
	invit, err := s.Store.CreateAdminInvitation(ctx, HashSessionToken(sessionToken), uuid.New(), HashSessionToken(raw), identifier, legalName, expiresSeconds)
	if err != nil {
		return IssuedInvitation{}, err
	}
	return IssuedInvitation{Invitation: invit, Token: raw}, nil
}

func (s *OnlineAuthService) AcceptInvitation(ctx context.Context, token []byte, password string) (uuid.UUID, uuid.UUID, error) {
	if len(token) != 32 {
		return uuid.Nil, uuid.Nil, ErrInvitationInvalid
	}
	hash, parameters, err := HashPassword(password)
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrInvitationInvalid
	}
	accountID, personID, _, err := s.Store.ConsumeAdminInvitation(ctx, HashSessionToken(token), hash, parameters, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrInvitationInvalid
	}
	return accountID, personID, nil
}

func (s *OnlineAuthService) Bootstrap(ctx context.Context, secret, identifier, legalName, password string) (uuid.UUID, error) {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(identifier) == "" || strings.TrimSpace(legalName) == "" {
		return uuid.Nil, ErrBootstrapUnavailable
	}
	passwordHash, parameters, err := HashPassword(password)
	if err != nil {
		return uuid.Nil, ErrBootstrapUnavailable
	}
	requestRaw, err := randomBytes(32)
	if err != nil {
		return uuid.Nil, err
	}
	secretHash := digestBytes(secret)
	requestHash := HashSessionToken(requestRaw)
	requestID := uuid.New()
	if err := s.Store.PrepareBootstrapRequest(ctx, requestID, requestHash, secretHash, identifier, legalName, passwordHash, parameters, 600); err != nil {
		return uuid.Nil, ErrBootstrapUnavailable
	}
	accountID, _, err := s.Store.CompleteBootstrapRequest(ctx, requestHash, secretHash, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		return uuid.Nil, ErrBootstrapUnavailable
	}
	return accountID, nil
}

func (s *OnlineAuthService) recordLoginFailure(ctx context.Context, reason string) error {
	return s.Store.RecordLoginFailure(ctx, uuid.New(), reason)
}

func HashSessionToken(token []byte) []byte {
	hash := sha256.Sum256(token)
	return hash[:]
}

func digestBytes(value string) []byte {
	if value == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func digestKey(parts ...string) []byte {
	return digestBytes(strings.Join(parts, "\x00"))
}

func randomBytes(size int) ([]byte, error) {
	result := make([]byte, size)
	if _, err := rand.Read(result); err != nil {
		return nil, fmt.Errorf("generate random authentication value: %w", err)
	}
	return result, nil
}

func privacyIPPrefix(remoteAddr string) string {
	host := remoteAddr
	if parsedHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsedHost
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	if address.Is4() {
		return netip.PrefixFrom(address, 24).Masked().String()
	}
	return netip.PrefixFrom(address, 64).Masked().String()
}
