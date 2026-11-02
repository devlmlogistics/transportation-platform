package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

var ErrDriverAuthRejected = errors.New("driver authentication rejected")

type DriverMobileService struct {
	Store      DriverMobileStore
	Engine     *webauthn.WebAuthn
	Now        func() time.Time
	Diagnostic func(stage, errorType string)
}

func NewDriverMobileService(store DriverMobileStore, engine *webauthn.WebAuthn) (*DriverMobileService, error) {
	if store == nil || engine == nil {
		return nil, ErrWebAuthnUnavailable
	}
	return &DriverMobileService{Store: store, Engine: engine, Now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *DriverMobileService) BeginLogin(ctx context.Context, identifier string, installationID uuid.UUID) (uuid.UUID, *protocol.CredentialAssertion, error) {
	if s == nil || s.Store == nil || s.Engine == nil || strings.TrimSpace(identifier) == "" || installationID == uuid.Nil {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	key := digestKey("driver-webauthn", strings.ToLower(strings.TrimSpace(identifier)), installationID.String())
	allowed, err := s.Store.ConsumeRateLimit(ctx, key, "DRIVER_WEBAUTHN_INSTALLATION", 10, 60)
	if err != nil || !allowed {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	credentials, err := s.Store.ListDriverLoginCredentials(ctx, strings.ToLower(strings.TrimSpace(identifier)), installationID)
	if err != nil || len(credentials) == 0 {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	user := driverWebAuthnUser(credentials)
	assertion, ceremony, err := s.Engine.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	challengeRaw, err := base64.RawURLEncoding.DecodeString(ceremony.Challenge)
	if err != nil || len(challengeRaw) < 16 {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	data, err := json.Marshal(ceremony)
	if err != nil {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	id := uuid.New()
	binding := driverLoginBinding(id, installationID)
	if err := s.Store.BeginDriverLoginChallenge(ctx, DriverLoginChallengeRequest{
		Identifier: strings.ToLower(strings.TrimSpace(identifier)), InstallationID: installationID,
		ChallengeID: id, DeviceAuthorizationID: credentials[0].DeviceAuthorizationID,
		ChallengeHash: hashBytes(challengeRaw), SessionData: data, SessionDataFormat: "webauthn.v1",
		BindingHash: binding, ExpiresAt: s.Now().Add(4*time.Minute + 30*time.Second), AuditID: uuid.New(),
	}); err != nil {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	return id, assertion, nil
}

func (s *DriverMobileService) FinishLogin(ctx context.Context, challengeID uuid.UUID, request *http.Request, possessionSignature []byte, userAgent, remoteAddr string) (LoginResult, error) {
	if s == nil || s.Store == nil || s.Engine == nil || challengeID == uuid.Nil || request == nil {
		return LoginResult{}, ErrDriverAuthRejected
	}
	challenge, err := s.Store.GetDriverLoginChallenge(ctx, challengeID)
	if err != nil {
		s.reportDiagnostic("driver_login.challenge_load", err)
		return LoginResult{}, ErrDriverAuthRejected
	}
	var ceremony webauthn.SessionData
	if err := json.Unmarshal(challenge.SessionData, &ceremony); err != nil {
		s.reportDiagnostic("driver_login.session_data_decode", err)
		return LoginResult{}, ErrDriverAuthRejected
	}
	credentials, err := s.Store.ListDriverLoginCredentialsForChallenge(ctx, challengeID)
	if err != nil || len(credentials) == 0 {
		s.reportDiagnostic("driver_login.credentials_load", err)
		return LoginResult{}, ErrDriverAuthRejected
	}
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		return LoginResult{}, ErrDriverAuthRejected
	}
	request.Body = io.NopCloser(bytes.NewReader(payload))
	request.ContentLength = int64(len(payload))
	credential, err := s.Engine.FinishLogin(driverWebAuthnUser(credentials), ceremony, request)
	if err != nil || credential == nil || !credential.Flags.UserVerified {
		if err == nil {
			err = ErrDriverAuthRejected
		}
		s.reportDiagnostic("driver_login.finish_webauthn", err)
		return LoginResult{}, ErrDriverAuthRejected
	}
	if !verifyDriverPossession(challenge.InstallationPublicKey, challenge.KeyAlgorithm, challenge.ChallengeHash, challenge.InstallationID, possessionSignature) {
		s.reportDiagnostic("driver_login.installation_possession", ErrDriverAuthRejected)
		return LoginResult{}, ErrDriverAuthRejected
	}
	token, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, err := randomBytes(32)
	if err != nil {
		return LoginResult{}, err
	}
	record, err := s.Store.CompleteDriverLogin(ctx, CompleteDriverLoginRequest{
		ChallengeID: challenge.ID, ChallengeHash: challenge.ChallengeHash, BindingHash: challenge.BindingHash,
		CredentialKey: credential.ID, SignCount: credential.Authenticator.SignCount, SessionID: uuid.New(),
		TokenHash: HashSessionToken(token), CSRFSecretHash: HashSessionToken(csrfToken),
		UserAgentHash: digestBytes(userAgent), IPPrefix: privacyIPPrefix(remoteAddr),
		AuditID: uuid.New(), LoginAuditID: uuid.New(),
	})
	if err != nil {
		s.reportDiagnostic("driver_login.session_persist", err)
		return LoginResult{}, ErrDriverAuthRejected
	}
	return LoginResult{Session: record, Token: token, CSRFToken: csrfToken}, nil
}

func (s *DriverMobileService) ListAuthorizedContexts(ctx context.Context, token []byte) ([]DriverAuthorizedContext, error) {
	if s == nil || s.Store == nil || len(token) != 32 {
		return nil, ErrDriverAuthRejected
	}
	return s.Store.ListDriverAuthorizedContexts(ctx, HashSessionToken(token))
}

func (s *DriverMobileService) reportDiagnostic(stage string, err error) {
	if s != nil && s.Diagnostic != nil && err != nil {
		s.Diagnostic(stage, WebAuthnErrorType(err))
	}
}

func driverWebAuthnUser(credentials []DriverLoginCredential) webAuthnUser {
	user := webAuthnUser{id: credentials[0].UserAccountID[:], name: credentials[0].UserAccountID.String(), displayName: credentials[0].UserAccountID.String()}
	for _, item := range credentials {
		transports := make([]protocol.AuthenticatorTransport, 0, len(item.Credential.Transports))
		for _, value := range item.Credential.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(value))
		}
		backupState := item.Credential.BackupState != nil && *item.Credential.BackupState == "backed_up"
		user.credentials = append(user.credentials, webauthn.Credential{
			ID: item.Credential.CredentialID, PublicKey: item.Credential.PublicKey, Transport: transports,
			Flags:         webauthn.CredentialFlags{BackupEligible: item.Credential.BackupEligible, BackupState: backupState, UserVerified: item.Credential.UserVerifiedLast},
			Authenticator: webauthn.Authenticator{SignCount: item.Credential.SignCount},
		})
	}
	return user
}

func driverLoginBinding(challengeID, installationID uuid.UUID) []byte {
	hash := sha256.Sum256(append(append(append([]byte("tp-driver-login-binding\x00"), challengeID[:]...), installationID[:]...), []byte("DRIVER_LOGIN")...))
	return hash[:]
}

func verifyDriverPossession(publicKey []byte, algorithm string, challengeHash []byte, installationID uuid.UUID, signature []byte) bool {
	if algorithm != "ECDSA_P256_SHA256" || len(publicKey) != 65 || publicKey[0] != 4 || len(signature) != 64 || len(challengeHash) != 32 || installationID == uuid.Nil {
		return false
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return false
	}
	message := append(append([]byte("tp-driver-possession\x00"), challengeHash...), installationID[:]...)
	digest := sha256.Sum256(message)
	key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
}
