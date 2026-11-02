package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrWebAuthnUnavailable = errors.New("webauthn is unavailable")
	ErrWebAuthnRejected    = errors.New("webauthn ceremony rejected")
	ErrStepUpRequired      = errors.New("recent WebAuthn step-up is required")
)

type WebAuthnService struct {
	Store      WebAuthnStore
	Engine     *webauthn.WebAuthn
	Now        func() time.Time
	Diagnostic func(stage, errorType string)
}

type WebAuthnConfig struct {
	RPID          string
	RPDisplayName string
	RPOrigins     []string
}

type webAuthnUser struct {
	id          []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u webAuthnUser) WebAuthnID() []byte                         { return u.id }
func (u webAuthnUser) WebAuthnName() string                       { return u.name }
func (u webAuthnUser) WebAuthnDisplayName() string                { return u.displayName }
func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func NewWebAuthnService(store WebAuthnStore, cfg WebAuthnConfig) (*WebAuthnService, error) {
	if store == nil || strings.TrimSpace(cfg.RPID) == "" || len(cfg.RPOrigins) == 0 {
		return nil, ErrWebAuthnUnavailable
	}
	engine, err := webauthn.New(&webauthn.Config{
		RPID:                        cfg.RPID,
		RPDisplayName:               cfg.RPDisplayName,
		RPOrigins:                   append([]string(nil), cfg.RPOrigins...),
		RPTopOrigins:                append([]string(nil), cfg.RPOrigins...),
		RPTopOriginVerificationMode: protocol.TopOriginExplicitVerificationMode,
		AuthenticatorSelection:      protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired},
	})
	if err != nil {
		return nil, fmt.Errorf("create WebAuthn engine: %w", err)
	}
	return &WebAuthnService{Store: store, Engine: engine, Now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *WebAuthnService) BeginRegistration(ctx context.Context, token []byte, session AuthenticatedSession, displayName string, deviceRequestID *uuid.UUID) (uuid.UUID, *protocol.CredentialCreation, error) {
	if !s.canRegister(session) || len(token) != 32 {
		return uuid.Nil, nil, ErrStepUpRequired
	}
	credentials, err := s.Store.ListAuthenticatorCredentials(ctx, HashSessionToken(token))
	if err != nil {
		return uuid.Nil, nil, ErrWebAuthnRejected
	}
	user := webAuthnUserForSession(session, credentials, true)
	creation, ceremony, err := s.Engine.BeginRegistration(user)
	if err != nil {
		return uuid.Nil, nil, ErrWebAuthnRejected
	}
	id, err := s.persistChallenge(ctx, token, session.ID, RegistrationChallenge, ceremony, deviceRequestID)
	return id, creation, err
}

