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
	"path/filepath"
	"sync"
	"testing"

	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	auth "transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/migrations"
	"transportation-platform/internal/platform/testdb"
)

func TestPostgres16DriverManagement(t *testing.T) {
	ctx := context.Background()
	h, err := testdb.Start(ctx, filepath.Clean(filepath.Join(migrationDirectory(t), "..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	db, err := migrations.Open(ctx, h.MigratorDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	prepareSchema(t, ctx, db, h.AdminDSN)
	pool := func(dsn string) *pgxpool.Pool {
		p, e := pgxpool.New(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	runtime, ap, driver, admin := pool(h.RuntimeDSN), pool(h.AuthDSN), pool(h.DriverAuthDSN), pool(h.AdminDSN)
	f := seedFoundation(t, ctx, runtime)
	repo, err := NewOnlineAuthRepository(runtime, ap, driver)
	if err != nil {
		t.Fatal(err)
	}
	service := auth.DriverManagementService{Store: repo}
	policy, _ := auth.PolicyFor(auth.AdministrativeSession)
	token := makeHash(0xD4)
	sess, err := createSyntheticSession(ctx, admin, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: f.UserASupervisor, TokenHash: auth.HashSessionToken(token), Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	call := func(company uuid.UUID, action string, input auth.DriverManagementInput, key uuid.UUID) auth.DriverManagementResult {
		r, e := service.Manage(ctx, token, company, action, input, key)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	if r := call(f.CompanyA, "list", auth.DriverManagementInput{}, uuid.Nil); r.Status != "FORBIDDEN" {
		t.Fatalf("implicit supervisor manage: %+v", r)
	}
	grant := uuid.New()
	if _, err := admin.Exec(ctx, `INSERT INTO platform.permission_grant(id,person_id,permission_code,scope_type,company_id) VALUES($1,$2,'driver.manage','COMPANY',$3)`, grant, f.PersonASupervisor, f.CompanyA); err != nil {
		t.Fatal(err)
	}
	input := auth.DriverManagementInput{GivenName: "New", FamilyName: "Driver", Identifier: "new-driver@example.test", Phone: "+56912345678", OperationIDs: []uuid.UUID{f.OperationA}}
	if r := call(f.CompanyB, "create", input, uuid.New()); r.Status != "FORBIDDEN" {
		t.Fatalf("cross-company create: %+v", r)
	}
	bad := input
	bad.OperationIDs = []uuid.UUID{f.OperationB}
	if r := call(f.CompanyA, "create", bad, uuid.New()); r.Status != "FORBIDDEN" {
		t.Fatalf("cross-operation create: %+v", r)
	}
	key := uuid.New()
	created := call(f.CompanyA, "create", input, key)
	if created.Status != "CREATED" || created.PersonID == nil {
		t.Fatalf("new driver: %+v", created)
	}
	if r := call(f.CompanyA, "create", input, key); r.Status != "CREATED" || *r.PersonID != *created.PersonID {
		t.Fatal("idempotent retry changed result")
	}
	if r := call(f.CompanyA, "create", input, uuid.New()); r.Status != "CONFLICT" {
		t.Fatal("duplicate driver accepted")
	}
	var accounts int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM platform.login_identifier WHERE normalized_identifier=$1`, input.Identifier).Scan(&accounts); err != nil || accounts != 1 {
		t.Fatal("duplicate global identity")
	}
	var newAccount uuid.UUID
	if err := admin.QueryRow(ctx, `SELECT user_account_id FROM platform.login_identifier WHERE normalized_identifier=$1`, input.Identifier).Scan(&newAccount); err != nil {
		t.Fatal(err)
	}
	// Driver management alone does not grant invitation/device capabilities.
	if _, err := repo.ListDriverEnrollmentCandidates(ctx, auth.HashSessionToken(token), f.CompanyA, nil); err == nil {
		t.Fatal("manage permission implied device permission")
	}
	// Existing operational supervisor permission remains independent.
	candidates, err := repo.ListDriverEnrollmentCandidates(ctx, auth.HashSessionToken(token), f.CompanyA, &f.OperationA)
	if err != nil || len(candidates) != 1 || candidates[0].PersonID != *created.PersonID {
		t.Fatalf("new driver not eligible: %+v %v", candidates, err)
	}
	invitations, _ := auth.NewDriverEnrollmentInvitationService(repo)
	supervisorSession := auth.AuthenticatedSession{ID: sess.ID, UserAccountID: f.UserASupervisor, PersonID: f.PersonASupervisor, SessionPolicy: auth.AdministrativeSession}
	invite, err := invitations.Create(ctx, token, supervisorSession, auth.DriverEnrollmentInvitationInput{PersonID: *created.PersonID, CompanyID: f.CompanyA, CompanyOperationID: &f.OperationA, ExpiresSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	install := newInvitationInstallationKey(t)
	installationID := uuid.New()
	activation, err := invitations.Consume(ctx, auth.DriverEnrollmentConsumeInput{Token: invite.Token, ConfirmationCode: invite.Code, InstallationID: installationID, InstallationPublicKey: elliptic.Marshal(elliptic.P256(), install.PublicKey.X, install.PublicKey.Y), KeyAlgorithm: "ECDSA_P256_SHA256"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repo.ResolveHTTPSession(ctx, auth.HashSessionToken(activation.Token))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ApproveDeviceEnrollmentRequest(ctx, auth.HashSessionToken(token), activation.Request.ID, "synthetic approval"); err != nil {
		t.Fatal(err)
	}
	web, err := auth.NewWebAuthnService(repo, auth.WebAuthnConfig{RPID: "localhost", RPDisplayName: "Test", RPOrigins: []string{"http://localhost:8080"}})
	if err != nil {
		t.Fatal(err)
	}
	challengeID, _, err := web.BeginRegistration(ctx, activation.Token, resolved, "new driver", &activation.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := repo.GetWebAuthnChallenge(ctx, auth.HashSessionToken(activation.Token), challengeID, auth.RegistrationChallenge)
	if err != nil {
		t.Fatal(err)
	}
	credential := newTestAssertionCredential(t)
	payload := validRegistrationPayloadForCredential(t, challenge.SessionData, credential)
	if err := web.FinishRegistration(ctx, activation.Token, resolved, challengeID, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload)), "new driver"); err != nil {
		t.Fatal(err)
	}
	mobile, err := auth.NewDriverMobileService(repo, web.Engine)
	if err != nil {
		t.Fatal(err)
	}
	loginChallengeID, _, err := mobile.BeginLogin(ctx, input.Identifier, installationID)
	if err != nil {
		t.Fatal(err)
	}
	loginChallenge, err := repo.GetDriverLoginChallenge(ctx, loginChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	var ceremony webauthn.SessionData
	if err := json.Unmarshal(loginChallenge.SessionData, &ceremony); err != nil {
		t.Fatal(err)
	}
	assertion := signedAssertion(t, ceremony, credential, true, true)
	proofMessage := append(append([]byte("tp-driver-possession\x00"), loginChallenge.ChallengeHash...), installationID[:]...)
	digest := sha256.Sum256(proofMessage)
	r, s, err := ecdsa.Sign(rand.Reader, install, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	proof := make([]byte, 64)
	r.FillBytes(proof[:32])
	s.FillBytes(proof[32:])
	loggedIn, err := mobile.FinishLogin(ctx, loginChallengeID, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(assertion)), proof, "test", "192.0.2.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if loggedIn.Session.UserAccountID != newAccount {
		t.Fatal("new driver recurring login identity mismatch")
	}
	// Existing global identity outside A produces a review request, never an attachment or personal data.
	if _, err := admin.Exec(ctx, `INSERT INTO platform.login_identifier(id,user_account_id,normalized_identifier) VALUES($1,$2,'existing-beta@example.test')`, uuid.New(), f.UserBAdmin); err != nil {
		t.Fatal(err)
	}
	existing := input
	existing.Identifier = "existing-beta@example.test"
	review := call(f.CompanyA, "create", existing, uuid.New())
	if review.Status != "REVIEW_REQUIRED" || review.PersonID != nil || review.RequestID == nil {
		t.Fatalf("unsafe existing identity handling: %+v", review)
	}
	var linked bool
	if err := ap.QueryRow(ctx, `SELECT platform.approve_driver_company_link($1,$2)`, auth.HashSessionToken(token), *review.RequestID).Scan(&linked); err != nil || linked {
		t.Fatal("tenant approved global link")
	}
	if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM platform.company_membership WHERE company_id=$1 AND person_id=$2)`, f.CompanyA, f.PersonBAdmin).Scan(&linked); err != nil || linked {
		t.Fatal("automatic cross-tenant link")
	}
	platformToken := makeHash(0xD7)
	platformSession, err := createSyntheticSession(ctx, admin, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: f.UserGlobal, TokenHash: auth.HashSessionToken(platformToken), Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if err := ap.QueryRow(ctx, `SELECT platform.approve_driver_company_link($1,$2)`, auth.HashSessionToken(platformToken), *review.RequestID).Scan(&linked); err != nil || linked {
		t.Fatal("platform linked without step-up")
	}
	platformCredential := newTestAssertionCredential(t)
	if _, err := admin.Exec(ctx, `INSERT INTO platform.authenticator_credential(id,user_account_id,credential_id,public_key,sign_count,status,metadata) VALUES($1,$2,$3,$4,0,'ACTIVE','{}')`, platformCredential.RecordID, f.UserGlobal, platformCredential.CredentialID, platformCredential.PublicKey); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE platform.auth_session SET authentication_level='WEBAUTHN_STEP_UP',step_up_expires_at=clock_timestamp()+interval '5 minutes',step_up_credential_id=$1 WHERE id=$2`, platformCredential.RecordID, platformSession.ID); err != nil {
		t.Fatal(err)
	}
	if err := ap.QueryRow(ctx, `SELECT platform.approve_driver_company_link($1,$2)`, auth.HashSessionToken(platformToken), *review.RequestID).Scan(&linked); err != nil || !linked {
		t.Fatalf("platform reviewed link failed: %v", err)
	}
	var globalName string
	if err := admin.QueryRow(ctx, `SELECT legal_name FROM platform.person WHERE id=$1`, f.PersonBAdmin).Scan(&globalName); err != nil || globalName != "Beta Admin" {
		t.Fatal("link modified global identity")
	}
	// Concurrency with independent request keys still creates only one identity.
	concurrent := input
	concurrent.Identifier = "concurrent-new@example.test"
	outcomes := make(chan auth.DriverManagementResult, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := service.Manage(ctx, token, f.CompanyA, "create", concurrent, uuid.New())
			if e != nil {
				t.Error(e)
			}
			outcomes <- r
		}()
	}
	wg.Wait()
	close(outcomes)
	successes := 0
	for r := range outcomes {
		if r.Status == "CREATED" {
			successes++
		} else if r.Status != "CONFLICT" {
			t.Fatalf("concurrent create: %+v", r)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent duplication")
	}
	// Establish an independent B relationship/session in the synthetic harness.
	if _, err := admin.Exec(ctx, `INSERT INTO platform.company_membership(id,company_id,person_id,status) VALUES($1,$2,$3,'ACTIVE')`, uuid.New(), f.CompanyB, *created.PersonID); err != nil {
		t.Fatal(err)
	}
	var authA uuid.UUID
	if err := admin.QueryRow(ctx, `SELECT id FROM platform.device_authorization WHERE user_account_id=$1`, newAccount).Scan(&authA); err != nil {
		t.Fatal(err)
	}
	authB := uuid.New()
	if _, err := admin.Exec(ctx, `INSERT INTO platform.device_authorization(id,installation_id,user_account_id,company_id,installation_public_key,key_algorithm,status) VALUES($1,$2,$3,$4,$5,'ES256','ACTIVE')`, authB, uuid.New(), newAccount, f.CompanyB, makeHash(0xA1)); err != nil {
		t.Fatal(err)
	}
	driverPolicy, _ := auth.PolicyFor(auth.DriverSession)
	sessionHashes := [][]byte{makeHash(0xD5), makeHash(0xD6)}
	for i, id := range []uuid.UUID{authA, authB} {
		s, e := createSyntheticSession(ctx, admin, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: newAccount, TokenHash: sessionHashes[i], Policy: driverPolicy})
		if e != nil {
			t.Fatal(e)
		}
		if _, e := admin.Exec(ctx, `UPDATE platform.auth_session SET device_authorization_id=$1 WHERE id=$2`, id, s.ID); e != nil {
			t.Fatal(e)
		}
	}
	update := auth.DriverManagementInput{PersonID: *created.PersonID, Version: 1, Status: "SUSPENDED", OperationIDs: input.OperationIDs}
	if r := call(f.CompanyA, "update", update, uuid.New()); r.Status != "UPDATED" {
		t.Fatalf("suspend: %+v", r)
	}
	if _, err := repo.ResolveHTTPSession(ctx, sessionHashes[0]); err == nil {
		t.Fatal("A session survived suspension")
	}
	if _, err := repo.ResolveHTTPSession(ctx, auth.HashSessionToken(loggedIn.Token)); err == nil {
		t.Fatal("real driver session survived suspension")
	}
	// Options are not an access decision: use a valid assertion/installation
	// signature and verify that the final persistent session boundary rejects it.
	if nextID, _, beginErr := mobile.BeginLogin(ctx, input.Identifier, installationID); beginErr == nil {
		nextChallenge, e := repo.GetDriverLoginChallenge(ctx, nextID)
		if e != nil {
			t.Fatal(e)
		}
		var nextCeremony webauthn.SessionData
		if e := json.Unmarshal(nextChallenge.SessionData, &nextCeremony); e != nil {
			t.Fatal(e)
		}
		nextAssertion := signedAssertion(t, nextCeremony, credential, true, true)
		nextDigest := sha256.Sum256(append(append([]byte("tp-driver-possession\x00"), nextChallenge.ChallengeHash...), installationID[:]...))
		r, s, e := ecdsa.Sign(rand.Reader, install, nextDigest[:])
		if e != nil {
			t.Fatal(e)
		}
		nextProof := make([]byte, 64)
		r.FillBytes(nextProof[:32])
		s.FillBytes(nextProof[32:])
		if _, e := mobile.FinishLogin(ctx, nextID, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(nextAssertion)), nextProof, "test", "192.0.2.1:1234"); e == nil {
			t.Fatal("suspended driver authenticated with valid cryptographic proofs")
		}
	}
	if _, err := repo.ResolveHTTPSession(ctx, sessionHashes[1]); err != nil {
		t.Fatalf("B access affected by A suspension: %v", err)
	}
	if r := call(f.CompanyA, "update", update, uuid.New()); r.Status != "CONFLICT" {
		t.Fatal("stale version overwrote update")
	}
	rows := call(f.CompanyA, "list", auth.DriverManagementInput{Search: "New Driver"}, uuid.Nil)
	var listed []map[string]any
	if err := json.Unmarshal(rows.Drivers, &listed); err != nil || len(listed) != 3 {
		t.Fatalf("driver search: %s err=%v", rows.Drivers, err)
	}
	var success, denied int
	if err := admin.QueryRow(ctx, `SELECT count(*) FILTER(WHERE outcome='SUCCESS'),count(*) FILTER(WHERE outcome='DENIED') FROM platform.security_audit_event WHERE event_type LIKE 'DRIVER_MANAGEMENT%'`).Scan(&success, &denied); err != nil || success < 3 || denied < 3 {
		t.Fatalf("audit success=%d denied=%d err=%v", success, denied, err)
	}
	for _, p := range []*pgxpool.Pool{runtime, ap, driver} {
		var allowed bool
		if err := p.QueryRow(ctx, `SELECT has_table_privilege(current_user,'platform.company_driver_contact','INSERT') OR has_function_privilege(current_user,'platform.set_driver_company_relationship(uuid,uuid,uuid,uuid[],text)','EXECUTE')`).Scan(&allowed); err != nil || allowed {
			t.Fatal("runtime obtained direct provisioning privileges")
		}
	}
	var throttled bool
	for range 25 {
		result := call(f.CompanyA, "create", input, uuid.New())
		if result.Status == "RATE_LIMITED" {
			throttled = true
			break
		}
		if result.Status != "CONFLICT" {
			t.Fatalf("unexpected duplicate result: %+v", result)
		}
	}
	if !throttled {
		t.Fatal("provisioning limit was not enforced")
	}
}
