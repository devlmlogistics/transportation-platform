package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
)

type fakeDriverAuth struct {
	status applicationauth.DriverActivationRequestStatus
	err    error
	called bool
}

func (f *fakeDriverAuth) LoginDriver(context.Context, string, string, string, string) (applicationauth.LoginResult, error) {
	return applicationauth.LoginResult{}, errors.New("not implemented")
}
func (f *fakeDriverAuth) BeginLogin(context.Context, string, uuid.UUID) (uuid.UUID, *protocol.CredentialAssertion, error) {
	return uuid.Nil, nil, errors.New("not implemented")
}
func (f *fakeDriverAuth) FinishLogin(context.Context, uuid.UUID, *http.Request, []byte, string, string) (applicationauth.LoginResult, error) {
	return applicationauth.LoginResult{}, errors.New("not implemented")
}
func (f *fakeDriverAuth) ResolveSession(context.Context, []byte) (applicationauth.AuthenticatedSession, error) {
	return applicationauth.AuthenticatedSession{}, errors.New("not implemented")
}
func (f *fakeDriverAuth) GetDriverActivationRequestStatus(_ context.Context, _ []byte, requestID, installationID uuid.UUID) (applicationauth.DriverActivationRequestStatus, error) {
	f.called = true
	if f.status.RequestID != requestID || f.status.InstallationID != installationID {
		return applicationauth.DriverActivationRequestStatus{}, errors.New("binding mismatch")
	}
	return f.status, f.err
}
func (f *fakeDriverAuth) Logout(context.Context, []byte, uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeDriverAuth) ListAuthorizedContexts(context.Context, []byte) ([]applicationauth.DriverAuthorizedContext, error) {
	return nil, errors.New("not implemented")
}

func TestDriverActivationStatusBindsSessionAndInstallation(t *testing.T) {
	token := []byte("driver-session-token-01234567890")
	requestID := uuid.New()
	installationID := uuid.New()
	fakeDriver := &fakeDriverAuth{status: applicationauth.DriverActivationRequestStatus{
		RequestID: requestID, InstallationID: installationID, Status: "APPROVED", ExpiresAt: time.Now().Add(time.Hour),
	}}
	fakeOnline := &fakeOnlineAuth{token: token, session: applicationauth.AuthenticatedSession{
		ID: uuid.New(), UserAccountID: uuid.New(), PersonID: uuid.New(), Status: "ACTIVE",
		SessionPolicy: applicationauth.DriverActivationSession, AbsoluteExpiresAt: time.Now().Add(time.Hour),
	}}
	security, err := NewAuthHandler(fakeOnline, []string{"http://localhost:5173"}, false)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewDriverAuthHandler(fakeDriver, security)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/driver/auth/activation/status?device_request_id="+requestID.String()+"&installation_id="+installationID.String(), nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString(token), Path: "/api/v1"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !fakeDriver.called {
		t.Fatalf("status=%d called=%v body=%s", response.Code, fakeDriver.called, response.Body.String())
	}
	if !contains(response.Body.String(), `"status":"APPROVED"`) {
		t.Fatalf("status response omitted approval: %s", response.Body.String())
	}
}

func TestDriverActivationStatusRejectsNonActivationSession(t *testing.T) {
	token := []byte("driver-session-token-01234567890")
	fakeDriver := &fakeDriverAuth{status: applicationauth.DriverActivationRequestStatus{RequestID: uuid.New(), InstallationID: uuid.New()}}
	fakeOnline := &fakeOnlineAuth{token: token, session: applicationauth.AuthenticatedSession{
		ID: uuid.New(), UserAccountID: uuid.New(), PersonID: uuid.New(), Status: "ACTIVE",
		SessionPolicy: applicationauth.DriverSession, AbsoluteExpiresAt: time.Now().Add(time.Hour),
	}}
	security, err := NewAuthHandler(fakeOnline, []string{"http://localhost:5173"}, false)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewDriverAuthHandler(fakeDriver, security)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/driver/auth/activation/status?device_request_id="+uuid.NewString()+"&installation_id="+uuid.NewString(), nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString(token), Path: "/api/v1"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden || fakeDriver.called {
		t.Fatalf("status=%d called=%v body=%s", response.Code, fakeDriver.called, response.Body.String())
	}
}

func contains(value, want string) bool {
	return strings.Contains(value, want)
}