func (s *WebAuthnService) FinishRegistration(ctx context.Context, token []byte, session AuthenticatedSession, challengeID uuid.UUID, request *http.Request, displayName string) error {
	if !s.canRegister(session) || len(token) != 32 || request == nil {
		s.reportDiagnostic("registration.session_policy", ErrStepUpRequired)
		return ErrStepUpRequired
	}
	if request.Body == nil {
		s.reportDiagnostic("registration.response_read", ErrWebAuthnRejected)
		return ErrWebAuthnRejected
	}
	challenge, err := s.Store.GetWebAuthnChallenge(ctx, HashSessionToken(token), challengeID, RegistrationChallenge)
	if err != nil {
		s.reportDiagnostic("registration.challenge_load", err)
		return ErrWebAuthnRejected
	}
	var ceremony webauthn.SessionData
	if err := json.Unmarshal(challenge.SessionData, &ceremony); err != nil {
		s.reportDiagnostic("registration.session_data_decode", err)
		return ErrWebAuthnRejected
	}
	credentials, err := s.Store.ListAuthenticatorCredentials(ctx, HashSessionToken(token))
	if err != nil {
		s.reportDiagnostic("registration.credentials_load", err)
		return ErrWebAuthnRejected
	}
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		s.reportDiagnostic("registration.response_read", err)
		return ErrWebAuthnRejected
	}
	if _, err := protocol.ParseCredentialCreationResponseBytes(payload); err != nil {
		s.reportDiagnostic("registration.response_parse", err)
		return ErrWebAuthnRejected
	}
	request.Body = io.NopCloser(bytes.NewReader(payload))
	request.ContentLength = int64(len(payload))
	credential, err := s.Engine.FinishRegistration(webAuthnUserForSession(session, credentials, true), ceremony, request)
	if err != nil {
		s.reportDiagnostic("registration.finish_registration", err)
		return ErrWebAuthnRejected
	}
	if credential == nil || !credential.Flags.UserVerified {
		s.reportDiagnostic("registration.finish_registration", ErrWebAuthnRejected)
		return ErrWebAuthnRejected
	}
	metadata, _ := json.Marshal(map[string]any{"attestation_type": credential.AttestationType, "aaguid": credential.Authenticator.AAGUID})
	transports := make([]string, 0, len(credential.Transport))
	for _, transport := range credential.Transport {
		transports = append(transports, string(transport))
	}
	binding := ceremonyBinding(session.ID, RegistrationChallenge)
	_, _, err = s.Store.CompleteWebAuthnRegistration(ctx, HashSessionToken(token), CompleteRegistrationRequest{
		ChallengeID: challengeID, ChallengeHash: challenge.ChallengeHash, BindingHash: binding,
		CredentialID: uuid.New(), CredentialKey: credential.ID, PublicKey: credential.PublicKey,
		SignCount: credential.Authenticator.SignCount, Transports: transports,
		BackupEligible: credential.Flags.BackupEligible, BackupState: boolText(credential.Flags.BackupState),
		UserVerified: credential.Flags.UserVerified, DisplayName: displayName, Metadata: metadata,
		AuditID: uuid.New(),
	})
	if err != nil {
		s.reportDiagnostic("registration.credential_persist", err)
	}
	return err
}

func (s *WebAuthnService) reportDiagnostic(stage string, err error) {
	if s == nil || s.Diagnostic == nil || err == nil {
		return
	}
	s.Diagnostic(stage, WebAuthnErrorType(err))
}

// WebAuthnErrorType returns only a safe error class for development diagnostics.
// It never includes error details, request data, tokens, or WebAuthn payloads.
func WebAuthnErrorType(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "postgres." + pgErr.Code
	}
	return fmt.Sprintf("%T", err)
}

func (s *WebAuthnService) BeginStepUp(ctx context.Context, token []byte, session AuthenticatedSession) (uuid.UUID, *protocol.CredentialAssertion, error) {
	if len(token) != 32 || session.ID == uuid.Nil {
		s.reportDiagnostic("step_up.begin.session_policy", ErrWebAuthnRejected)
		return uuid.Nil, nil, ErrWebAuthnRejected
	}
	credentials, err := s.Store.ListAuthenticatorCredentials(ctx, HashSessionToken(token))
	if err != nil {
		s.reportDiagnostic("step_up.begin.credentials_load", err)
		return uuid.Nil, nil, ErrWebAuthnRejected
	}
	user := webAuthnUserForSession(session, credentials, false)
	if len(user.credentials) == 0 {
		s.reportDiagnostic("step_up.begin.no_active_credentials", ErrWebAuthnRejected)
		return uuid.Nil, nil, ErrWebAuthnRejected
	}
	assertion, ceremony, err := s.Engine.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.reportDiagnostic("step_up.begin.login_options", err)
		return uuid.Nil, nil, ErrWebAuthnRejected
	}
	id, err := s.persistChallenge(ctx, token, session.ID, StepUpChallenge, ceremony, nil)
	if err != nil {
		s.reportDiagnostic("step_up.begin.challenge_persist", err)
	}
	return id, assertion, err
}

