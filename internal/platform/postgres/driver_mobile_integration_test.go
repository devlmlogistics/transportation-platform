package postgres

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationauth "transportation-platform/internal/application/auth"
)

func testDriverMobileAuth(t *testing.T, ctx context.Context, repository *OnlineAuthRepository, webauthnService *applicationauth.WebAuthnService, runtimePool, adminPool *pgxpool.Pool, fixture foundationFixture) {
	t.Helper()
	if _, err := runtimePool.Exec(ctx, `SELECT platform.complete_driver_login($1,$2,$3,$4,0,$5,$6,$7,$8,'', $9,$10)`, uuid.New(), makeHash(0xB1), makeHash(0xB2), makeHash(0xB3), uuid.New(), makeHash(0xB4), makeHash(0xB5), makeHash(0xB6), uuid.New(), uuid.New()); err == nil {
		t.Fatal("tp_runtime executed the DRIVER session issuance boundary")
	}
	identifier := "driver.mobile@example.test"
	hash, parameters, err := applicationauth.HashPassword("driver activation password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.login_identifier (id,user_account_id,normalized_identifier) VALUES ($1,$2,$3)
	`, uuid.New(), fixture.UserADriver, identifier); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.password_credential (id,user_account_id,purpose,password_hash,kdf_name,kdf_parameters)
		VALUES ($1,$2,'LOGIN',$3,'argon2id',$4)
	`, uuid.New(), fixture.UserADriver, hash, parameters); err != nil {
		t.Fatal(err)
	}
	credential := newTestAssertionCredential(t)
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.authenticator_credential (id,user_account_id,credential_id,public_key,sign_count,status,metadata)
		VALUES ($1,$2,$3,$4,0,'ACTIVE','{}'::jsonb)
	`, credential.RecordID, fixture.UserADriver, credential.CredentialID, credential.PublicKey); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `INSERT INTO platform.driver_profile (id,person_id,status) VALUES ($1,$2,'ACTIVE')`, uuid.New(), fixture.PersonADriver); err != nil {
		t.Fatal(err)
	}
	installationKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	installationID := uuid.New()
	deviceID := uuid.New()
	installationPublicKey := elliptic.Marshal(elliptic.P256(), installationKey.PublicKey.X, installationKey.PublicKey.Y)
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.device_authorization (id,installation_id,user_account_id,company_id,company_operation_id,installation_public_key,key_algorithm,status,approved_at)
		VALUES ($1,$2,$3,$4,$5,$6,'ECDSA_P256_SHA256','ACTIVE',clock_timestamp())
	`, deviceID, installationID, fixture.UserADriver, fixture.CompanyA, fixture.OperationA, installationPublicKey); err != nil {
		t.Fatal(err)
	}
	driverService, err := applicationauth.NewDriverMobileService(repository, webauthnService.Engine)
	if err != nil {
		t.Fatal(err)
	}
	activationService, err := applicationauth.NewOnlineAuthService(repository)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := activationService.LoginDriver(ctx, identifier, "driver activation password", "192.0.2.54:443", "test-driver")
	if err != nil {
		t.Fatal(err)
	}
	if activation.Session.ActiveCompanyID != nil || activation.Session.ActiveOperationID != nil || activation.Session.AbsoluteExpiresAt.Sub(activation.Session.CreatedAt) != time.Hour {
		t.Fatalf("activation session was not limited: %+v", activation.Session)
	}
	if err := activationService.Logout(ctx, activation.Token, activation.Session.ID); err != nil {
		t.Fatal(err)
	}
	challengeID, _, err := driverService.BeginLogin(ctx, identifier, installationID)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := repository.GetDriverLoginChallenge(ctx, challengeID)
	if err != nil {
		t.Fatal(err)
	}
	var ceremony webauthn.SessionData
	if err := json.Unmarshal(challenge.SessionData, &ceremony); err != nil {
		t.Fatal(err)
	}
	credentialPayload := signedAssertion(t, ceremony, credential, true, true)
	proofMessage := append(append([]byte("tp-driver-possession\x00"), challenge.ChallengeHash...), installationID[:]...)
	digest := sha256.Sum256(proofMessage)
	r, s, err := ecdsa.Sign(rand.Reader, installationKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	proof := make([]byte, 64)
	r.FillBytes(proof[:32])
	s.FillBytes(proof[32:])
	request := httptest.NewRequest(http.MethodPost, "/api/v1/driver/auth/webauthn/finish", bytes.NewReader(credentialPayload))
	result, err := driverService.FinishLogin(ctx, challengeID, request, proof, "test-driver", "192.0.2.55:443")
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.UserAccountID != fixture.UserADriver || result.Session.ActiveCompanyID == nil || *result.Session.ActiveCompanyID != fixture.CompanyA || result.Session.ActiveOperationID == nil || *result.Session.ActiveOperationID != fixture.OperationA {
		t.Fatalf("unexpected device-scoped driver session: %+v", result.Session)
	}
	if result.Session.AbsoluteExpiresAt.Sub(result.Session.CreatedAt) != 12*time.Hour || result.Session.IdleExpiresAt.Sub(result.Session.CreatedAt) != time.Hour {
		t.Fatalf("unexpected driver TTL: created=%s idle=%s absolute=%s", result.Session.CreatedAt, result.Session.IdleExpiresAt, result.Session.AbsoluteExpiresAt)
	}
	contexts, err := driverService.ListAuthorizedContexts(ctx, result.Token)
	if err != nil || len(contexts) != 1 || contexts[0].CompanyID != fixture.CompanyA || contexts[0].CompanyOperationID == nil || *contexts[0].CompanyOperationID != fixture.OperationA {
		t.Fatalf("unexpected authorized driver contexts: contexts=%+v err=%v", contexts, err)
	}
	resolved, err := repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(result.Token))
	if err != nil || resolved.SessionPolicy != applicationauth.DriverSession || resolved.ActiveCompanyID == nil || *resolved.ActiveCompanyID != fixture.CompanyA {
		t.Fatalf("driver session did not resolve through the online boundary: session=%+v err=%v", resolved, err)
	}
	if _, err := driverService.FinishLogin(ctx, challengeID, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(credentialPayload)), proof, "test-driver", "192.0.2.55:443"); err == nil {
		t.Fatal("driver login challenge replay was accepted")
	}
}
