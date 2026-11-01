package postgres

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/tenancy"
)

func testAuthFoundation(t *testing.T, ctx context.Context, runtimePool, authPool *pgxpool.Pool, adminDSN string, fixture foundationFixture) {
	t.Helper()
	repository := newAuthRepository(authPool)
	policy, err := auth.PolicyFor(auth.AdministrativeSession)
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()

	t.Run("session_hash_resolution_and_individual_revoke", func(t *testing.T) {
		rawToken := bytes.Repeat([]byte{0x11}, 32)
		tokenHash := HashSessionToken(rawToken)
		session, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{
			ID:            uuid.New(),
			UserAccountID: fixture.UserAAdmin,
			TokenHash:     tokenHash,
			Policy:        policy,
		})
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := repository.ResolveSession(ctx, tokenHash)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.ID != session.ID || resolved.PersonID != fixture.PersonAAdmin || resolved.Status != "ACTIVE" {
			t.Fatalf("unexpected resolved session: %+v", resolved)
		}
		if _, err := repository.ResolveSession(ctx, bytes.Repeat([]byte{0x22}, 32)); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected unknown token to return no rows, got %v", err)
		}
		if err := repository.RevokeSession(ctx, session.ID, tokenHash, "test_revoke"); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ResolveSession(ctx, tokenHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected revoked token to return no rows, got %v", err)
		}
	})

	t.Run("global_epoch_revocation", func(t *testing.T) {
		firstHash := bytes.Repeat([]byte{0x31}, 32)
		secondHash := bytes.Repeat([]byte{0x32}, 32)
		if _, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: firstHash, Policy: policy}); err != nil {
			t.Fatal(err)
		}
		if _, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: secondHash, Policy: policy}); err != nil {
			t.Fatal(err)
		}
		if err := repository.RevokeAllSessions(ctx, firstHash, "test_revoke_all"); err != nil {
			t.Fatal(err)
		}
		for name, hash := range map[string][]byte{"first": firstHash, "second": secondHash} {
			if _, err := repository.ResolveSession(ctx, hash); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("%s session survived global revoke: %v", name, err)
			}
		}
		var epoch int64
		withAccess(t, ctx, runtimePool, tenancy.Platform(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&epoch)
		})
		if epoch != 1 {
			t.Fatalf("expected session epoch 1, got %d", epoch)
		}
		thirdHash := bytes.Repeat([]byte{0x33}, 32)
		created, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: thirdHash, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		if created.SessionEpoch != 1 {
			t.Fatalf("new session did not capture incremented epoch: %+v", created)
		}
	})

	t.Run("expired_session_is_not_resolved", func(t *testing.T) {
		expiredHash := bytes.Repeat([]byte{0x41}, 32)
		shortPolicy := auth.SessionPolicy{IdleTTL: time.Minute, AbsoluteTTL: 2 * time.Minute}
		created, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: expiredHash, Policy: shortPolicy})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.auth_session
			SET idle_expires_at = created_at + interval '1 millisecond',
			    absolute_expires_at = created_at + interval '1 hour'
			WHERE id = $1
		`, created.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ResolveSession(ctx, expiredHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected expired session to be rejected, got %v", err)
		}
	})

	t.Run("challenge_consumption_and_replay", func(t *testing.T) {
		challengeID := uuid.New()
		challengeHash := bytes.Repeat([]byte{0x51}, 32)
		bindingHash := bytes.Repeat([]byte{0x52}, 32)
		if err := repository.CreateChallenge(ctx, auth.CreateChallengeRequest{
			ID:                challengeID,
			Purpose:           auth.LoginChallenge,
			UserAccountID:     uuidPointer(fixture.UserAAdmin),
			ChallengeHash:     challengeHash,
			SessionData:       []byte(`{"challenge":"synthetic"}`),
			SessionDataFormat: "json",
			BindingHash:       bindingHash,
			ExpiresAt:         time.Now().UTC().Add(5 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		consumed, err := repository.ConsumeChallenge(ctx, challengeID, auth.LoginChallenge, challengeHash, bindingHash)
		if err != nil {
			t.Fatal(err)
		}
		if consumed.ID != challengeID || !bytes.Equal(consumed.SessionData, []byte(`{"challenge":"synthetic"}`)) {
			t.Fatalf("unexpected consumed challenge: %+v", consumed)
		}
		if _, err := repository.ConsumeChallenge(ctx, challengeID, auth.LoginChallenge, challengeHash, bindingHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected replay to fail, got %v", err)
		}
	})

	t.Run("challenge_concurrent_consume_exactly_once", func(t *testing.T) {
		challengeID := uuid.New()
		challengeHash := bytes.Repeat([]byte{0x61}, 32)
		bindingHash := bytes.Repeat([]byte{0x62}, 32)
		if err := repository.CreateChallenge(ctx, auth.CreateChallengeRequest{
			ID:                challengeID,
			Purpose:           auth.StepUpChallenge,
			UserAccountID:     uuidPointer(fixture.UserAAdmin),
			ChallengeHash:     challengeHash,
			SessionData:       []byte(`{"challenge":"concurrent"}`),
			SessionDataFormat: "json",
			BindingHash:       bindingHash,
			ExpiresAt:         time.Now().UTC().Add(5 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := repository.ConsumeChallenge(ctx, challengeID, auth.StepUpChallenge, challengeHash, bindingHash)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		var successes, noRows int
		for err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, pgx.ErrNoRows):
				noRows++
			default:
				t.Fatalf("unexpected concurrent consume error: %v", err)
			}
		}
		if successes != 1 || noRows != 1 {
			t.Fatalf("expected one successful consume and one replay rejection, successes=%d noRows=%d", successes, noRows)
		}
	})

	t.Run("expired_challenge_is_not_consumed", func(t *testing.T) {
		challengeID := uuid.New()
		challengeHash := bytes.Repeat([]byte{0x71}, 32)
		bindingHash := bytes.Repeat([]byte{0x72}, 32)
		if err := repository.CreateChallenge(ctx, auth.CreateChallengeRequest{
			ID:                challengeID,
			Purpose:           auth.RecoveryChallenge,
			UserAccountID:     uuidPointer(fixture.UserAAdmin),
			ChallengeHash:     challengeHash,
			SessionData:       []byte(`{"challenge":"expired"}`),
			SessionDataFormat: "json",
			BindingHash:       bindingHash,
			ExpiresAt:         time.Now().UTC().Add(time.Second),
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond)
		if _, err := repository.ConsumeChallenge(ctx, challengeID, auth.RecoveryChallenge, challengeHash, bindingHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected expired challenge to fail, got %v", err)
		}
	})

	t.Run("runtime_cannot_select_global_auth_rows", func(t *testing.T) {
		for _, table := range []string{"auth_session", "auth_challenge", "authenticator_credential", "password_credential", "recovery_request", "bootstrap_control", "auth_password_verification"} {
			err := withAccessReturningError(ctx, runtimePool, tenancy.Platform(), func(tx pgx.Tx) error {
				var count int
				return tx.QueryRow(ctx, "SELECT count(*) FROM platform."+table).Scan(&count)
			})
			if err == nil {
				t.Fatalf("tp_runtime unexpectedly selected platform.%s", table)
			}
		}
	})

	t.Run("runtime_and_auth_role_minimum_privilege", func(t *testing.T) {
		expectedAuthExecution := map[string]bool{
			"platform.create_auth_session(uuid,uuid,uuid,bytea,bytea,bigint,bigint,uuid,uuid,uuid,uuid,bytea,text)": false,
			"platform.create_auth_challenge(uuid,uuid,text,uuid,uuid,bytea,bytea,text,bytea,timestamptz,bytea)":     true,
			"platform.consume_auth_challenge(uuid,uuid,text,bytea,bytea)":                                           true,
			"platform.revoke_auth_session(uuid,uuid,bytea,text)":                                                    true,
			"platform.revoke_all_auth_sessions(uuid,bytea,text)":                                                    true,
			"platform.record_auth_security_audit(uuid,text,text,uuid,uuid,uuid,uuid,text,text,jsonb)":               false,
		}
		for roleName, rolePool := range map[string]*pgxpool.Pool{
			"tp_runtime":      runtimePool,
			"tp_auth_runtime": authPool,
		} {
			for _, table := range []string{
				"auth_session", "auth_challenge", "authenticator_credential",
				"password_credential", "recovery_request", "device_authorization",
				"device_enrollment_request", "security_audit_event", "bootstrap_control",
				"auth_password_verification",
			} {
				var allowed bool
				if err := rolePool.QueryRow(ctx, `
					SELECT has_table_privilege(current_user, $1, 'SELECT')
				`, "platform."+table).Scan(&allowed); err != nil {
					t.Fatal(err)
				}
				if allowed {
					t.Fatalf("%s unexpectedly has SELECT privilege on platform.%s", roleName, table)
				}
			}
			for _, signature := range []string{
				"platform.create_auth_session(uuid,uuid,uuid,bytea,bytea,bigint,bigint,uuid,uuid,uuid,uuid,bytea,text)",
				"platform.create_auth_challenge(uuid,uuid,text,uuid,uuid,bytea,bytea,text,bytea,timestamptz,bytea)",
				"platform.consume_auth_challenge(uuid,uuid,text,bytea,bytea)",
				"platform.revoke_auth_session(uuid,uuid,bytea,text)",
				"platform.revoke_all_auth_sessions(uuid,bytea,text)",
				"platform.record_auth_security_audit(uuid,text,text,uuid,uuid,uuid,uuid,text,text,jsonb)",
			} {
				var allowed bool
				if err := rolePool.QueryRow(ctx, `
					SELECT has_function_privilege(current_user, $1, 'EXECUTE')
				`, signature).Scan(&allowed); err != nil {
					t.Fatal(err)
				}
				if roleName == "tp_runtime" && allowed {
					t.Fatalf("tp_runtime unexpectedly has EXECUTE on %s", signature)
				}
				if roleName == "tp_auth_runtime" && allowed != expectedAuthExecution[signature] {
					t.Fatalf("tp_auth_runtime EXECUTE mismatch on %s: got %v want %v", signature, allowed, expectedAuthExecution[signature])
				}
			}
		}

		expectDenied := func(roleName string, rolePool *pgxpool.Pool, statement string, args ...any) {
			t.Helper()
			err := withAccessReturningError(ctx, rolePool, tenancy.Platform(), func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, statement, args...)
				return err
			})
			if err == nil {
				t.Fatalf("%s unexpectedly executed %s", roleName, statement)
			}
		}

		for roleName, rolePool := range map[string]*pgxpool.Pool{
			"tp_runtime":      runtimePool,
			"tp_auth_runtime": authPool,
		} {
			expectDenied(roleName, rolePool, `
				INSERT INTO platform.auth_session (
					id, user_account_id, token_hash, status, created_at,
					last_seen_at, idle_expires_at, absolute_expires_at, session_epoch
				) VALUES ($1, $2, $3, 'ACTIVE', now(), now(),
				          now() + interval '1 hour', now() + interval '2 hours', 0)
			`, uuid.New(), fixture.UserAAdmin, bytes.Repeat([]byte{0x91}, 32))
			expectDenied(roleName, rolePool, `
			SELECT platform.create_auth_session(
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
			)
		`, uuid.New(), uuid.New(), fixture.UserAAdmin, bytes.Repeat([]byte{0x93}, 32), nil,
				int64(60), int64(120), nil, nil, nil, nil, nil, "")
			expectDenied(roleName, rolePool, `UPDATE platform.device_authorization SET status = 'ACTIVE' WHERE false`)
			expectDenied(roleName, rolePool, `UPDATE platform.device_enrollment_request SET status = 'APPROVED' WHERE false`)
		}

		expectDenied("tp_runtime", runtimePool, `
			UPDATE platform.user_account
			SET session_epoch = session_epoch + 1
			WHERE id = $1
		`, fixture.UserAAdmin)
		expectDenied("tp_runtime", runtimePool, `
			SELECT platform.revoke_auth_session($1, $2, $3, 'unauthorized')
		`, uuid.New(), uuid.New(), bytes.Repeat([]byte{0x92}, 32))
		if _, err := repository.CreateSession(ctx, auth.CreateSessionRequest{
			ID:            uuid.New(),
			UserAccountID: fixture.UserAAdmin,
			TokenHash:     bytes.Repeat([]byte{0x94}, 32),
			Policy:        policy,
		}); err == nil {
			t.Fatal("authentication repository emitted a session without a credential boundary")
		}
		if err := repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{ID: uuid.New(), EventType: "UNTRUSTED", Outcome: "SUCCESS"}); !errors.Is(err, ErrSecurityAuditBoundaryClosed) {
			t.Fatalf("generic audit writer was not closed: %v", err)
		}
	})

	t.Run("self_revocation_requires_matching_token_proof", func(t *testing.T) {
		firstHash := bytes.Repeat([]byte{0xa1}, 32)
		secondHash := bytes.Repeat([]byte{0xa2}, 32)
		first, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: firstHash, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		second, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: secondHash, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		var changed bool
		if err := authPool.QueryRow(ctx, `
			SELECT platform.revoke_auth_session($1, $2, $3, 'wrong-proof')
		`, second.ID, uuid.New(), firstHash).Scan(&changed); err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Fatal("a token hash revoked a different session")
		}
		if err := repository.RevokeSession(ctx, first.ID, firstHash, "self-revoke"); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ResolveSession(ctx, firstHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("self-revoked session still resolved: %v", err)
		}
		if _, err := repository.ResolveSession(ctx, secondHash); err != nil {
			t.Fatalf("different session was affected by self-revocation: %v", err)
		}
	})

	t.Run("global_revoke_rejects_expired_token", func(t *testing.T) {
		cases := []struct {
			name     string
			hashByte byte
			absolute bool
		}{
			{name: "idle_expired", hashByte: 0xb1},
			{name: "absolute_expired", hashByte: 0xb2, absolute: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				hash := bytes.Repeat([]byte{tc.hashByte}, 32)
				session, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{
					ID:            uuid.New(),
					UserAccountID: fixture.UserAAdmin,
					TokenHash:     hash,
					Policy:        policy,
				})
				if err != nil {
					t.Fatal(err)
				}
				var beforeEpoch int64
				if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&beforeEpoch); err != nil {
					t.Fatal(err)
				}
				if _, err := adminPool.Exec(ctx, `
					UPDATE platform.auth_session
					SET created_at = clock_timestamp() - interval '2 hours',
					    last_seen_at = clock_timestamp() - interval '2 hours',
					    idle_expires_at = clock_timestamp() - interval '2 seconds',
					    absolute_expires_at = CASE WHEN $2 THEN clock_timestamp() - interval '1 second' ELSE clock_timestamp() + interval '2 hours' END
					WHERE id = $1
				`, session.ID, tc.absolute); err != nil {
					t.Fatal(err)
				}
				var createdAt, idleExpiresAt, absoluteExpiresAt time.Time
				if err := adminPool.QueryRow(ctx, `
					SELECT created_at, idle_expires_at, absolute_expires_at
					FROM platform.auth_session WHERE id = $1
				`, session.ID).Scan(&createdAt, &idleExpiresAt, &absoluteExpiresAt); err != nil {
					t.Fatal(err)
				}
				if !createdAt.Before(time.Now()) || !idleExpiresAt.Before(time.Now()) || (tc.absolute && !absoluteExpiresAt.Before(time.Now())) {
					t.Fatalf("expired fixture was not in the past: created_at=%s idle_expires_at=%s absolute_expires_at=%s", createdAt, idleExpiresAt, absoluteExpiresAt)
				}
				if err := repository.RevokeAllSessions(ctx, hash, "expired-global-revoke"); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("expected expired token to be rejected, got %v", err)
				}
				var afterEpoch int64
				var status string
				if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&afterEpoch); err != nil {
					t.Fatal(err)
				}
				if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.auth_session WHERE id = $1`, session.ID).Scan(&status); err != nil {
					t.Fatal(err)
				}
				if afterEpoch != beforeEpoch || status != "ACTIVE" {
					t.Fatalf("expired token changed state: epoch %d -> %d, status=%s", beforeEpoch, afterEpoch, status)
				}
			})
		}
	})

	t.Run("global_revoke_rejects_stale_epoch", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0xb3}, 32)
		session, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{
			ID:            uuid.New(),
			UserAccountID: fixture.UserAAdmin,
			TokenHash:     hash,
			Policy:        policy,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.user_account
			SET session_epoch = session_epoch + 1
			WHERE id = $1
		`, fixture.UserAAdmin); err != nil {
			t.Fatal(err)
		}
		var beforeEpoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&beforeEpoch); err != nil {
			t.Fatal(err)
		}
		if err := repository.RevokeAllSessions(ctx, hash, "stale-epoch-global-revoke"); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected stale epoch token to be rejected, got %v", err)
		}
		var afterEpoch int64
		var status string
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&afterEpoch); err != nil {
			t.Fatal(err)
		}
		if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.auth_session WHERE id = $1`, session.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if afterEpoch != beforeEpoch || status != "ACTIVE" {
			t.Fatalf("stale token changed state: epoch %d -> %d, status=%s", beforeEpoch, afterEpoch, status)
		}
	})

	t.Run("global_revoke_rejects_revoked_token", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0xb4}, 32)
		session, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{
			ID:            uuid.New(),
			UserAccountID: fixture.UserAAdmin,
			TokenHash:     hash,
			Policy:        policy,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.RevokeSession(ctx, session.ID, hash, "before-global-revoke"); err != nil {
			t.Fatal(err)
		}
		var beforeEpoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&beforeEpoch); err != nil {
			t.Fatal(err)
		}
		if err := repository.RevokeAllSessions(ctx, hash, "revoked-global-revoke"); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected revoked token to be rejected, got %v", err)
		}
		var afterEpoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&afterEpoch); err != nil {
			t.Fatal(err)
		}
		if afterEpoch != beforeEpoch {
			t.Fatalf("revoked token changed epoch: %d -> %d", beforeEpoch, afterEpoch)
		}
	})

	t.Run("concurrent_global_revocation_has_one_winner", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0xb5}, 32)
		if _, err := createSyntheticSession(ctx, adminPool, auth.CreateSessionRequest{
			ID:            uuid.New(),
			UserAccountID: fixture.UserAAdmin,
			TokenHash:     hash,
			Policy:        policy,
		}); err != nil {
			t.Fatal(err)
		}
		var beforeEpoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&beforeEpoch); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- repository.RevokeAllSessions(ctx, hash, "concurrent-global-revoke")
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		var successes, noRows int
		for err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, pgx.ErrNoRows):
				noRows++
			default:
				t.Fatalf("unexpected concurrent global revoke error: %v", err)
			}
		}
		if successes != 1 || noRows != 1 {
			t.Fatalf("expected one global revoke winner and one invalidated-token rejection, successes=%d noRows=%d", successes, noRows)
		}
		var afterEpoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&afterEpoch); err != nil {
			t.Fatal(err)
		}
		if afterEpoch != beforeEpoch+1 {
			t.Fatalf("concurrent global revoke incremented epoch more than once: %d -> %d", beforeEpoch, afterEpoch)
		}
	})

	t.Run("audit_rollback", func(t *testing.T) {
		sessionID := uuid.New()
		callbackErr := withAccessReturningError(ctx, adminPool, tenancy.Platform(), func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO platform.auth_session (
					id, user_account_id, token_hash, status, created_at,
					last_seen_at, idle_expires_at, absolute_expires_at, session_epoch
				)
				VALUES ($1, $2, $3, 'ACTIVE', now(), now(), now() + interval '1 hour', now() + interval '2 hours', 1)
			`, sessionID, fixture.UserAAdmin, bytes.Repeat([]byte{0x81}, 32))
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO platform.security_audit_event (id, event_type, outcome, details)
				VALUES ($1, 'SESSION_CREATED', 'INVALID', '{}'::jsonb)
			`, uuid.New())
			return err
		})
		if callbackErr == nil {
			t.Fatal("expected invalid audit outcome to fail")
		}
		var count int
		if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM platform.auth_session WHERE id = $1`, sessionID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("session mutation survived audit rollback: %d", count)
		}
	})
}

// createSyntheticSession is deliberately test-only. It uses the admin DSN of
// the verified ephemeral harness to seed state; neither runtime role receives
// permission to call platform.create_auth_session in 1B.2-A.
func createSyntheticSession(ctx context.Context, pool *pgxpool.Pool, request auth.CreateSessionRequest) (auth.SessionRecord, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return auth.SessionRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var record auth.SessionRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO platform.auth_session (
			id, user_account_id, token_hash, csrf_secret_hash, status,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at,
			session_epoch, context_version, active_company_id, active_operation_id,
			authenticator_credential_id, device_authorization_id, user_agent_hash,
			ip_prefix
		)
		SELECT $1, $2, $3, $4, 'ACTIVE', now(), now(),
		       now() + make_interval(secs => $5),
		       now() + make_interval(secs => $6),
		       ua.session_epoch, 0, $7, $8, $9, $10, $11, NULLIF($12, '')
		FROM platform.user_account AS ua
		JOIN platform.person AS p ON p.id = ua.person_id
		WHERE ua.id = $2 AND ua.status = 'ACTIVE' AND p.status = 'ACTIVE'
		RETURNING id, user_account_id, session_epoch, status,
		          created_at, last_seen_at, idle_expires_at, absolute_expires_at,
		          active_company_id, active_operation_id, context_version
	`, request.ID, request.UserAccountID, request.TokenHash,
		nullableBytes(request.CSRFSecretHash),
		int64(request.Policy.IdleTTL/time.Second), int64(request.Policy.AbsoluteTTL/time.Second),
		request.ActiveCompanyID, request.ActiveOperationID,
		request.AuthenticatorCredentialID, request.DeviceAuthorizationID,
		nullableBytes(request.UserAgentHash), request.IPPrefix).Scan(
		&record.ID, &record.UserAccountID, &record.SessionEpoch, &record.Status,
		&record.CreatedAt, &record.LastSeenAt, &record.IdleExpiresAt,
		&record.AbsoluteExpiresAt, &record.ActiveCompanyID,
		&record.ActiveOperationID, &record.ContextVersion,
	)
	if err != nil {
		return auth.SessionRecord{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.security_audit_event (
			id, event_type, outcome, subject_user_account_id, details
		) VALUES ($1, 'SESSION_CREATED', 'SUCCESS', $2, '{}'::jsonb)
	`, uuid.New(), request.UserAccountID); err != nil {
		return auth.SessionRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.SessionRecord{}, err
	}
	return record, nil
}

func withAccessReturningError(ctx context.Context, pool *pgxpool.Pool, access tenancy.Context, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setLocalContext(ctx, tx, access); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
