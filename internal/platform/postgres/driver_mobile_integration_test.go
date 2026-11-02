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
	"sync"
	"testing"
	"time"

	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationauth "transportation-platform/internal/application/auth"
)

func testDriverMobileAuth(t *testing.T, ctx context.Context, repository *OnlineAuthRepository, webauthnService *applicationauth.WebAuthnService, runtimePool, authPool, driverAuthPool, adminPool *pgxpool.Pool, fixture foundationFixture) {
	t.Helper()
	var authCanIssue, driverAuthCanIssue bool
	if err := authPool.QueryRow(ctx, `SELECT has_function_privilege(current_user, 'platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid)', 'EXECUTE')`).Scan(&authCanIssue); err != nil {
		t.Fatal(err)
	}
	if err := driverAuthPool.QueryRow(ctx, `SELECT has_function_privilege(current_user, 'platform.complete_driver_login(uuid,bytea,bytea,bytea,bigint,uuid,bytea,bytea,bytea,text,uuid,uuid)', 'EXECUTE')`).Scan(&driverAuthCanIssue); err != nil {
		t.Fatal(err)
	}
	if authCanIssue || !driverAuthCanIssue {
		t.Fatalf("unexpected driver issuance privileges: tp_auth_runtime=%v tp_driver_auth_runtime=%v", authCanIssue, driverAuthCanIssue)
	}
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
	resolvedActivation, err := activationService.ResolveSession(ctx, activation.Token)
	if err != nil || resolvedActivation.SessionPolicy != applicationauth.DriverActivationSession || resolvedActivation.ActiveCompanyID != nil {
		t.Fatalf("activation session did not resolve as limited: session=%+v err=%v", resolvedActivation, err)
	}
	var activationPolicy string
	var activationDeviceID *uuid.UUID
	if err := adminPool.QueryRow(ctx, `SELECT session_policy, device_authorization_id FROM platform.auth_session WHERE id = $1`, activation.Session.ID).Scan(&activationPolicy, &activationDeviceID); err != nil {
		t.Fatal(err)
	}
	if activationPolicy != string(applicationauth.DriverActivationSession) || activationDeviceID != nil {
		t.Fatalf("activation session was not unscoped: policy=%s device=%v", activationPolicy, activationDeviceID)
	}
	activationInstallationID := uuid.New()
	activationInstallationKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	activationPublicKey := elliptic.Marshal(elliptic.P256(), activationInstallationKey.PublicKey.X, activationInstallationKey.PublicKey.Y)
	enrollment, err := webauthnService.CreateDeviceEnrollmentRequest(ctx, activation.Token, resolvedActivation,
		fixture.CompanyA, &fixture.OperationA, activationInstallationID, activationPublicKey, "ECDSA_P256_SHA256")
	if err != nil {
		t.Fatalf("activation session could not request enrollment: %v", err)
	}
	supervisorToken := makeHash(0xC1)
	supervisorPolicy, err := applicationauth.PolicyFor(applicationauth.AdministrativeSession)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSyntheticSession(ctx, adminPool, applicationauth.CreateSessionRequest{
		ID: uuid.New(), UserAccountID: fixture.UserASupervisor,
		TokenHash: applicationauth.HashSessionToken(supervisorToken), Policy: supervisorPolicy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApproveDeviceEnrollmentRequest(ctx, applicationauth.HashSessionToken(supervisorToken), enrollment.ID, "test approval"); err != nil {
		t.Fatalf("approved enrollment request was not accepted: %v", err)
	}
	if _, err := authPool.Exec(ctx, `SELECT platform.complete_driver_login($1,$2,$3,$4,0,$5,$6,$7,$8,'',$9,$10)`, uuid.New(), makeHash(0xB8), makeHash(0xB9), makeHash(0xBA), uuid.New(), makeHash(0xBB), makeHash(0xBC), makeHash(0xBD), uuid.New(), uuid.New()); err == nil {
		t.Fatal("tp_auth_runtime executed the DRIVER session issuance boundary")
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
	if _, err := authPool.Exec(ctx, `SELECT platform.complete_driver_login($1,$2,$3,$4,0,$5,$6,$7,$8,'',$9,$10)`, challenge.ID, challenge.ChallengeHash, challenge.BindingHash, credential.CredentialID, uuid.New(), makeHash(0xC2), makeHash(0xC3), makeHash(0xC4), uuid.New(), uuid.New()); err == nil {
		t.Fatal("tp_auth_runtime issued a DRIVER session without the Go verification boundary")
	}
	badProof := make([]byte, 64)
	if _, err := driverService.FinishLogin(ctx, challengeID, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(credentialPayload)), badProof, "test-driver", "192.0.2.55:443"); err == nil {
		t.Fatal("invalid installation proof was accepted")
	}
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
	if contexts, err := repository.ListDriverAuthorizedContexts(ctx, applicationauth.HashSessionToken(result.Token)); err != nil || len(contexts) != 1 || contexts[0].CompanyID != fixture.CompanyA {
		t.Fatalf("company A context was not isolated: contexts=%+v err=%v", contexts, err)
	}
	if contexts, err := repository.ListDriverAuthorizedContexts(ctx, applicationauth.HashSessionToken(makeHash(0xD0))); err != nil || len(contexts) != 0 {
		t.Fatalf("unknown token exposed driver contexts: contexts=%+v err=%v", contexts, err)
	}
	if credentials, err := repository.ListDriverLoginCredentials(ctx, identifier, uuid.New()); err != nil || len(credentials) != 0 {
		t.Fatalf("installation from another scope exposed credentials: credentials=%+v err=%v", credentials, err)
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.company_membership SET status = 'SUSPENDED' WHERE company_id = $1 AND person_id = $2`, fixture.CompanyA, fixture.PersonADriver); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(result.Token)); err == nil {
		t.Fatal("company membership revocation did not invalidate DRIVER resolution")
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.company_membership SET status = 'ACTIVE' WHERE company_id = $1 AND person_id = $2`, fixture.CompanyA, fixture.PersonADriver); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.operation_membership SET status = 'SUSPENDED' WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3`, fixture.CompanyA, fixture.OperationA, fixture.PersonADriver); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(result.Token)); err == nil {
		t.Fatal("operation membership revocation did not invalidate DRIVER resolution")
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.operation_membership SET status = 'ACTIVE' WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3`, fixture.CompanyA, fixture.OperationA, fixture.PersonADriver); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.authenticator_credential SET status = 'REVOKED', revoked_at = clock_timestamp() WHERE id = $1`, credential.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := driverService.BeginLogin(ctx, identifier, installationID); err == nil {
		t.Fatal("revoked credential produced driver login options")
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.authenticator_credential SET status = 'ACTIVE', revoked_at = NULL WHERE id = $1`, credential.RecordID); err != nil {
		t.Fatal(err)
	}
	testDriverSessionExpiry(t, ctx, repository, adminPool, fixture, deviceID, credential.RecordID)
	start := make(chan struct{})
	var concurrentResolveErr, concurrentRevokeErr error
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, concurrentResolveErr = repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(result.Token))
	}()
	go func() {
		defer group.Done()
		<-start
		concurrentRevokeErr = repository.RevokeDeviceAuthorization(ctx, applicationauth.HashSessionToken(supervisorToken), deviceID, "concurrent test revoke")
	}()
	close(start)
	group.Wait()
	if concurrentRevokeErr != nil {
		t.Fatalf("concurrent device revocation failed: %v", concurrentRevokeErr)
	}
	if _, err := repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(result.Token)); err == nil {
		t.Fatalf("revoked device session resolved after concurrent race; resolver err=%v", concurrentResolveErr)
	}
	var deviceStatus, sessionStatus string
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.device_authorization WHERE id = $1`, deviceID).Scan(&deviceStatus); err != nil {
		t.Fatal(err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.auth_session WHERE id = $1`, result.Session.ID).Scan(&sessionStatus); err != nil {
		t.Fatal(err)
	}
	if deviceStatus != "REVOKED" || sessionStatus != "REVOKED" {
		t.Fatalf("concurrent revocation state: device=%s session=%s", deviceStatus, sessionStatus)
	}
	if _, err := driverService.FinishLogin(ctx, challengeID, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(credentialPayload)), proof, "test-driver", "192.0.2.55:443"); err == nil {
		t.Fatal("driver login challenge replay was accepted")
	}
}

