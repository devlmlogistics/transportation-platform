package postgres

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationauth "transportation-platform/internal/application/auth"
)

type testAssertionCredential struct {
	RecordID     uuid.UUID
	CredentialID []byte
	PrivateKey   *ecdsa.PrivateKey
	PublicKey    []byte
}

func testWebAuthnNegativeAssertions(t *testing.T, ctx context.Context, service *applicationauth.OnlineAuthService, webauthnService *applicationauth.WebAuthnService, repository *OnlineAuthRepository, adminPool *pgxpool.Pool, fixture foundationFixture) {
	t.Helper()
	var diagnosticStage, diagnosticType string
	previousDiagnostic := webauthnService.Diagnostic
	webauthnService.Diagnostic = func(stage, errorType string) {
		diagnosticStage, diagnosticType = stage, errorType
	}
	t.Cleanup(func() { webauthnService.Diagnostic = previousDiagnostic })
	credential := newTestAssertionCredential(t)
	fallback := newTestAssertionCredential(t)
	insertCredential := func(value testAssertionCredential) {
		t.Helper()
		if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.authenticator_credential (
			id, user_account_id, credential_id, public_key, sign_count, display_name, status, metadata
		) VALUES ($1, $2, $3, $4, 0, 'crypto-test', 'ACTIVE', '{}'::jsonb)
		`, value.RecordID, fixture.UserGlobal, value.CredentialID, value.PublicKey); err != nil {
			t.Fatal(err)
		}
	}
	insertCredential(credential)
	insertCredential(fallback)

	loginNumber := 0
	login := func() applicationauth.LoginResult {
		t.Helper()
		loginNumber++
		result, err := service.Login(ctx, "global.admin@example.test", "correct horse battery staple", fmt.Sprintf("192.0.%d.1:443", 80+loginNumber), "webauthn-negative-test")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = service.Logout(ctx, result.Token, result.Session.ID) })
		return result
	}

	begin := func(result applicationauth.LoginResult) (uuid.UUID, applicationauth.WebAuthnChallenge, webauthn.SessionData) {
		t.Helper()
		session, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		records, err := repository.ListAuthenticatorCredentials(ctx, applicationauth.HashSessionToken(result.Token))
		if err != nil {
			t.Fatalf("list credentials before step-up: %v", err)
		}
		if len(records) == 0 {
			t.Fatal("no active credentials available before step-up")
		}
		challengeID, _, err := webauthnService.BeginStepUp(ctx, result.Token, session)
		if err != nil {
			t.Fatalf("begin step-up: %v (stage=%s error_type=%s)", err, diagnosticStage, diagnosticType)
		}
		challenge, err := repository.GetWebAuthnChallenge(ctx, applicationauth.HashSessionToken(result.Token), challengeID, applicationauth.StepUpChallenge)
		if err != nil {
			t.Fatal(err)
		}
		var ceremony webauthn.SessionData
		if err := json.Unmarshal(challenge.SessionData, &ceremony); err != nil {
			t.Fatal(err)
		}
		return challengeID, challenge, ceremony
	}

	finish := func(result applicationauth.LoginResult, challengeID uuid.UUID, payload []byte) (time.Time, error) {
		t.Helper()
		session, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			return time.Time{}, err
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/webauthn/step-up/finish", bytes.NewReader(payload))
		return webauthnService.FinishStepUp(ctx, result.Token, session, challengeID, request)
	}

	t.Run("valid_uv_true_elevates_session", func(t *testing.T) {
		result := login()
		challengeID, _, ceremony := begin(result)
		payload := signedAssertion(t, ceremony, credential, true, true)
		expires, err := finish(result, challengeID, payload)
		if err != nil {
			t.Fatalf("valid UV=true assertion rejected: %v (stage=%s error_type=%s)", err, diagnosticStage, diagnosticType)
		}
		resolved, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.AuthenticationLevel != "WEBAUTHN_STEP_UP" || resolved.StepUpExpiresAt == nil || !resolved.StepUpExpiresAt.Equal(expires) {
			t.Fatalf("session was not elevated correctly: level=%s step_up_expires_at=%v returned=%v", resolved.AuthenticationLevel, resolved.StepUpExpiresAt, expires)
		}
	})

	t.Run("valid_uv_false_rejected_without_elevation", func(t *testing.T) {
		result := login()
		challengeID, _, ceremony := begin(result)
		if _, err := finish(result, challengeID, signedAssertion(t, ceremony, credential, false, true)); err == nil {
			t.Fatal("UV=false assertion was accepted")
		}
		resolved, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.AuthenticationLevel != "PASSWORD" || resolved.StepUpExpiresAt != nil {
			t.Fatalf("UV=false assertion changed session: level=%s step_up_expires_at=%v", resolved.AuthenticationLevel, resolved.StepUpExpiresAt)
		}
	})

	t.Run("invalid_signature_rejected_without_elevation", func(t *testing.T) {
		result := login()
		challengeID, _, ceremony := begin(result)
		if _, err := finish(result, challengeID, signedAssertion(t, ceremony, credential, true, false)); err == nil {
			t.Fatal("invalid signature was accepted")
		}
		resolved, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.AuthenticationLevel != "PASSWORD" || resolved.StepUpExpiresAt != nil {
			t.Fatalf("invalid signature changed session: level=%s step_up_expires_at=%v", resolved.AuthenticationLevel, resolved.StepUpExpiresAt)
		}
	})

	t.Run("challenge_replay_does_not_renew_step_up", func(t *testing.T) {
		result := login()
		challengeID, _, ceremony := begin(result)
		payload := signedAssertion(t, ceremony, credential, true, true)
		firstExpiry, err := finish(result, challengeID, payload)
		if err != nil {
			t.Fatalf("valid assertion rejected: %v", err)
		}
		if _, err := finish(result, challengeID, payload); err == nil {
			t.Fatal("replayed assertion was accepted")
		}
		resolved, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.AuthenticationLevel != "WEBAUTHN_STEP_UP" || resolved.StepUpExpiresAt == nil || !resolved.StepUpExpiresAt.Equal(firstExpiry) {
			t.Fatalf("replay changed step-up state: level=%s step_up_expires_at=%v first=%v", resolved.AuthenticationLevel, resolved.StepUpExpiresAt, firstExpiry)
		}
	})

	t.Run("revoked_credential_rejected_even_with_valid_signature", func(t *testing.T) {
		result := login()
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.authenticator_credential
			   SET status = 'REVOKED', revoked_at = clock_timestamp(), revoked_reason = 'negative-test'
			 WHERE id = $1 AND status = 'ACTIVE'
		`, credential.RecordID); err != nil {
			t.Fatal(err)
		}
		challengeID, _, ceremony := begin(result)
		if _, err := finish(result, challengeID, signedAssertion(t, ceremony, credential, true, true)); err == nil {
			t.Fatal("revoked credential completed step-up")
		}
		resolved, err := service.ResolveSession(ctx, result.Token)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.AuthenticationLevel != "PASSWORD" || resolved.StepUpExpiresAt != nil {
			t.Fatalf("revoked credential changed session: level=%s step_up_expires_at=%v", resolved.AuthenticationLevel, resolved.StepUpExpiresAt)
		}
	})
}

