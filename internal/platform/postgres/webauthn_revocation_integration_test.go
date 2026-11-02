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
	applicationauth "transportation-platform/internal/application/auth"
)

func testWebAuthnCredentialRevocation(t *testing.T, ctx context.Context, repository *OnlineAuthRepository, authPool, adminPool *pgxpool.Pool, fixture foundationFixture) {
	t.Helper()
	policy, err := applicationauth.PolicyFor(applicationauth.AdministrativeSession)
	if err != nil {
		t.Fatal(err)
	}
	rawToken := bytes.Repeat([]byte{0xd1}, 32)
	if _, err := createSyntheticSession(ctx, adminPool, applicationauth.CreateSessionRequest{
		ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: HashSessionToken(rawToken), Policy: policy,
	}); err != nil {
		t.Fatal(err)
	}

	revokedID := uuid.New()
	remainingID := uuid.New()
	revokedKey := bytes.Repeat([]byte{0xd2}, 16)
	remainingKey := bytes.Repeat([]byte{0xd3}, 16)
	for _, credential := range []struct {
		id  uuid.UUID
		key []byte
	}{
		{revokedID, revokedKey},
		{remainingID, remainingKey},
	} {
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.authenticator_credential (
				id, user_account_id, credential_id, public_key, status, metadata
			) VALUES ($1, $2, $3, $4, 'ACTIVE', '{}'::jsonb)
		`, credential.id, fixture.UserAAdmin, credential.key, []byte{0x04, 0x01}); err != nil {
			t.Fatal(err)
		}
	}

	if err := repository.RevokeOwnAuthenticator(ctx, HashSessionToken(rawToken), revokedID, "test replacement"); err != nil {
		t.Fatalf("revoke active credential: %v", err)
	}
	var status string
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.authenticator_credential WHERE id = $1`, revokedID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "REVOKED" {
		t.Fatalf("revoked credential status = %s", status)
	}

	postRevokeToken := bytes.Repeat([]byte{0xd9}, 32)
	if _, err := createSyntheticSession(ctx, adminPool, applicationauth.CreateSessionRequest{
		ID: uuid.New(), UserAccountID: fixture.UserAAdmin, TokenHash: HashSessionToken(postRevokeToken), Policy: policy,
	}); err != nil {
		t.Fatal(err)
	}
	challengeID := uuid.New()
	challengeHash := bytes.Repeat([]byte{0xd4}, 32)
	bindingHash := bytes.Repeat([]byte{0xd5}, 32)
	if err := repository.BeginWebAuthnChallenge(ctx, HashSessionToken(postRevokeToken), applicationauth.WebAuthnChallengeRequest{
		ID: challengeID, Purpose: applicationauth.StepUpChallenge, ChallengeHash: challengeHash,
		SessionData: []byte(`{"challenge":"synthetic"}`), SessionDataFormat: "webauthn.v1",
		BindingHash: bindingHash, ExpiresAt: time.Now().UTC().Add(5 * time.Minute), AuditID: uuid.New(),
	}); err != nil {
		t.Fatal(err)
	}
	var stepUpExpiry *time.Time
	if err := authPool.QueryRow(ctx, `
		SELECT platform.complete_webauthn_step_up($1,$2,$3,$4,$5,$6)
	`, HashSessionToken(postRevokeToken), challengeID, challengeHash, bindingHash, revokedKey, uuid.New()).Scan(&stepUpExpiry); err != nil {
		t.Fatal(err)
	}
	if stepUpExpiry != nil {
		t.Fatal("revoked credential completed step-up")
	}

	if err := repository.RevokeOwnAuthenticator(ctx, HashSessionToken(postRevokeToken), remainingID, "last credential"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected last active credential to remain protected, got %v", err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.authenticator_credential WHERE id = $1`, remainingID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ACTIVE" {
		t.Fatalf("last credential status = %s", status)
	}

	concurrentToken := bytes.Repeat([]byte{0xd6}, 32)
	if _, err := createSyntheticSession(ctx, adminPool, applicationauth.CreateSessionRequest{
		ID: uuid.New(), UserAccountID: fixture.UserBAdmin, TokenHash: HashSessionToken(concurrentToken), Policy: policy,
	}); err != nil {
		t.Fatal(err)
	}
	concurrentIDs := []uuid.UUID{uuid.New(), uuid.New()}
	concurrentKeys := [][]byte{bytes.Repeat([]byte{0xd7}, 16), bytes.Repeat([]byte{0xd8}, 16)}
	for i := range concurrentIDs {
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.authenticator_credential (
				id, user_account_id, credential_id, public_key, status, metadata
			) VALUES ($1, $2, $3, $4, 'ACTIVE', '{}'::jsonb)
		`, concurrentIDs[i], fixture.UserBAdmin, concurrentKeys[i], []byte{0x04, byte(i + 2)}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, len(concurrentIDs))
	for _, credentialID := range concurrentIDs {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			results <- repository.RevokeOwnAuthenticator(ctx, HashSessionToken(concurrentToken), id, "concurrent replacement")
		}(credentialID)
	}
	wg.Wait()
	close(results)
	var successes, noRows int
	for revokeErr := range results {
		switch {
		case revokeErr == nil:
			successes++
		case errors.Is(revokeErr, pgx.ErrNoRows):
			noRows++
		default:
			t.Fatalf("unexpected concurrent credential revoke error: %v", revokeErr)
		}
	}
	if successes != 1 || noRows != 1 {
		t.Fatalf("expected one concurrent revoke winner and one protected last credential, successes=%d noRows=%d", successes, noRows)
	}
	var activeCount int
	if err := adminPool.QueryRow(ctx, `
		SELECT count(*) FROM platform.authenticator_credential
		 WHERE user_account_id = $1 AND status = 'ACTIVE'
	`, fixture.UserBAdmin).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Fatalf("concurrent revocation left %d active credentials", activeCount)
	}
}
