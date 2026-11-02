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
	var sessionCookie, activeCSRFCookie, legacyCSRFCleanup *http.Cookie
	for _, cookie := range cookies {
		switch {
		case cookie.Name == sessionCookieName && cookie.Path == "/api/v1":
			sessionCookie = cookie
		case cookie.Name == csrfCookieName && cookie.Path == "/" && cookie.MaxAge > 0:
			activeCSRFCookie = cookie
		case cookie.Name == csrfCookieName && cookie.Path == legacyCSRFCookiePath && cookie.MaxAge < 0:
			legacyCSRFCleanup = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || activeCSRFCookie == nil || activeCSRFCookie.HttpOnly || legacyCSRFCleanup == nil {
		t.Fatalf("unexpected authentication cookies: %+v", cookies)
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode || activeCSRFCookie.SameSite != http.SameSiteLaxMode || legacyCSRFCleanup.SameSite != http.SameSiteLaxMode {
		t.Fatal("authentication cookies did not use SameSite=Lax")
	}
	if activeCSRFCookie.Path != "/" || activeCSRFCookie.HttpOnly {
		t.Fatalf("active csrf cookie path/httpOnly = %q/%v", activeCSRFCookie.Path, activeCSRFCookie.HttpOnly)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Header.Set("Origin", "http://localhost:5173")
	logout.AddCookie(sessionCookie)
	logout.AddCookie(activeCSRFCookie)
	withoutCSRF := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRF, logout)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf status = %d", withoutCSRF.Code)
	}

	logout = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Header.Set("Origin", "http://localhost:5173")
	logout.Header.Set(csrfHeaderName, activeCSRFCookie.Value)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(activeCSRFCookie)
	withCSRF := httptest.NewRecorder()
	handler.ServeHTTP(withCSRF, logout)
	if withCSRF.Code != http.StatusNoContent || !fake.logout {
		t.Fatalf("logout with csrf status = %d logout=%v", withCSRF.Code, fake.logout)
	}
	logoutCookies := withCSRF.Result().Cookies()
	var sessionCleanup, rootCSRFCleanup, legacyLogoutCSRFCleanup *http.Cookie
	for _, cookie := range logoutCookies {
		switch {
		case cookie.Name == sessionCookieName && cookie.Path == "/api/v1":
			sessionCleanup = cookie
		case cookie.Name == csrfCookieName && cookie.Path == "/":
			rootCSRFCleanup = cookie
		case cookie.Name == csrfCookieName && cookie.Path == legacyCSRFCookiePath:
			legacyLogoutCSRFCleanup = cookie
		}
	}
	if sessionCleanup == nil || sessionCleanup.MaxAge >= 0 || rootCSRFCleanup == nil || rootCSRFCleanup.MaxAge >= 0 || legacyLogoutCSRFCleanup == nil || legacyLogoutCSRFCleanup.MaxAge >= 0 {
		t.Fatalf("logout did not clear session and both csrf paths: %+v", logoutCookies)
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