func newTestAssertionCredential(t *testing.T) testAssertionCredential {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	credentialID := make([]byte, 16)
	if _, err := rand.Read(credentialID); err != nil {
		t.Fatal(err)
	}
	xCoord := privateKey.PublicKey.X.FillBytes(make([]byte, 32))
	yCoord := privateKey.PublicKey.Y.FillBytes(make([]byte, 32))
	publicKey, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: xCoord, -3: yCoord})
	if err != nil {
		t.Fatal(err)
	}
	return testAssertionCredential{RecordID: uuid.New(), CredentialID: credentialID, PrivateKey: privateKey, PublicKey: publicKey}
}

func signedAssertion(t *testing.T, ceremony webauthn.SessionData, credential testAssertionCredential, userVerified, validSignature bool) []byte {
	t.Helper()
	clientDataJSON, err := json.Marshal(map[string]any{
		"type": "webauthn.get", "challenge": ceremony.Challenge, "origin": "http://localhost:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	rpIDHash := sha256.Sum256([]byte("localhost"))
	authenticatorData := bytes.NewBuffer(nil)
	_, _ = authenticatorData.Write(rpIDHash[:])
	flags := byte(0x01)
	if userVerified {
		flags |= 0x04
	}
	_ = authenticatorData.WriteByte(flags)
	_ = binary.Write(authenticatorData, binary.BigEndian, uint32(1))
	clientDataHash := sha256.Sum256(clientDataJSON)
	signedData := append(append([]byte(nil), authenticatorData.Bytes()...), clientDataHash[:]...)
	digest := sha256.Sum256(signedData)
	r, s, err := ecdsa.Sign(rand.Reader, credential.PrivateKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	if err != nil {
		t.Fatal(err)
	}
	if !validSignature {
		signature[len(signature)-1] ^= 0x01
	}
	encode := func(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
	payload, err := json.Marshal(map[string]any{
		"id": encode(credential.CredentialID), "rawId": encode(credential.CredentialID), "type": "public-key",
		"response": map[string]string{
			"clientDataJSON": encode(clientDataJSON), "authenticatorData": encode(authenticatorData.Bytes()),
			"signature": encode(signature),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
