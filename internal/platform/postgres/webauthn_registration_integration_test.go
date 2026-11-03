package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationauth "transportation-platform/internal/application/auth"
)

func testWebAuthnRegistration(t *testing.T, ctx context.Context, repository *OnlineAuthRepository, service *applicationauth.OnlineAuthService, adminPool *pgxpool.Pool) {
	t.Helper()
	login, err := service.Login(ctx, "global.admin@example.test", "correct horse battery staple", "192.0.2.21:443", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Logout(ctx, login.Token, login.Session.ID) })
	session, err := service.ResolveSession(ctx, login.Token)
	if err != nil {
		t.Fatal(err)
	}

	webauthnService, err := applicationauth.NewWebAuthnService(repository, applicationauth.WebAuthnConfig{
		RPID: "localhost", RPDisplayName: "Transportation Platform", RPOrigins: []string{"http://localhost:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	challengeID, _, err := webauthnService.BeginRegistration(ctx, login.Token, session, "test passkey", nil)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	challenge, err := repository.GetWebAuthnChallenge(ctx, applicationauth.HashSessionToken(login.Token), challengeID, applicationauth.RegistrationChallenge)
	if err != nil {
		t.Fatal(err)
	}
	payload, credentialID := validRegistrationPayload(t, challenge.SessionData)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/webauthn/registration/finish", bytes.NewReader(payload))
	if err := webauthnService.FinishRegistration(ctx, login.Token, session, challengeID, request, "test passkey"); err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	var credentialStatus, challengeStatus string
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.authenticator_credential WHERE credential_id = $1`, credentialID).Scan(&credentialStatus); err != nil {
		t.Fatal(err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.auth_challenge WHERE id = $1`, challengeID).Scan(&challengeStatus); err != nil {
		t.Fatal(err)
	}
	if credentialStatus != "ACTIVE" || challengeStatus != "CONSUMED" {
		t.Fatalf("registration state credential=%s challenge=%s", credentialStatus, challengeStatus)
	}
}

func validRegistrationPayload(t *testing.T, sessionData []byte) ([]byte, []byte) {
	t.Helper()
	credential := newTestAssertionCredential(t)
	return validRegistrationPayloadForCredential(t, sessionData, credential), credential.CredentialID
}

func validRegistrationPayloadForCredential(t *testing.T, sessionData []byte, credential testAssertionCredential) []byte {
	t.Helper()
	var ceremony webauthn.SessionData
	if err := json.Unmarshal(sessionData, &ceremony); err != nil {
		t.Fatal(err)
	}
	publicKey := credential.PublicKey
	if len(publicKey) == 0 {
		t.Fatal("registration credential has no public key")
	}
	credentialKey := credential.CredentialID
	if len(credentialKey) == 0 {
		t.Fatal("registration credential has no credential ID")
	}
	clientData, err := json.Marshal(map[string]string{
		"type": "webauthn.create", "challenge": ceremony.Challenge, "origin": "http://localhost:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	rpIDHash := sha256.Sum256([]byte("localhost"))
	authenticatorData := bytes.NewBuffer(nil)
	_, _ = authenticatorData.Write(rpIDHash[:])
	_ = authenticatorData.WriteByte(0x45)
	_ = binary.Write(authenticatorData, binary.BigEndian, uint32(0))
	_, _ = authenticatorData.Write(make([]byte, 16))
	_ = binary.Write(authenticatorData, binary.BigEndian, uint16(len(credentialKey)))
	_, _ = authenticatorData.Write(credentialKey)
	_, _ = authenticatorData.Write(publicKey)
	attestationObject, err := webauthncbor.Marshal(map[string]any{
		"fmt": "none", "authData": authenticatorData.Bytes(), "attStmt": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
	payload, err := json.Marshal(map[string]any{
		"id": encode(credentialKey), "rawId": encode(credentialKey), "type": "public-key",
		"response": map[string]string{"clientDataJSON": encode(clientData), "attestationObject": encode(attestationObject)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
