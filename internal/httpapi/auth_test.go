package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationauth "transportation-platform/internal/application/auth"
)

type fakeOnlineAuth struct {
	token   []byte
	csrf    []byte
	session applicationauth.AuthenticatedSession
	logout  bool
}

func (f *fakeOnlineAuth) Login(context.Context, string, string, string, string) (applicationauth.LoginResult, error) {
	return applicationauth.LoginResult{
		Token: f.token, CSRFToken: f.csrf,
		Session: applicationauth.SessionRecord{ID: f.session.ID, UserAccountID: f.session.UserAccountID, AbsoluteExpiresAt: f.session.AbsoluteExpiresAt},
	}, nil
}
func (f *fakeOnlineAuth) ResolveSession(_ context.Context, token []byte) (applicationauth.AuthenticatedSession, error) {
	if string(token) != string(f.token) {
		return applicationauth.AuthenticatedSession{}, applicationauth.ErrSessionNotFound
	}
	return f.session, nil
}
func (f *fakeOnlineAuth) Logout(context.Context, []byte, uuid.UUID) error {
	f.logout = true
	return nil
}
func (f *fakeOnlineAuth) ListSessions(context.Context, []byte) ([]applicationauth.SessionSummary, error) {
	return nil, nil
}
func (f *fakeOnlineAuth) RevokeAll(context.Context, []byte) error { return nil }
func (f *fakeOnlineAuth) ChangeContext(context.Context, []byte, *uuid.UUID, *uuid.UUID) (int64, error) {
	return 1, nil
}
func (f *fakeOnlineAuth) AcceptInvitation(context.Context, []byte, string) (uuid.UUID, uuid.UUID, error) {
	return uuid.New(), uuid.New(), nil
}

func TestAuthHandlerUsesCookiesAndCSRF(t *testing.T) {
	token := []byte(strings.Repeat("t", 32))
	csrf := []byte(strings.Repeat("c", 32))
	csrfHash := applicationauth.HashSessionToken(csrf)
	fake := &fakeOnlineAuth{
		token: token, csrf: csrf,
		session: applicationauth.AuthenticatedSession{
			ID: uuid.New(), UserAccountID: uuid.New(), PersonID: uuid.New(), Status: "ACTIVE",
			CSRFSecretHash: csrfHash, AbsoluteExpiresAt: time.Now().Add(time.Hour),
		},
	}
	handler, err := NewAuthHandler(fake, []string{"http://localhost:5173"}, false)
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"admin@example.test","password":"correct password"}`))
	login.Header.Set("Origin", "http://localhost:5173")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 2 || !cookies[0].HttpOnly || cookies[1].HttpOnly {
		t.Fatalf("unexpected authentication cookies: %+v", cookies)
	}
	if cookies[0].SameSite != http.SameSiteLaxMode || cookies[1].SameSite != http.SameSiteLaxMode {
		t.Fatal("authentication cookies did not use SameSite=Lax")
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Header.Set("Origin", "http://localhost:5173")
	logout.AddCookie(cookies[0])
	logout.AddCookie(cookies[1])
	withoutCSRF := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRF, logout)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf status = %d", withoutCSRF.Code)
	}

	logout = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Header.Set("Origin", "http://localhost:5173")
	logout.Header.Set(csrfHeaderName, cookies[1].Value)
	logout.AddCookie(cookies[0])
	logout.AddCookie(cookies[1])
	withCSRF := httptest.NewRecorder()
	handler.ServeHTTP(withCSRF, logout)
	if withCSRF.Code != http.StatusNoContent || !fake.logout {
		t.Fatalf("logout with csrf status = %d logout=%v", withCSRF.Code, fake.logout)
	}
}

func TestAuthHandlerRejectsUntrustedOrigin(t *testing.T) {
	fake := &fakeOnlineAuth{token: []byte(strings.Repeat("t", 32)), csrf: []byte(strings.Repeat("c", 32))}
	handler, err := NewAuthHandler(fake, []string{"http://localhost:5173"}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"admin@example.test","password":"password"}`))
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("untrusted origin status = %d", response.Code)
	}
}
