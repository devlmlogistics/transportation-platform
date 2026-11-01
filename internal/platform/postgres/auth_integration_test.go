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

func testAuthFoundation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, adminDSN string, fixture foundationFixture) {
	t.Helper()
	repository := NewAuthRepository(pool)
	policy, err := auth.PolicyFor(auth.AdministrativeSession)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("session_hash_resolution_and_individual_revoke", func(t *testing.T) {
		rawToken := bytes.Repeat([]byte{0x11}, 32)
		tokenHash := HashSessionToken(rawToken)
		session, err := repository.CreateSession(ctx, auth.CreateSessionRequest{
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
		if err := repository.RevokeSession(ctx, fixture.UserAAdmin, session.ID, "test_revoke", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ResolveSession(ctx, tokenHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected revoked token to return no rows, got %v", err)
		}
	})

	t.Run("global_epoch_revocation", func(t *testing.T) {
		firstHash := bytes.Repeat([]byte{0x31}, 32)
		secondHash := bytes.Repeat([]byte{0x32}, 32)
		if _, err := repository.CreateSession(ctx, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: firstHash, Policy: policy}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.CreateSession(ctx, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: secondHash, Policy: policy}); err != nil {
			t.Fatal(err)
		}
		if err := repository.RevokeAllSessions(ctx, fixture.UserAAdmin, "test_revoke_all", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		for name, hash := range map[string][]byte{"first": firstHash, "second": secondHash} {
			if _, err := repository.ResolveSession(ctx, hash); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("%s session survived global revoke: %v", name, err)
			}
		}
		var epoch int64
		withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, fixture.UserAAdmin).Scan(&epoch)
		})
		if epoch != 1 {
			t.Fatalf("expected session epoch 1, got %d", epoch)
		}
		thirdHash := bytes.Repeat([]byte{0x33}, 32)
		created, err := repository.CreateSession(ctx, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: thirdHash, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		if created.SessionEpoch != 1 {
			t.Fatalf("new session did not capture incremented epoch: %+v", created)
		}
	})

	t.Run("expired_session_is_not_resolved", func(t *testing.T) {
		expiredRepository := NewAuthRepository(pool)
		expiredRepository.Clock = func() time.Time { return time.Now().UTC().Add(-2 * time.Hour) }
		expiredHash := bytes.Repeat([]byte{0x41}, 32)
		shortPolicy := auth.SessionPolicy{IdleTTL: time.Minute, AbsoluteTTL: 2 * time.Minute}
		if _, err := expiredRepository.CreateSession(ctx, auth.CreateSessionRequest{ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: expiredHash, Policy: shortPolicy}); err != nil {
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
		for _, table := range []string{"auth_session", "auth_challenge", "authenticator_credential", "password_credential", "recovery_request"} {
			err := withAccessReturningError(ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
				var count int
				return tx.QueryRow(ctx, "SELECT count(*) FROM platform."+table).Scan(&count)
			})
			if err == nil {
				t.Fatalf("tp_runtime unexpectedly selected platform.%s", table)
			}
		}
	})

	t.Run("audit_rollback", func(t *testing.T) {
		adminPool, err := pgxpool.New(ctx, adminDSN)
		if err != nil {
			t.Fatal(err)
		}
		defer adminPool.Close()
		sessionID := uuid.New()
		callbackErr := withAccessReturningError(ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
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