func testDriverSessionExpiry(t *testing.T, ctx context.Context, repository *OnlineAuthRepository, adminPool *pgxpool.Pool, fixture foundationFixture, deviceID, credentialID uuid.UUID) {
	t.Helper()
	for _, tc := range []struct {
		name         string
		absolutePast bool
	}{
		{name: "idle"},
		{name: "absolute", absolutePast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := []byte(uuid.NewString())
			sessionID := uuid.New()
			var epoch int64
			if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserADriver).Scan(&epoch); err != nil {
				t.Fatal(err)
			}
			if _, err := adminPool.Exec(ctx, `
				INSERT INTO platform.auth_session(
					id,user_account_id,token_hash,status,created_at,last_seen_at,idle_expires_at,
					absolute_expires_at,session_epoch,context_version,session_policy,
					authentication_level,active_company_id,active_operation_id,
					authenticator_credential_id,device_authorization_id)
				VALUES($1,$2,$3,'ACTIVE',clock_timestamp(),clock_timestamp(),
					clock_timestamp()+interval '1 hour', clock_timestamp()+interval '2 hours',
					$4,0,'DRIVER','PASSWORD',$5,$6,$7,$8)
			`, sessionID, fixture.UserADriver, applicationauth.HashSessionToken(token),
				epoch, fixture.CompanyA, fixture.OperationA, credentialID, deviceID); err != nil {
				t.Fatal(err)
			}
			update := `UPDATE platform.auth_session SET created_at = clock_timestamp() - interval '2 hours', last_seen_at = clock_timestamp() - interval '2 hours', idle_expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`
			if tc.absolutePast {
				update = `UPDATE platform.auth_session SET created_at = clock_timestamp() - interval '3 hours', last_seen_at = clock_timestamp() - interval '3 hours', idle_expires_at = clock_timestamp() - interval '2 hours', absolute_expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`
			}
			if _, err := adminPool.Exec(ctx, update, sessionID); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(token)); err == nil {
				t.Fatal("expired DRIVER session resolved")
			}
			var status string
			if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.auth_session WHERE id = $1`, sessionID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "EXPIRED" {
				t.Fatalf("expired session status=%s", status)
			}
		})
	}
}
