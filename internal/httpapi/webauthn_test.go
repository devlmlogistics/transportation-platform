package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClosedWebAuthnHandlerRejectsByDefault(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/webauthn/step-up/options", nil)
	ClosedWebAuthnHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}
