package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

	insertActivationSession := func(userID uuid.UUID, tokenHash []byte) uuid.UUID {
		t.Helper()
		sessionID := uuid.New()
		var epoch int64
		if err := adminPool.QueryRow(ctx, `SELECT session_epoch FROM platform.user_account WHERE id = $1`, userID).Scan(&epoch); err != nil {
			t.Fatal(err)
		}
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.auth_session(
				id, user_account_id, token_hash, csrf_secret_hash, status,
				last_seen_at, idle_expires_at, absolute_expires_at, session_epoch,
				session_policy)
			VALUES ($1, $2, $3, $4, 'ACTIVE', clock_timestamp(),
				clock_timestamp() + interval '15 minutes', clock_timestamp() + interval '1 hour', $5,
				'DRIVER_ACTIVATION')
		`, sessionID, userID, tokenHash, makeHash(byte(sessionID[0])), epoch); err != nil {
			t.Fatal(err)
		}
		return sessionID
	}

	createActivationRequest := func(tokenHash []byte, requestID, companyID uuid.UUID, operationID *uuid.UUID, installationID uuid.UUID) error {
		var operation any
		if operationID != nil {
			operation = *operationID
		}
		var returnedID uuid.UUID
		return authPool.QueryRow(ctx, `
			SELECT request_id
			  FROM platform.create_device_enrollment_request(
				$1, $2, $3, $4, $5, $6, $7, 'ECDSA_P256_SHA256', $8, clock_timestamp() + interval '10 minutes', $9)
		`, tokenHash, requestID, requestID.String(), companyID, operation, installationID,
			makeHash(byte(requestID[0])), makeHash(byte(requestID[1])), uuid.New()).Scan(&returnedID)
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
		companyScopedRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, nil)
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, companyScopedRequest).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 0 {
			t.Fatal("operation-only supervisor saw a company-scoped request")
		}
		companyApproveGrant := uuid.New()
		companyRevokeGrant := uuid.New()
		for _, grant := range []struct {
			id   uuid.UUID
			code string
		}{
			{companyApproveGrant, "device.enrollment.approve"},
			{companyRevokeGrant, "device.enrollment.revoke"},
		} {
			if _, err := adminPool.Exec(ctx, `
				INSERT INTO platform.permission_grant(
					id, person_id, permission_code, scope_type, company_id,
					valid_from, lifecycle_status)
				VALUES ($1, $2, $3, 'COMPANY', $4, clock_timestamp() - interval '1 minute', 'ACTIVE')
			`, grant.id, fixture.PersonASupervisor, grant.code, fixture.CompanyA); err != nil {
				t.Fatal(err)
			}
		}
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, companyScopedRequest).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 1 {
			t.Fatalf("company-scoped permission did not reveal company request: visible=%d", visibleRequests)
		}
		if !callDecision("approve_device_enrollment_request", supervisorToken, companyScopedRequest) {
			t.Fatal("company-scoped permission did not allow approval")
		}
		approvedRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.device_enrollment_request
			   SET status = 'APPROVED', approved_at = clock_timestamp()
			 WHERE id = $1
		`, approvedRequest); err != nil {
			t.Fatal(err)
		}
		var approvedStatus string
		if err := authPool.QueryRow(ctx, `
			SELECT status FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, approvedRequest).Scan(&approvedStatus); err != nil {
			t.Fatal(err)
		}
		if approvedStatus != "APPROVED" {
			t.Fatalf("approved request status = %q, want APPROVED", approvedStatus)
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.permission_grant
			   SET lifecycle_status = 'REVOKED', revoked_at = clock_timestamp()
			 WHERE id IN ($1, $2)
		`, companyApproveGrant, companyRevokeGrant); err != nil {
			t.Fatal(err)
		}
		driverToken := newTokenHash()
		insertSession(fixture.UserADriver, driverToken)
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, driverToken, requestA).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 0 {
			t.Fatalf("driver without supervisor permission saw request: %s", requestA)
		}

		requestWithChangedTargetMembership := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.operation_membership
			   SET status = 'REVOKED'
			 WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3
		`, fixture.CompanyA, fixture.OperationA, fixture.PersonADriver); err != nil {
			t.Fatal(err)
		}
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, requestWithChangedTargetMembership).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 1 {
			t.Fatalf("request disappeared after target membership changed: visible=%d request=%s", visibleRequests, requestWithChangedTargetMembership)
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.operation_membership
			   SET status = 'ACTIVE'
			 WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3
		`, fixture.CompanyA, fixture.OperationA, fixture.PersonADriver); err != nil {
			t.Fatal(err)
		}

		expiredRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.device_enrollment_request
			   SET requested_at = clock_timestamp() - interval '2 minutes',
			       expires_at = clock_timestamp() - interval '1 second'
			 WHERE id = $1
		`, expiredRequest); err != nil {
			t.Fatal(err)
		}
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, expiredRequest).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 1 {
			t.Fatalf("expired request was not retained for status display: %s", expiredRequest)
		}
		if err := authPool.QueryRow(ctx, `
			SELECT status FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, expiredRequest).Scan(&approvedStatus); err != nil {
			t.Fatal(err)
		}
		if approvedStatus != "EXPIRED" {
			t.Fatalf("expired request status = %q, want EXPIRED", approvedStatus)
		}

		consumedRequest := companyOperationRequest(fixture.UserADriver, fixture.CompanyA, &fixture.OperationA)
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.device_enrollment_request
			   SET status = 'CONSUMED', consumed_at = clock_timestamp()
			 WHERE id = $1
		`, consumedRequest); err != nil {
			t.Fatal(err)
		}
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_enrollment_requests($1) WHERE request_id = $2
		`, supervisorToken, consumedRequest).Scan(&visibleRequests); err != nil {
			t.Fatal(err)
		}
		if visibleRequests != 0 {
			t.Fatalf("consumed request was visible: %s", consumedRequest)
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
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.operation_membership
			   SET status = 'REVOKED'
			 WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3
		`, fixture.CompanyA, fixture.OperationA, fixture.PersonADriver); err != nil {
			t.Fatal(err)
		}
		if err := authPool.QueryRow(ctx, `
			SELECT count(*) FROM platform.list_device_authorizations($1) WHERE authorization_id = $2
		`, supervisorToken, authorizationA).Scan(&visibleAuthorizations); err != nil {
			t.Fatal(err)
		}
		if visibleAuthorizations != 1 {
			t.Fatalf("authorization disappeared after target membership changed: visible=%d authorization=%s", visibleAuthorizations, authorizationA)
		}
		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.operation_membership
			   SET status = 'ACTIVE'
			 WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3
		`, fixture.CompanyA, fixture.OperationA, fixture.PersonADriver); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("activation_requests_are_deduplicated_per_installation_scope", func(t *testing.T) {
		t.Helper()
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO platform.driver_profile (id, person_id, status)
			VALUES ($1, $2, 'ACTIVE')
			ON CONFLICT (person_id) DO UPDATE SET status = 'ACTIVE'
		`, uuid.New(), fixture.PersonADriver); err != nil {
			t.Fatal(err)
		}
		token := newTokenHash()
		insertActivationSession(fixture.UserADriver, token)
		installationID := uuid.New()
		firstRequestID := uuid.New()
		if err := createActivationRequest(token, firstRequestID, fixture.CompanyA, &fixture.OperationA, installationID); err != nil {
			t.Fatalf("first activation request: %v", err)
		}

		assertActiveConflict := func(err error) {
			t.Helper()
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "P0001" || pgErr.ConstraintName != "device_enrollment_request_active_conflict" {
				t.Fatalf("error = %v, want active-request conflict", err)
			}
		}
		assertActiveConflict(createActivationRequest(token, uuid.New(), fixture.CompanyA, &fixture.OperationA, installationID))

		if _, err := adminPool.Exec(ctx, `UPDATE platform.device_enrollment_request SET status = 'APPROVED', approved_at = clock_timestamp() WHERE id = $1`, firstRequestID); err != nil {
			t.Fatal(err)
		}
		assertActiveConflict(createActivationRequest(token, uuid.New(), fixture.CompanyA, &fixture.OperationA, installationID))

		if _, err := adminPool.Exec(ctx, `
			UPDATE platform.device_enrollment_request
			   SET requested_at = clock_timestamp() - interval '2 minutes',
			       expires_at = clock_timestamp() - interval '1 second'
			 WHERE id = $1
		`, firstRequestID); err != nil {
			t.Fatal(err)
		}
		if err := createActivationRequest(token, uuid.New(), fixture.CompanyA, &fixture.OperationA, installationID); err != nil {
			t.Fatalf("request after expiry: %v", err)
		}

		if err := createActivationRequest(token, uuid.New(), fixture.CompanyA, nil, installationID); err != nil {
			t.Fatalf("different scope on same installation: %v", err)
		}
		if err := createActivationRequest(token, uuid.New(), fixture.CompanyA, &fixture.OperationA, uuid.New()); err != nil {
			t.Fatalf("different installation: %v", err)
		}

		otherDriverToken := newTokenHash()
		insertActivationSession(fixture.UserShared, otherDriverToken)
		if err := createActivationRequest(otherDriverToken, uuid.New(), fixture.CompanyB, &fixture.OperationB, uuid.New()); err != nil {
			t.Fatalf("different driver and company: %v", err)
		}

		concurrentInstallationID := uuid.New()
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results <- createActivationRequest(token, uuid.New(), fixture.CompanyA, &fixture.OperationA, concurrentInstallationID)
			}()
		}
		wg.Wait()
		close(results)
		successes := 0
		conflicts := 0
		for err := range results {
			if err == nil {
				successes++
				continue
			}
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "device_enrollment_request_active_conflict" {
				conflicts++
				continue
			}
			t.Fatalf("concurrent activation request: %v", err)
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent results: successes=%d conflicts=%d, want 1/1", successes, conflicts)
		}

		var activeCount, historyCount int
		if err := adminPool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status IN ('PENDING', 'APPROVED') AND expires_at > clock_timestamp()), count(*)
			  FROM platform.device_enrollment_request
			 WHERE installation_id = $1 AND company_id = $2 AND company_operation_id = $3
		`, installationID, fixture.CompanyA, fixture.OperationA).Scan(&activeCount, &historyCount); err != nil {
			t.Fatal(err)
		}
		if activeCount != 1 || historyCount != 2 {
			t.Fatalf("installation history: active=%d total=%d, want 1/2", activeCount, historyCount)
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
