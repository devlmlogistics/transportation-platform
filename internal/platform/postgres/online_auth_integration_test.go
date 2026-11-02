package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationauth "transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/migrations"
	"transportation-platform/internal/platform/testdb"
)

func testOnlineAuth(t *testing.T, ctx context.Context, runtimePool, authPool *pgxpool.Pool, adminDSN string, fixture foundationFixture) {
	t.Helper()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	hash, parameters, err := applicationauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	identifier := "global.admin@example.test"
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.login_identifier (id, user_account_id, normalized_identifier)
		VALUES ($1, $2, $3)
	`, uuid.New(), fixture.UserGlobal, identifier); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform.password_credential (id, user_account_id, purpose, password_hash, kdf_name, kdf_parameters)
		VALUES ($1, $2, 'LOGIN', $3, 'argon2id', $4)
	`, uuid.New(), fixture.UserGlobal, hash, parameters); err != nil {
		t.Fatal(err)
	}
	repository, err := NewOnlineAuthRepository(runtimePool, authPool)
	if err != nil {
		t.Fatal(err)
	}
	service, err := applicationauth.NewOnlineAuthService(repository)
	if err != nil {
		t.Fatal(err)
	}
	webauthnService, err := applicationauth.NewWebAuthnService(repository, applicationauth.WebAuthnConfig{
		RPID: "localhost", RPDisplayName: "Transportation Platform", RPOrigins: []string{"http://localhost:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("webauthn_challenge_read_matches_sql_return_contract", func(t *testing.T) {
		login, err := service.Login(ctx, identifier, "correct horse battery staple", "192.0.2.20:443", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = service.Logout(ctx, login.Token, login.Session.ID) })
		request := applicationauth.WebAuthnChallengeRequest{
			ID:                uuid.New(),
			Purpose:           applicationauth.RegistrationChallenge,
			ChallengeHash:     makeHash(0xa1),
			SessionData:       []byte(`{"challenge":"synthetic"}`),
			SessionDataFormat: "webauthn.v1",
			BindingHash:       makeHash(0xa2),
			ExpiresAt:         time.Now().UTC().Add(5 * time.Minute),
			AuditID:           uuid.New(),
		}
		if err := repository.BeginWebAuthnChallenge(ctx, applicationauth.HashSessionToken(login.Token), request); err != nil {
			t.Fatal(err)
		}
		challenge, err := repository.GetWebAuthnChallenge(ctx, applicationauth.HashSessionToken(login.Token), request.ID, applicationauth.RegistrationChallenge)
		if err != nil {
			t.Fatalf("read WebAuthn challenge: %v", err)
		}
		if challenge.ID != request.ID || string(challenge.SessionData) != string(request.SessionData) || challenge.DeviceRequestID != nil {
			t.Fatalf("unexpected challenge: %+v", challenge)
		}
	})
	t.Run("webauthn_registration_full_flow", func(t *testing.T) {
		testWebAuthnRegistration(t, ctx, repository, service, adminPool)
	})
	t.Run("webauthn_credential_revocation", func(t *testing.T) {
		testWebAuthnCredentialRevocation(t, ctx, repository, authPool, adminPool, fixture)
	})
	t.Run("webauthn_negative_uv_and_signature_assertions", func(t *testing.T) {
		testWebAuthnNegativeAssertions(t, ctx, service, webauthnService, repository, adminPool, fixture)
	})

	t.Run("valid_login_resolves_and_replays_no_secret", func(t *testing.T) {
		result, err := service.Login(ctx, identifier, "correct horse battery staple", "192.0.2.10:443", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Token) != 32 || len(result.CSRFToken) != 32 || result.Session.ID == uuid.Nil {
			t.Fatalf("invalid login result: %+v", result.Session)
		}
		var tokenHash []byte
		if err := adminPool.QueryRow(ctx, `SELECT token_hash FROM platform.auth_session WHERE id = $1`, result.Session.ID).Scan(&tokenHash); err != nil {
			t.Fatal(err)
		}
		if string(tokenHash) == string(result.Token) {
			t.Fatal("raw session token was stored")
		}
		resolved, err := service.ResolveSession(ctx, result.Token)
		if err != nil || resolved.UserAccountID != fixture.UserGlobal || resolved.PersonID != fixture.PersonGlobal {
			t.Fatalf("resolved session = %+v, err=%v", resolved, err)
		}
		if _, err := service.Login(ctx, identifier, "wrong password", "192.0.2.10:443", "test-agent"); !errors.Is(err, applicationauth.ErrAuthenticationFailed) {
			t.Fatalf("invalid password error = %v", err)
		}
		if err := service.Logout(ctx, result.Token, result.Session.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ResolveSession(ctx, result.Token); !errors.Is(err, applicationauth.ErrSessionNotFound) {
			t.Fatalf("revoked session resolved: %v", err)
		}
	})

	t.Run("role_bound_handoff_rejects_arbitrary_session", func(t *testing.T) {
		var rows int
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.issue_auth_session_from_verified_login(
				$1, $2, $3, $4, $5, $6, 'ADMINISTRATIVE', NULL, NULL, NULL, NULL, NULL, ''
			)
		`, makeHash(0x41), uuid.New(), uuid.New(), uuid.New(), makeHash(0x42), makeHash(0x43)).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatal("authentication role issued a session without a verified grant")
		}
		if _, err := runtimePool.Exec(ctx, `
			SELECT platform.issue_auth_session_from_verified_login(
				$1, $2, $3, $4, $5, $6, 'ADMINISTRATIVE', NULL, NULL, NULL, NULL, NULL, ''
			)
		`, makeHash(0x51), uuid.New(), uuid.New(), uuid.New(), makeHash(0x52), makeHash(0x53)); err == nil {
			t.Fatal("runtime role executed the session issuance boundary")
		}
		if _, err := authPool.Exec(ctx, `
			SELECT platform.record_verified_password_login($1, $2, $3, $4, 120)
		`, uuid.New(), makeHash(0x61), uuid.New(), makeHash(0x62)); err == nil {
			t.Fatal("authentication role inserted a verification grant")
		}
		if _, err := authPool.Exec(ctx, `UPDATE platform.user_account SET session_epoch = session_epoch + 1 WHERE id = $1`, fixture.UserGlobal); err == nil {
			t.Fatal("authentication role altered account epoch directly")
		}
		if _, err := runtimePool.Exec(ctx, `SELECT platform.issue_auth_session_from_verified_login($1, $2, $3, $4, $5, $6, 'ADMINISTRATIVE', NULL, NULL, NULL, NULL, NULL, '')`, makeHash(0x71), uuid.New(), uuid.New(), uuid.New(), makeHash(0x72), makeHash(0x73)); err == nil {
			t.Fatal("runtime role issued a session through the auth function")
		}
		if _, err := runtimePool.Exec(ctx, `
			SELECT platform.record_verified_password_login($1, $2, $3, $4, $5, $6)
		`, uuid.New(), makeHash(0x74), fixture.UserGlobal, fixture.PersonGlobal, hash, 120); err == nil {
			t.Fatal("runtime role reused the stored password hash as authentication proof")
		}
		if _, err := runtimePool.Exec(ctx, `
			SELECT platform.create_admin_invitation($1, $2, $3, $4, $5, $6)
		`, fixture.UserGlobal, uuid.New(), makeHash(0x75), "spoof@example.test", "Spoof", 600); err == nil {
			t.Fatal("runtime role invoked the legacy actor-id invitation boundary")
		}
	})

	t.Run("disabled_account_and_expired_credential_fail_closed", func(t *testing.T) {
		if _, err := adminPool.Exec(ctx, `UPDATE platform.user_account SET status = 'DISABLED' WHERE id = $1`, fixture.UserGlobal); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Login(ctx, identifier, "correct horse battery staple", "192.0.2.12:443", "test-agent"); !errors.Is(err, applicationauth.ErrAuthenticationFailed) {
			t.Fatalf("disabled account login error = %v", err)
		}
		if _, err := adminPool.Exec(ctx, `UPDATE platform.user_account SET status = 'ACTIVE' WHERE id = $1`, fixture.UserGlobal); err != nil {
			t.Fatal(err)
		}
		if _, err := adminPool.Exec(ctx, `UPDATE platform.password_credential SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 second' WHERE user_account_id = $1 AND purpose = 'LOGIN'`, fixture.UserGlobal); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Login(ctx, identifier, "correct horse battery staple", "192.0.2.12:443", "test-agent"); !errors.Is(err, applicationauth.ErrAuthenticationFailed) {
			t.Fatalf("expired credential login error = %v", err)
		}
		if _, err := adminPool.Exec(ctx, `UPDATE platform.password_credential SET expires_at = NULL WHERE user_account_id = $1 AND purpose = 'LOGIN'`, fixture.UserGlobal); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invitation_is_single_use_and_does_not_assign_role", func(t *testing.T) {
		adminLogin, err := service.Login(ctx, identifier, "correct horse battery staple", "192.0.2.13:443", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		issued, err := service.IssueInvitation(ctx, adminLogin.Token, "invited.admin@example.test", "Invited Admin", 600)
		if err != nil {
			t.Fatal(err)
		}
		accountID, personID, err := service.AcceptInvitation(ctx, issued.Token, "another correct password")
		if err != nil {
			t.Fatal(err)
		}
		if accountID == uuid.Nil || personID == uuid.Nil {
			t.Fatal("invitation did not create identity")
		}
		if _, _, err := service.AcceptInvitation(ctx, issued.Token, "another correct password"); !errors.Is(err, applicationauth.ErrInvitationInvalid) {
			t.Fatalf("invitation replay error = %v", err)
		}
		var roles int
		if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM platform.role_assignment WHERE person_id = $1`, personID).Scan(&roles); err != nil {
			t.Fatal(err)
		}
		if roles != 0 {
			t.Fatalf("invitation assigned %d roles", roles)
		}
		if err := service.Logout(ctx, adminLogin.Token, adminLogin.Session.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("context_revalidates_membership", func(t *testing.T) {
		result, err := service.Login(ctx, identifier, "correct horse battery staple", "192.0.2.11:443", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.ChangeContext(ctx, result.Token, ptrUUID(fixture.CompanyA), nil); !errors.Is(err, applicationauth.ErrSessionNotFound) {
			t.Fatalf("global identity context result = %v", err)
		}
		if err := service.Logout(ctx, result.Token, result.Session.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("webauthn_sql_boundary_denies_arbitrary_elevation", func(t *testing.T) {
		var runtimeCanExecute bool
		if err := adminPool.QueryRow(ctx, `SELECT has_function_privilege('tp_runtime', $1, 'EXECUTE')`,
			"platform.complete_webauthn_step_up(bytea,uuid,bytea,bytea,bytea,uuid)").Scan(&runtimeCanExecute); err != nil {
			t.Fatal(err)
		}
		if runtimeCanExecute {
			t.Fatal("tp_runtime can execute WebAuthn step-up boundary")
		}
		var elevated bool
		if err := authPool.QueryRow(ctx, `SELECT platform.complete_webauthn_step_up($1,$2,$3,$4,$5,$6) IS NOT NULL`,
			makeHash(0x91), uuid.New(), makeHash(0x92), makeHash(0x93), makeHash(0x94), uuid.New()).Scan(&elevated); err != nil {
			t.Fatal(err)
		}
		if elevated {
			t.Fatal("authentication role elevated a session without a live session/challenge")
		}
		if _, err := runtimePool.Exec(ctx, `SELECT platform.begin_webauthn_challenge($1,$2,'STEP_UP',$3,$4,'webauthn.v1',$5,clock_timestamp()+interval '5 minutes',NULL,$6)`,
			makeHash(0x95), uuid.New(), makeHash(0x96), []byte("session-data"), makeHash(0x97), uuid.New()); err == nil {
			t.Fatal("runtime role created an arbitrary WebAuthn challenge")
		}
	})

	t.Run("driver_mobile_webauthn_session_is_device_scoped", func(t *testing.T) {
		testDriverMobileAuth(t, ctx, repository, webauthnService, runtimePool, adminPool, fixture)
	})
}

func TestPostgres16OnlineBootstrap(t *testing.T) {
	ctx := context.Background()
	root := filepath.Clean(filepath.Join(migrationDirectory(t), "..", ".."))
	harness, err := testdb.Start(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = harness.Close(context.Background()) })
	migrator, err := migrations.Open(ctx, harness.MigratorDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	if err := migrations.Up(ctx, migrator, migrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	bootstrapPool, err := pgxpool.New(ctx, harness.BootstrapDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrapPool.Close()
	runtimePool, err := pgxpool.New(ctx, harness.RuntimeDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	authPool, err := pgxpool.New(ctx, harness.AuthDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer authPool.Close()
	adminPool, err := pgxpool.New(ctx, harness.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	repository, err := NewBootstrapAuthRepository(bootstrapPool)
	if err != nil {
		t.Fatal(err)
	}
	service, err := applicationauth.NewOnlineAuthService(repository)
	if err != nil {
		t.Fatal(err)
	}
	var bootstrapSuperuser, bootstrapBypassRLS bool
	if err := adminPool.QueryRow(ctx, `
		SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'tp_bootstrap_runtime'
	`).Scan(&bootstrapSuperuser, &bootstrapBypassRLS); err != nil {
		t.Fatal(err)
	}
	if bootstrapSuperuser || bootstrapBypassRLS {
		t.Fatalf("bootstrap role is overprivileged superuser=%v bypassrls=%v", bootstrapSuperuser, bootstrapBypassRLS)
	}
	for _, table := range []string{"bootstrap_control", "auth_password_verification", "auth_session"} {
		var allowed bool
		if err := adminPool.QueryRow(ctx, `SELECT has_table_privilege('tp_bootstrap_runtime', $1, 'SELECT')`, "platform."+table).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Fatalf("bootstrap role unexpectedly has SELECT on platform.%s", table)
		}
	}
	for _, signature := range []string{
		"platform.prepare_bootstrap_request(uuid,bytea,bytea,text,text,text,jsonb,integer)",
		"platform.complete_bootstrap_request(bytea,bytea,uuid,uuid,uuid,uuid,uuid)",
	} {
		var allowed bool
		if err := adminPool.QueryRow(ctx, `SELECT has_function_privilege('tp_bootstrap_runtime', $1, 'EXECUTE')`, signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("bootstrap role lacks EXECUTE on %s", signature)
		}
	}
	// The dedicated bootstrap role is the only role allowed to reach these
	// functions. The ordinary runtime roles cannot prepare or complete a
	// request even when supplied with arbitrary UUIDs and hashes.
	if _, err := runtimePool.Exec(ctx, `
		SELECT platform.prepare_bootstrap_request($1, $2, $3, $4, $5, $6, $7, 600)
	`, uuid.New(), makeHash(0x81), makeHash(0x82), "runtime@example.test", "Runtime", "argon2id$invalid", `{}`); err == nil {
		t.Fatal("runtime role prepared a bootstrap request")
	}
	if _, err := authPool.Exec(ctx, `
		SELECT platform.complete_bootstrap_request($1, $2, $3, $4, $5, $6, $7)
	`, makeHash(0x81), makeHash(0x82), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("authentication role completed a bootstrap request")
	}

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, identifier := range []string{"first.admin@example.test", "second.admin@example.test"} {
		wg.Add(1)
		go func(loginIdentifier string) {
			defer wg.Done()
			_, bootstrapErr := service.Bootstrap(ctx, "bootstrap-secret-from-test", loginIdentifier, "First Administrator", "bootstrap password secure")
			results <- bootstrapErr
		}(identifier)
	}
	wg.Wait()
	close(results)
	var successes int
	for bootstrapErr := range results {
		if bootstrapErr == nil {
			successes++
		} else if !errors.Is(bootstrapErr, applicationauth.ErrBootstrapUnavailable) {
			t.Fatalf("concurrent bootstrap error = %v", bootstrapErr)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent bootstrap successes = %d, want 1", successes)
	}

	var accountID uuid.UUID
	if err := adminPool.QueryRow(ctx, `
		SELECT completed_user_account_id FROM platform.bootstrap_control WHERE id = true
	`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if accountID == uuid.Nil {
		t.Fatal("bootstrap control did not record completed account")
	}
	var globals int
	if err := adminPool.QueryRow(ctx, `
		SELECT count(*) FROM platform.role_assignment
		WHERE role_code = 'GLOBAL_ADMIN' AND lifecycle_status = 'ACTIVE'
	`).Scan(&globals); err != nil {
		t.Fatal(err)
	}
	if globals != 1 {
		t.Fatalf("global admins = %d, want 1", globals)
	}
	if _, err := adminPool.Exec(ctx, `
		UPDATE platform.role_assignment
		SET lifecycle_status = 'REVOKED', revoked_at = clock_timestamp()
		WHERE role_code = 'GLOBAL_ADMIN'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.user_account SET status = 'DISABLED' WHERE id = $1`, accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(ctx, "bootstrap-secret-from-test", "third.admin@example.test", "Third Administrator", "bootstrap password secure"); !errors.Is(err, applicationauth.ErrBootstrapUnavailable) {
		t.Fatalf("bootstrap after global admin disable error = %v", err)
	}
}

func makeHash(value byte) []byte {
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = value
	}
	return hash
}
