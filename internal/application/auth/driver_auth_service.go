package auth

import (
	"context"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
)

// DriverAuthService composes the existing identity/password boundary with the
// driver-specific WebAuthn ceremony. It does not create a second identity or
// session implementation.
type DriverAuthService struct {
	Activation *OnlineAuthService
	Mobile     *DriverMobileService
}

func (s *DriverAuthService) LoginDriver(ctx context.Context, identifier, password, remoteAddr, userAgent string) (LoginResult, error) {
	if s == nil || s.Activation == nil {
		return LoginResult{}, ErrAuthenticationFailed
	}
	return s.Activation.LoginDriver(ctx, identifier, password, remoteAddr, userAgent)
}

func (s *DriverAuthService) BeginLogin(ctx context.Context, identifier string, installationID uuid.UUID) (uuid.UUID, *protocol.CredentialAssertion, error) {
	if s == nil || s.Mobile == nil {
		return uuid.Nil, nil, ErrDriverAuthRejected
	}
	return s.Mobile.BeginLogin(ctx, identifier, installationID)
}

func (s *DriverAuthService) FinishLogin(ctx context.Context, challengeID uuid.UUID, request *http.Request, possessionSignature []byte, userAgent, remoteAddr string) (LoginResult, error) {
	if s == nil || s.Mobile == nil {
		return LoginResult{}, ErrDriverAuthRejected
	}
	return s.Mobile.FinishLogin(ctx, challengeID, request, possessionSignature, userAgent, remoteAddr)
}

func (s *DriverAuthService) ResolveSession(ctx context.Context, token []byte) (AuthenticatedSession, error) {
	if s == nil || s.Activation == nil {
		return AuthenticatedSession{}, ErrSessionNotFound
	}
	return s.Activation.ResolveSession(ctx, token)
}

func (s *DriverAuthService) GetDriverActivationRequestStatus(ctx context.Context, token []byte, requestID, installationID uuid.UUID) (DriverActivationRequestStatus, error) {
	if s == nil || s.Activation == nil {
		return DriverActivationRequestStatus{}, ErrSessionNotFound
	}
	return s.Activation.GetDriverActivationRequestStatus(ctx, token, requestID, installationID)
}

func (s *DriverAuthService) Logout(ctx context.Context, token []byte, sessionID uuid.UUID) error {
	if s == nil || s.Activation == nil {
		return ErrSessionNotFound
	}
	return s.Activation.Logout(ctx, token, sessionID)
}

func (s *DriverAuthService) ListAuthorizedContexts(ctx context.Context, token []byte) ([]DriverAuthorizedContext, error) {
	if s == nil || s.Mobile == nil {
		return nil, ErrDriverAuthRejected
	}
	return s.Mobile.ListAuthorizedContexts(ctx, token)
}
