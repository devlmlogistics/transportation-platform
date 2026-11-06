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

	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
)

type invitationHTTPAuthStub struct{}

func (invitationHTTPAuthStub) Login(context.Context, string, string, string, string) (applicationauth.LoginResult, error) {
	return applicationauth.LoginResult{}, nil
}
func (invitationHTTPAuthStub) ResolveSession(context.Context, []byte) (applicationauth.AuthenticatedSession, error) {
	return applicationauth.AuthenticatedSession{}, nil
}
func (invitationHTTPAuthStub) Logout(context.Context, []byte, uuid.UUID) error { return nil }
func (invitationHTTPAuthStub) ListSessions(context.Context, []byte) ([]applicationauth.SessionSummary, error) {
	return nil, nil
}
func (invitationHTTPAuthStub) RevokeAll(context.Context, []byte) error { return nil }
func (invitationHTTPAuthStub) ChangeContext(context.Context, []byte, *uuid.UUID, *uuid.UUID) (int64, error) {
	return 0, nil
}
func (invitationHTTPAuthStub) AcceptInvitation(context.Context, []byte, string) (uuid.UUID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, nil
}

type invitationHTTPServiceStub struct {
	preview        applicationauth.DriverEnrollmentInvitationPreview
	candidateError error
}

func (s invitationHTTPServiceStub) Create(context.Context, []byte, applicationauth.AuthenticatedSession, applicationauth.DriverEnrollmentInvitationInput) (applicationauth.IssuedDriverEnrollmentInvitation, error) {
	return applicationauth.IssuedDriverEnrollmentInvitation{}, nil
}
func (s invitationHTTPServiceStub) List(context.Context, []byte, applicationauth.AuthenticatedSession) ([]applicationauth.DriverEnrollmentInvitation, error) {
	return nil, nil
}
func (s invitationHTTPServiceStub) Candidates(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, *uuid.UUID) ([]applicationauth.DriverEnrollmentCandidate, error) {
	return nil, s.candidateError
}
func (s invitationHTTPServiceStub) Revoke(context.Context, []byte, applicationauth.AuthenticatedSession, uuid.UUID, string) error {
	return nil
}
func (s invitationHTTPServiceStub) Resolve(context.Context, []byte) (applicationauth.DriverEnrollmentInvitationPreview, error) {
	return s.preview, nil
}
func (s invitationHTTPServiceStub) Consume(context.Context, applicationauth.DriverEnrollmentConsumeInput) (applicationauth.DriverEnrollmentActivation, error) {
	return applicationauth.DriverEnrollmentActivation{
		LoginResult: applicationauth.LoginResult{Token: make([]byte, 32), CSRFToken: make([]byte, 32)},
		Request:     applicationauth.DeviceEnrollmentRequest{ID: uuid.New(), CorrelationCode: "REQ-TEST", Status: "PENDING", ExpiresAt: time.Now().Add(time.Minute)},
	}, nil
}

func TestDriverInvitationPublicValidateRequiresTrustedOrigin(t *testing.T) {
	security, err := NewAuthHandler(invitationHTTPAuthStub{}, []string{"https://demo.test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewDriverInvitationHandler(invitationHTTPServiceStub{preview: applicationauth.DriverEnrollmentInvitationPreview{ID: uuid.New(), CompanyName: "Acme"}}, security, true)
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/driver/invitations/validate", strings.NewReader(`{"token":"`+token+`"}`))
	request.Header.Set("Origin", "https://unexpected.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("untrusted origin status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/driver/invitations/validate", strings.NewReader(`{"token":"`+token+`"}`))
	request.Header.Set("Origin", "https://demo.test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Acme") {
		t.Fatalf("trusted validation status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDriverInvitationConsumeSetsSessionCookiesWithoutExposingTokenInResponse(t *testing.T) {
	security, err := NewAuthHandler(invitationHTTPAuthStub{}, []string{"https://demo.test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewDriverInvitationHandler(invitationHTTPServiceStub{}, security, true)
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	body := `{"token":"` + token + `","confirmation_code":"12345678","installation_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","installation_public_key":"cHVibGljLWtleQ","key_algorithm":"ECDSA_P256_SHA256"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/driver/invitations/consume", strings.NewReader(body))
	request.Header.Set("Origin", "https://demo.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("consume status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), token) {
		t.Fatal("bearer invitation token was returned in the response")
	}
	if cookies := response.Header().Values("Set-Cookie"); len(cookies) < 2 {
		t.Fatalf("expected session and csrf cookies, got %v", cookies)
	}
}

func TestDriverCandidatesRequiresSessionAndReturnsSafePermissionError(t *testing.T) {
	security, err := NewAuthHandler(invitationHTTPAuthStub{}, []string{"https://demo.test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewDriverInvitationHandler(invitationHTTPServiceStub{candidateError: errors.New("internal SQL permission detail")}, security, false)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/auth/webauthn/driver-invitations/candidates?company_id=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		want := http.StatusUnauthorized
		if authenticated {
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString(make([]byte, 32))})
			want = http.StatusForbidden
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want || strings.Contains(response.Body.String(), "internal SQL") {
			t.Fatalf("candidate response status=%d want=%d body=%s", response.Code, want, response.Body.String())
		}
	}
}
