package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type rejectingWebAuthnStore struct{}

func (rejectingWebAuthnStore) BeginWebAuthnChallenge(context.Context, []byte, WebAuthnChallengeRequest) error {
	return errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) GetWebAuthnChallenge(context.Context, []byte, uuid.UUID, ChallengePurpose) (WebAuthnChallenge, error) {
	return WebAuthnChallenge{}, errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) CompleteWebAuthnRegistration(context.Context, []byte, CompleteRegistrationRequest) (uuid.UUID, *uuid.UUID, error) {
	return uuid.Nil, nil, errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) CompleteWebAuthnStepUp(context.Context, []byte, CompleteStepUpRequest) (time.Time, error) {
	return time.Time{}, errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) ListAuthenticatorCredentials(context.Context, []byte) ([]AuthenticatorCredentialRecord, error) {
	return nil, errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) RevokeOwnAuthenticator(context.Context, []byte, uuid.UUID, string) error {
	return errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) CreateDeviceEnrollmentRequest(context.Context, []byte, DeviceEnrollmentRequestInput) (DeviceEnrollmentRequest, error) {
	return DeviceEnrollmentRequest{}, errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) ApproveDeviceEnrollmentRequest(context.Context, []byte, uuid.UUID, string) error {
	return errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) RejectDeviceEnrollmentRequest(context.Context, []byte, uuid.UUID, string) error {
	return errors.New("unexpected store call")
}
func (rejectingWebAuthnStore) RevokeDeviceAuthorization(context.Context, []byte, uuid.UUID, string) error {
	return errors.New("unexpected store call")
}

func TestWebAuthnServiceRejectsRegistrationWithoutRecentAuthentication(t *testing.T) {
	service, err := NewWebAuthnService(rejectingWebAuthnStore{}, WebAuthnConfig{RPID: "localhost", RPDisplayName: "Test", RPOrigins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	session := AuthenticatedSession{ID: uuid.New(), UserAccountID: uuid.New(), PersonID: uuid.New(), Status: "ACTIVE", CreatedAt: time.Now().UTC().Add(-11 * time.Minute), AuthenticationLevel: "PASSWORD"}
	if _, _, err := service.BeginRegistration(context.Background(), make([]byte, 32), session, "new device", nil); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("stale password session error = %v", err)
	}
}

func TestWebAuthnServiceRejectsStepUpWithoutSessionToken(t *testing.T) {
	service, err := NewWebAuthnService(rejectingWebAuthnStore{}, WebAuthnConfig{RPID: "localhost", RPDisplayName: "Test", RPOrigins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.BeginStepUp(context.Background(), []byte("short"), AuthenticatedSession{ID: uuid.New()}); !errors.Is(err, ErrWebAuthnRejected) {
		t.Fatalf("missing token error = %v", err)
	}
}

func TestCeremonyBindingChangesWithSessionAndPurpose(t *testing.T) {
	session := uuid.New()
	if string(ceremonyBinding(session, RegistrationChallenge)) == string(ceremonyBinding(uuid.New(), RegistrationChallenge)) {
		t.Fatal("session binding was not unique")
	}
	if string(ceremonyBinding(session, RegistrationChallenge)) == string(ceremonyBinding(session, StepUpChallenge)) {
		t.Fatal("purpose binding was not unique")
	}
}