func (s *WebAuthnService) FinishStepUp(ctx context.Context, token []byte, session AuthenticatedSession, challengeID uuid.UUID, request *http.Request) (time.Time, error) {
	if len(token) != 32 || session.ID == uuid.Nil || request == nil {
		s.reportDiagnostic("step_up.session_policy", ErrWebAuthnRejected)
		return time.Time{}, ErrWebAuthnRejected
	}
	challenge, err := s.Store.GetWebAuthnChallenge(ctx, HashSessionToken(token), challengeID, StepUpChallenge)
	if err != nil {
		s.reportDiagnostic("step_up.challenge_load", err)
		return time.Time{}, ErrWebAuthnRejected
	}
	var ceremony webauthn.SessionData
	if err := json.Unmarshal(challenge.SessionData, &ceremony); err != nil {
		s.reportDiagnostic("step_up.session_data_decode", err)
		return time.Time{}, ErrWebAuthnRejected
	}
	credentials, err := s.Store.ListAuthenticatorCredentials(ctx, HashSessionToken(token))
	if err != nil {
		s.reportDiagnostic("step_up.credentials_load", err)
		return time.Time{}, ErrWebAuthnRejected
	}
	credential, err := s.Engine.FinishLogin(webAuthnUserForSession(session, credentials, false), ceremony, request)
	if err != nil || credential == nil || !credential.Flags.UserVerified {
		if err == nil {
			err = ErrWebAuthnRejected
		}
		s.reportDiagnostic("step_up.finish_login", err)
		return time.Time{}, ErrWebAuthnRejected
	}
	expires, err := s.Store.CompleteWebAuthnStepUp(ctx, HashSessionToken(token), CompleteStepUpRequest{
		ChallengeID: challengeID, ChallengeHash: challenge.ChallengeHash,
		BindingHash: ceremonyBinding(session.ID, StepUpChallenge), CredentialKey: credential.ID, AuditID: uuid.New(),
	})
	if err != nil {
		s.reportDiagnostic("step_up.persistence", err)
	}
	return expires, err
}

func (s *WebAuthnService) ListCredentials(ctx context.Context, token []byte) ([]AuthenticatorCredentialRecord, error) {
	if len(token) != 32 {
		return nil, ErrWebAuthnRejected
	}
	return s.Store.ListAuthenticatorCredentials(ctx, HashSessionToken(token))
}

func (s *WebAuthnService) RevokeCredential(ctx context.Context, token []byte, credentialID uuid.UUID, reason string) error {
	if len(token) != 32 || credentialID == uuid.Nil {
		return ErrWebAuthnRejected
	}
	return s.Store.RevokeOwnAuthenticator(ctx, HashSessionToken(token), credentialID, reason)
}

func (s *WebAuthnService) CreateDeviceEnrollmentRequest(ctx context.Context, token []byte, session AuthenticatedSession, companyID uuid.UUID, operationID *uuid.UUID, installationID uuid.UUID, installationPublicKey []byte, keyAlgorithm string) (DeviceEnrollmentRequest, error) {
	if len(token) != 32 || session.ID == uuid.Nil || companyID == uuid.Nil || installationID == uuid.Nil || len(installationPublicKey) < 16 || strings.TrimSpace(keyAlgorithm) == "" {
		return DeviceEnrollmentRequest{}, ErrWebAuthnRejected
	}
	correlation := uuid.NewString()
	binding := sha256.Sum256(append(append([]byte("tp-device-binding\x00"), session.ID[:]...), installationID[:]...))
	return s.Store.CreateDeviceEnrollmentRequest(ctx, HashSessionToken(token), DeviceEnrollmentRequestInput{
		ID: uuid.New(), CorrelationCode: correlation, CompanyID: companyID, CompanyOperationID: operationID,
		InstallationID: installationID, InstallationPublicKey: append([]byte(nil), installationPublicKey...),
		KeyAlgorithm: strings.TrimSpace(keyAlgorithm), BindingHash: binding[:], ExpiresAt: s.Now().Add(10 * time.Minute), AuditID: uuid.New(),
	})
}

func (s *WebAuthnService) ApproveDeviceEnrollmentRequest(ctx context.Context, token []byte, requestID uuid.UUID, reason string) error {
	if len(token) != 32 || requestID == uuid.Nil {
		return ErrWebAuthnRejected
	}
	return s.Store.ApproveDeviceEnrollmentRequest(ctx, HashSessionToken(token), requestID, reason)
}

