package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	auth "transportation-platform/internal/application/auth"
)

type driverManagementStub struct {
	calls   int
	company uuid.UUID
	action  string
	status  string
}

func (s *driverManagementStub) Manage(_ context.Context, _ []byte, c uuid.UUID, a string, _ auth.DriverManagementInput, _ uuid.UUID) (auth.DriverManagementResult, error) {
	s.calls++
	s.company = c
	s.action = a
	return auth.DriverManagementResult{Status: s.status}, nil
}

func TestDriverManagementHTTPBoundary(t *testing.T) {
	token := make([]byte, 32)
	csrf := make([]byte, 32)
	csrf[0] = 1
	hash := sha256.Sum256(csrf)
	authService := &fakeOnlineAuth{token: token, session: auth.AuthenticatedSession{ID: uuid.New(), SessionPolicy: auth.AdministrativeSession, CSRFSecretHash: hash[:]}}
	security, err := NewAuthHandler(authService, []string{"https://demo.test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	company := uuid.New()
	path := "/api/v1/supervisor/drivers?company_id=" + company.String()
	for _, tc := range []struct {
		name               string
		session, csrf, key bool
		status             string
		want               int
		called             bool
	}{
		{"no session", false, false, false, "OK", 401, false},
		{"no csrf", true, false, true, "CREATED", 403, false},
		{"no idempotency key", true, true, false, "CREATED", 400, false},
		{"permission denied", true, true, true, "FORBIDDEN", 403, true},
		{"created", true, true, true, "CREATED", 201, true},
		{"duplicate", true, true, true, "CONFLICT", 409, true},
		{"rate limited", true, true, true, "RATE_LIMITED", 429, true},
		{"review", true, true, true, "REVIEW_REQUIRED", 202, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &driverManagementStub{status: tc.status}
			handler := DriverManagementHandler{Service: stub, Security: security}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"given_name":"Test","family_name":"Driver","identifier":"test@example.test","phone":"+56912345678","operation_ids":[]}`))
			req.Header.Set("Origin", "https://demo.test")
			if tc.session {
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: base64.RawURLEncoding.EncodeToString(token)})
			}
			if tc.csrf {
				value := base64.RawURLEncoding.EncodeToString(csrf)
				req.Header.Set("X-CSRF-Token", value)
				req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: value})
			}
			if tc.key {
				req.Header.Set("Idempotency-Key", uuid.NewString())
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
			if (stub.calls > 0) != tc.called {
				t.Fatal("invalid HTTP boundary reached service")
			}
			if tc.called && (stub.company != company || stub.action != "create") {
				t.Fatal("URL company mismatch")
			}
		})
	}
}
