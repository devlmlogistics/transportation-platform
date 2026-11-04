package postgres

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDeviceAuthorizationBoundaries(t *testing.T, ctx context.Context, authPool *pgxpool.Pool, adminDSN string, fixture foundationFixture) {
	t.Helper()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	newTokenHash := func() []byte {
		seed := uuid.New()
		hash := sha256.Sum256(seed[:])
		return hash[:]
	}

	companyOperationRequest := func(userID uuid.UUID, companyID uuid.UUID, operationID *uuid.UUID) uuid.UUID {
		t.Helper()
		requestID := uuid.New()
		var operation interface{}
		if operationID != nil {
			operation = *operationID
		}
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.device_enrollment_request(
				id, correlation_code, user_account_id, company_id, company_operation_id,
				installation_id, binding_hash, status, expires_at,
				installation_public_key, key_algorithm)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'PENDING', clock_timestamp() + interval '10 minutes', $8, $9)
		`, requestID, requestID.String(), userID, companyID, operation,
			uuid.New(), makeHash(byte(requestID[0])), makeHash(0xA1), "ES256"); err != nil {
			t.Fatal(err)
		}
		return requestID
	}

	insertSession := func(userID uuid.UUID, tokenHash []byte) uuid.UUID {
		t.Helper()
		sessionID := uuid.New()
		var epoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, userID).Scan(&epoch); err != nil {
			t.Fatal(err)
		}
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.auth_session(
				id, user_account_id, token_hash, csrf_secret_hash, status,
				last_seen_at, idle_expires_at, absolute_expires_at, session_epoch)
			VALUES ($1, $2, $3, $4, 'ACTIVE', clock_timestamp(),
				clock_timestamp() + interval '1 hour', clock_timestamp() + interval '2 hours', $5)
		`, sessionID, userID, tokenHash, makeHash(byte(sessionID[0])), epoch); err != nil {
			t.Fatal(err)
		}
		return sessionID
	}

	callDecision := func(functionName string, tokenHash []byte, targetID uuid.UUID) bool {
		t.Helper()
		var allowed bool
		query := `SELECT platform.` + functionName + `($1, $2, 'test-boundary', $3)`
		if err := authPool.QueryRow(ctx, query, tokenHash, targetID, uuid.New()).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		return allowed
	}

	supervisorToken := newTokenHash()
	supervisorSession := insertSession(fixture.UserASupervisor, supervisorToken)

	t.Run("read_queries_are_scope_limited", func(t *testing.T) {
		requestA := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		requestB := companyOperationRequest(fixture.UserBAdmin, fixture.CompanyB, &fixture.OperationB)
		var visibleRequests int
		if err := authPool.QueryRow(ctx, `SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2`, supervisorToken, requestA).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 1 {
			t.Fatalf("visible requests = %d, want 1; A=%s B=%s", visibleRequests, requestA, requestB)
		}
		if err := authPool.QueryRow(ctx, `SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2`, supervisorToken, requestB).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 0 {
			t.Fatalf("cross-company request was visible: %s", requestB)
		}

		authorizationA := uuid.New()
		authorizationB := uuid.New()
		for _, item := range []struct {
			id, user, company, operation uuid.UUID
		}{
			{authorizationA, fixture.UserADriver, fixture.CompanyA, fixture.OperationA},
			{authorizationB, fixture.UserBAdmin, fixture.CompanyB, fixture.OperationB},
		} {
			if _, err := adminPool.Exec(ctx, `
				INSERT INTO platform.device_authorization(
					id, installation_id, user_account_id, company_id, company_operation_id,
					installation_public_key, key_algorithm, status, approved_at, expires_at)
				VALUES ($1, $2, $3, $4, $5, $6, 'ECDSA_P256_SHA256', 'ACTIVE', clock_timestamp(), clock_timestamp() + interval '1 day')
			`, item.id, uuid.New(), item.user, item.company, item.operation, makeHash(byte(item.id[0]))); err != nil {
				t.Fatal(err)
			}
		}
		var visibleAuthorizations int
		if err := authPool.QueryRow(ctx, `SELECT count(*) FROM platform.list_device_authorizations($1) WHERE authorization_id = $2`, supervisorToken, authorizationA).Scan(&visibleAuthorizations); err != nil {
			t.Fatal(err)
		}
		if visibleAuthorizations != 1 {
			t.Fatalf("visible authorizations = %d, want 1", visibleAuthorizations)
		}
		if err := authPool.QueryRow(ctx, `SELECT count(*) FROM platform.list_device_authorizations($1) WHERE authorization_id = $2`, supervisorToken, authorizationB).Scan(&visibleAuthorizations); err != nil {
			t.Fatal(err)
		}
		if visibleAuthorizations != 0 {
			t.Fatalf("cross-company authorization was visible: %s", authorizationB)
		}
	})

	t.Run("revoked_company_membership_blocks_rejection", func(t *testing.T) {
		requestID := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.company_membership SET status = 'REVOKED'
			 WHERE company_id = $1 AND person_id = $2
		`, fixture.CompanyA, fixture.PersonASupervisor); err != nil {
			t.Fatal(err)
		}
		if callDecision("reject_device_enrollment_request", supervisorToken, requestID) {
			t.Fatal("revoked company membership still allowed rejection")
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.company_membership SET status = 'ACTIVE'
			 WHERE company_id = $1 AND person_id = $2
		`, fixture.CompanyA, fixture.PersonASupervisor); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revoked_operation_membership_blocks_approve_and_reject", func(t *testing.T) {
		approveRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		rejectRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.operation_membership SET status = 'REVOKED'
			 WHERE company_operation_id = $1 AND person_id = $2
		`, fixture.OperationA, fixture.PersonASupervisor); err != nil {
			t.Fatal(err)
		}
		if callDecision("approve_device_enrollment_request", supervisorToken, approveRequest) {
			t.Fatal("revoked operation membership still allowed approval")
		}
		if callDecision("reject_device_enrollment_request", supervisorToken, rejectRequest) {
			t.Fatal("revoked operation membership still allowed rejection")
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.operation_membership SET status = 'ACTIVE'
			 WHERE company_operation_id = $1 AND person_id = $2
		`, fixture.OperationA, fixture.PersonASupervisor); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("cross_company_and_expired_role_are_denied", func(t *testing.T) {
		crossCompanyRequest := companyOperationRequest(fixture.UserBAdmin, fixture.CompanyB, &fixture.OperationB)
		if callDecision("approve_device_enrollment_request", supervisorToken, crossCompanyRequest) {
			t.Fatal("company A supervisor operated on company B")
		}

		expiredRoleRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.role_assignment
			   SET valid_until = clock_timestamp() - interval '1 second'
			 WHERE person_id = $1 AND role_code = 'SUPERVISOR'
		`, fixture.PersonASupervisor); err != nil {
			t.Fatal(err)
		}
		if callDecision("approve_device_enrollment_request", supervisorToken, expiredRoleRequest) {
			t.Fatal("expired supervisor role still allowed approval")
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.role_assignment SET valid_until = NULL
			 WHERE person_id = $1 AND role_code = 'SUPERVISOR'
		`, fixture.PersonASupervisor); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revoked_session_and_direct_sql_without_authorization_are_denied", func(t *testing.T) {
		requestID := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `UPDATE platform.auth_session SET status = 'REVOKED', revoked_at = clock_timestamp() WHERE id = $1`, supervisorSession); err != nil {
			t.Fatal(err)
		}
		if callDecision("reject_device_enrollment_request", supervisorToken, requestID) {
			t.Fatal("revoked session still administered a device request")
		}
		invalidRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if callDecision("reject_device_enrollment_request", makeHash(0xEE), invalidRequest) {
			t.Fatal("tp_auth_runtime accepted an unauthorized direct SQL call")
		}
	})

	t.Run("concurrent_revocation_is_single_use", func(t *testing.T) {
		token := newTokenHash()
		insertSession(fixture.UserASupervisor, token)
		authorizationID := uuid.New()
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.device_authorization(
				id, installation_id, user_account_id, company_id, company_operation_id,
				installation_public_key, key_algorithm, status, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'ES256', 'ACTIVE', clock_timestamp() + interval '1 hour')
		`, authorizationID, uuid.New(), fixture.UserADriver, fixture.CompanyA, fixture.OperationA, makeHash(0xC2)); err != nil {
			t.Fatal(err)
		}
		results := make(chan bool, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var allowed bool
				err := authPool.QueryRow(ctx, `
					SELECT platform.revoke_device_authorization($1, $2, 'concurrent-test', $3)
				`, token, authorizationID, uuid.New()).Scan(&allowed)
				if err != nil {
					t.Errorf("concurrent revoke: %v", err)
					return
				}
				results <- allowed
			}()
		}
		wg.Wait()
		close(results)
		var successes int
		for allowed := range results {
			if allowed {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent revocation successes = %d, want 1", successes)
		}
	})

}