func (s *WebAuthnService) RejectDeviceEnrollmentRequest(ctx context.Context, token []byte, requestID uuid.UUID, reason string) error {
	if len(token) != 32 || requestID == uuid.Nil {
		return ErrWebAuthnRejected
	}
	return s.Store.RejectDeviceEnrollmentRequest(ctx, HashSessionToken(token), requestID, reason)
}

func (s *WebAuthnService) RevokeDeviceAuthorization(ctx context.Context, token []byte, authorizationID uuid.UUID, reason string) error {
	if len(token) != 32 || authorizationID == uuid.Nil {
		return ErrWebAuthnRejected
	}
	return s.Store.RevokeDeviceAuthorization(ctx, HashSessionToken(token), authorizationID, reason)
}

func (s *WebAuthnService) canRegister(session AuthenticatedSession) bool {
	if session.ID == uuid.Nil || session.Status != "ACTIVE" {
		return false
	}
	now := s.Now()
	if session.AuthenticationLevel == "WEBAUTHN_STEP_UP" && session.StepUpExpiresAt != nil && now.Before(*session.StepUpExpiresAt) {
		return true
	}
	return now.Sub(session.CreatedAt) <= 10*time.Minute
}

func (s *WebAuthnService) persistChallenge(ctx context.Context, token []byte, sessionID uuid.UUID, purpose ChallengePurpose, ceremony *webauthn.SessionData, deviceRequest *uuid.UUID) (uuid.UUID, error) {
	data, err := json.Marshal(ceremony)
	if err != nil {
		return uuid.Nil, ErrWebAuthnRejected
	}
	raw, err := base64.RawURLEncoding.DecodeString(ceremony.Challenge)
	if err != nil || len(raw) < 16 {
		return uuid.Nil, ErrWebAuthnRejected
	}
	id := uuid.New()
	binding := ceremonyBinding(sessionID, purpose)
	if err := s.Store.BeginWebAuthnChallenge(ctx, HashSessionToken(token), WebAuthnChallengeRequest{
		ID: id, Purpose: purpose, ChallengeHash: hashBytes(raw), SessionData: data,
		SessionDataFormat: "webauthn.v1", BindingHash: binding, ExpiresAt: s.Now().Add(4*time.Minute + 30*time.Second),
		DeviceRequestID: deviceRequest, AuditID: uuid.New(),
	}); err != nil {
		return uuid.Nil, ErrWebAuthnRejected
	}
	return id, nil
}

func webAuthnUserForSession(session AuthenticatedSession, records []AuthenticatorCredentialRecord, includeRevoked bool) webAuthnUser {
	user := webAuthnUser{id: session.UserAccountID[:], name: session.UserAccountID.String(), displayName: session.PersonID.String()}
	for _, record := range records {
		if !includeRevoked && record.Status != "ACTIVE" {
			continue
		}
		if includeRevoked && record.Status == "REVOKED" {
			continue
		}
		transports := make([]protocol.AuthenticatorTransport, 0, len(record.Transports))
		for _, value := range record.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(value))
		}
		backupState := record.BackupState != nil && *record.BackupState == "backed_up"
		user.credentials = append(user.credentials, webauthn.Credential{ID: record.CredentialID, PublicKey: record.PublicKey,
			Transport: transports, Flags: webauthn.CredentialFlags{BackupEligible: record.BackupEligible, BackupState: backupState, UserVerified: record.UserVerifiedLast},
			Authenticator: webauthn.Authenticator{SignCount: record.SignCount}})
	}
	return user
}

func ceremonyBinding(sessionID uuid.UUID, purpose ChallengePurpose) []byte {
	hash := sha256.Sum256(append(append([]byte("tp-webauthn-binding\x00"), sessionID[:]...), []byte(string(purpose))...))
	return hash[:]
}

func hashBytes(value []byte) []byte { hash := sha256.Sum256(value); return hash[:] }
func boolText(value bool) string {
	if value {
		return "backed_up"
	}
	return "not_backed_up"
}
