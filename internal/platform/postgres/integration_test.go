package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/platform/migrations"
	"transportation-platform/internal/platform/tenancy"
	"transportation-platform/internal/platform/testdb"
)

type foundationFixture struct {
	CompanyA          uuid.UUID
	CompanyB          uuid.UUID
	OperationA        uuid.UUID
	OperationB        uuid.UUID
	PersonAAdmin      uuid.UUID
	PersonASupervisor uuid.UUID
	PersonADriver     uuid.UUID
	PersonBAdmin      uuid.UUID
	PersonShared      uuid.UUID
	PersonGlobal      uuid.UUID
	UserAAdmin        uuid.UUID
	UserASupervisor   uuid.UUID
	UserADriver       uuid.UUID
	UserBAdmin        uuid.UUID
	UserShared        uuid.UUID
	UserGlobal        uuid.UUID
	SupervisorRoleID  uuid.UUID
}

func TestPostgres16Foundation(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := filepath.Clean(filepath.Join(migrationDirectory(t), "..", ".."))
	harness, err := testdb.Start(ctx, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := harness.Close(context.Background()); err != nil {
			t.Errorf("close PostgreSQL harness: %v", err)
		}
	})
	runtimeDSN := harness.RuntimeDSN
	migratorDSN := harness.MigratorDSN
	adminDSN := harness.AdminDSN

	migratorDB, err := migrations.Open(ctx, migratorDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer migratorDB.Close()
	prepareSchema(t, ctx, migratorDB, adminDSN)

	runtimePool, err := pgxpool.New(ctx, runtimeDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	if err := runtimePool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	authPool, err := pgxpool.New(ctx, harness.AuthDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer authPool.Close()
	if err := authPool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	driverAuthPool, err := pgxpool.New(ctx, harness.DriverAuthDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer driverAuthPool.Close()
	if err := driverAuthPool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	fixture := seedFoundation(t, ctx, runtimePool)
	t.Run("tenant_isolation", func(t *testing.T) { testTenantIsolation(t, ctx, runtimePool, fixture) })
	t.Run("global_identity_rls", func(t *testing.T) { testGlobalIdentityRLS(t, ctx, runtimePool, fixture) })
	t.Run("authorization", func(t *testing.T) { testAuthorization(t, ctx, runtimePool, fixture) })
	t.Run("authorized_transaction_boundary", func(t *testing.T) { testAuthorizedTransactionBoundary(t, ctx, runtimePool, fixture) })
	t.Run("lifecycle", func(t *testing.T) { testLifecycle(t, ctx, runtimePool, fixture) })
	t.Run("state_invariants", func(t *testing.T) { testStateInvariants(t, ctx, runtimePool, fixture) })
	t.Run("audit_and_privileges", func(t *testing.T) { testAuditAndPrivileges(t, ctx, runtimePool, adminDSN, fixture) })
	t.Run("owner_force_rls", func(t *testing.T) { testOwnerForceRLS(t, ctx, migratorDSN) })
	t.Run("pool_context_a_b_a", func(t *testing.T) { testPoolContextSequence(t, ctx, runtimePool, fixture) })
	t.Run("auth_foundation", func(t *testing.T) { testAuthFoundation(t, ctx, runtimePool, authPool, adminDSN, fixture) })
	t.Run("online_authentication", func(t *testing.T) { testOnlineAuth(t, ctx, runtimePool, authPool, driverAuthPool, adminDSN, fixture) })
	t.Run("device_authorization_boundaries", func(t *testing.T) {
		testDeviceAuthorizationBoundaries(t, ctx, authPool, adminDSN, fixture)
	})

	version, err := migrations.Version(ctx, migratorDB)
	if err != nil || version != 21 {
		t.Fatalf("expected migration version 21 after integration suite, version=%d err=%v", version, err)
	}
}

func prepareSchema(t *testing.T, ctx context.Context, db *sql.DB, adminDSN string) {
	t.Helper()
	directory := migrationDirectory(t)
	if version, err := migrations.Version(ctx, db); err == nil && version > 0 {
		clearSyntheticData(t, ctx, adminDSN)
		if err := migrations.DownTo(ctx, db, directory, 0); err != nil {
			t.Fatalf("reset migrations: %v", err)
		}
	}
	if err := migrations.Up(ctx, db, directory); err != nil {
		t.Fatalf("migrations up: %v", err)
	}
	version, err := migrations.Version(ctx, db)
	if err != nil || version != 21 {
		t.Fatalf("expected version 21 after up, version=%d err=%v", version, err)
	}
	if err := migrations.DownTo(ctx, db, directory, 0); err != nil {
		t.Fatalf("migrations down: %v", err)
	}
	if err := migrations.Up(ctx, db, directory); err != nil {
		t.Fatalf("migrations up after down: %v", err)
	}
}

func clearSyntheticData(t *testing.T, ctx context.Context, adminDSN string) {
	t.Helper()
	adminDB, err := migrations.Open(ctx, adminDSN)
	if err != nil {
		t.Fatalf("open admin cleanup connection: %v", err)
	}
	defer adminDB.Close()
	if _, err := adminDB.ExecContext(ctx, `
		TRUNCATE TABLE
			platform.bootstrap_control,
			platform.auth_password_verification,
			platform.security_audit_event,
			platform.recovery_request,
			platform.auth_session,
			platform.auth_challenge,
			platform.device_enrollment_request,
			platform.device_authorization,
			platform.authenticator_credential,
			platform.password_credential,
			platform.admin_denied_attempt,
			platform.admin_audit_event,
			platform.permission_grant,
			platform.role_assignment,
			platform.operation_membership,
			platform.company_membership,
			platform.driver_profile,
			platform.user_account,
			platform.person,
			platform.company_operation,
			platform.company
		CASCADE
	`); err != nil {
		t.Fatalf("clear synthetic data: %v", err)
	}
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", "..", "..", "db", "migrations"))
}

func seedFoundation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) foundationFixture {
	t.Helper()
	f := foundationFixture{
		CompanyA:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		CompanyB:          uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		OperationA:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-000000000001"),
		OperationB:        uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-000000000001"),
		PersonAAdmin:      uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-100000000001"),
		PersonASupervisor: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-100000000002"),
		PersonADriver:     uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-100000000003"),
		PersonBAdmin:      uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-100000000001"),
		PersonShared:      uuid.MustParse("cccccccc-cccc-cccc-cccc-100000000001"),
		PersonGlobal:      uuid.MustParse("dddddddd-dddd-dddd-dddd-100000000001"),
		UserAAdmin:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-200000000001"),
		UserASupervisor:   uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-200000000002"),
		UserADriver:       uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-200000000003"),
		UserBAdmin:        uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-200000000001"),
		UserShared:        uuid.MustParse("cccccccc-cccc-cccc-cccc-200000000001"),
		UserGlobal:        uuid.MustParse("dddddddd-dddd-dddd-dddd-200000000001"),
		SupervisorRoleID:  uuid.New(),
	}

	companyRepo := CompanyRepository{}
	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		if err := companyRepo.Create(ctx, tx, CompanyRecord{ID: f.CompanyA, Slug: "acme-oxnard", LegalName: "Acme Oxnard Logistics", Status: "ACTIVE"}); err != nil {
			return err
		}
		return companyRepo.Create(ctx, tx, CompanyRecord{ID: f.CompanyB, Slug: "beta-longhaul", LegalName: "Beta Longhaul Transport", Status: "ACTIVE"})
	})

	createOperation(t, ctx, pool, f.CompanyA, f.OperationA, "DRAYAGE")
	createOperation(t, ctx, pool, f.CompanyB, f.OperationB, "TRANSPORTATION")
	createPersonMembership(t, ctx, pool, f.CompanyA, f.PersonAAdmin, f.UserAAdmin, "Acme Admin")
	createPersonMembership(t, ctx, pool, f.CompanyA, f.PersonASupervisor, f.UserASupervisor, "Acme Supervisor")
	createPersonMembership(t, ctx, pool, f.CompanyA, f.PersonADriver, f.UserADriver, "Acme Driver")
	createPersonMembership(t, ctx, pool, f.CompanyA, f.PersonShared, f.UserShared, "Shared Person")
	createPersonMembership(t, ctx, pool, f.CompanyB, f.PersonBAdmin, f.UserBAdmin, "Beta Admin")
	createPersonMembershipOnly(t, ctx, pool, f.CompanyB, f.PersonShared, "Shared Person")
	createPersonOnly(t, ctx, pool, f.CompanyA, f.PersonGlobal, f.UserGlobal, "Platform Admin")
	createDriverProfile(t, ctx, pool, f.PersonShared)

	createOperationMembership(t, ctx, pool, f.CompanyA, f.OperationA, f.PersonASupervisor)
	createOperationMembership(t, ctx, pool, f.CompanyA, f.OperationA, f.PersonADriver)
	createOperationMembership(t, ctx, pool, f.CompanyB, f.OperationB, f.PersonShared)

	assignRole(t, ctx, pool, f.PersonAAdmin, "COMPANY_ADMIN", "COMPANY", &f.CompanyA, nil)
	assignRole(t, ctx, pool, f.PersonASupervisor, "SUPERVISOR", "OPERATION", &f.CompanyA, &f.OperationA)
	assignRole(t, ctx, pool, f.PersonADriver, "DRIVER", "OPERATION", &f.CompanyA, &f.OperationA)
	assignRole(t, ctx, pool, f.PersonBAdmin, "COMPANY_ADMIN", "COMPANY", &f.CompanyB, nil)
	assignRole(t, ctx, pool, f.PersonShared, "COMPANY_ADMIN", "COMPANY", &f.CompanyA, nil)
	assignRole(t, ctx, pool, f.PersonShared, "AUDITOR", "COMPANY", &f.CompanyB, nil)
	assignRole(t, ctx, pool, f.PersonGlobal, "GLOBAL_ADMIN", "PLATFORM", nil, nil)

	return f
}

func createOperation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID, operationID uuid.UUID, operationType string) {
	t.Helper()
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO platform.company_operation (id, company_id, operation_type_code)
			VALUES ($1, $2, $3)
		`, operationID, companyID, operationType)
		return err
	})
}

func createPersonMembership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID, personID, userID uuid.UUID, name string) {
	t.Helper()
	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		if err := insertPersonAndUser(ctx, tx, personID, userID, name); err != nil {
			return err
		}
		return nil
	})
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		return insertMembership(ctx, tx, companyID, personID)
	})
}

func createPersonMembershipOnly(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID, personID uuid.UUID, name string) {
	t.Helper()
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		return insertMembership(ctx, tx, companyID, personID)
	})
}

func createPersonOnly(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID, personID, userID uuid.UUID, name string) {
	t.Helper()
	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		return insertPersonAndUser(ctx, tx, personID, userID, name)
	})
}

func createDriverProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, personID uuid.UUID) {
	t.Helper()
	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO platform.driver_profile (id, person_id, status)
			VALUES ($1, $2, 'ACTIVE')
		`, uuid.New(), personID)
		return err
	})
}

func insertPersonAndUser(ctx context.Context, tx pgx.Tx, personID, userID uuid.UUID, name string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO platform.person (id, legal_name) VALUES ($1, $2)`, personID, name); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO platform.user_account (id, person_id, status) VALUES ($1, $2, 'ACTIVE')`, userID, personID)
	return err
}

func insertMembership(ctx context.Context, tx pgx.Tx, companyID, personID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.company_membership (id, company_id, person_id, status)
		VALUES ($1, $2, $3, 'ACTIVE')
	`, uuid.New(), companyID, personID)
	return err
}

func createOperationMembership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID, operationID, personID uuid.UUID) {
	t.Helper()
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO platform.operation_membership (id, company_id, company_operation_id, person_id, status)
			VALUES ($1, $2, $3, $4, 'ACTIVE')
		`, uuid.New(), companyID, operationID, personID)
		return err
	})
}

func assignRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, personID uuid.UUID, role, scope string, companyID, operationID *uuid.UUID) {
	t.Helper()
	access := tenancy.Platform()
	if companyID != nil {
		access = tenancy.Tenant(*companyID)
	}
	validFrom := time.Now().UTC().Add(-time.Hour)
	withAccess(t, ctx, pool, access, func(tx pgx.Tx) error {
		return CreateRoleAssignment(ctx, tx, RoleAssignmentRecord{
			ID: uuid.New(), PersonID: personID, RoleCode: role, ScopeType: scope,
			CompanyID: companyID, CompanyOperationID: operationID, ValidFrom: validFrom,
		})
	})
}

func withAccess(t *testing.T, ctx context.Context, pool *pgxpool.Pool, access tenancy.Context, fn func(pgx.Tx) error) {
	t.Helper()
	if err := WithTenantTx(ctx, pool, access, func(ctx context.Context, tx pgx.Tx) error { return fn(tx) }); err != nil {
		t.Fatalf("tenant transaction (%s): %v", access.Mode, err)
	}
}

func testTenantIsolation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	repo := CompanyRepository{}
	withAccess(t, ctx, pool, tenancy.Tenant(f.CompanyA), func(tx pgx.Tx) error {
		company, err := repo.Get(ctx, tx, f.CompanyA)
		if err != nil || company.ID != f.CompanyA {
			return fmt.Errorf("company A not visible: %w", err)
		}
		_, err = repo.Get(ctx, tx, f.CompanyB)
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("company B leaked to A: %v", err)
		}
		return nil
	})
	withAccess(t, ctx, pool, tenancy.Tenant(f.CompanyB), func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.company`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("expected one company in B context, got %d", count)
		}
		return nil
	})

	withAccess(t, ctx, pool, tenancy.Tenant(f.CompanyA), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SAVEPOINT isolation_insert`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO platform.company (id, slug, legal_name)
			VALUES ($1, 'must-fail-b', 'Must Fail B')
		`, uuid.New())
		if err == nil {
			return errors.New("cross-tenant company insert unexpectedly succeeded")
		}
		if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT isolation_insert`); rollbackErr != nil {
			return rollbackErr
		}
		_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT isolation_insert`)
		return nil
	})
}

func testGlobalIdentityRLS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	withAccess(t, ctx, pool, tenancy.Tenant(f.CompanyA), func(tx pgx.Tx) error {
		if err := expectRLSRejection(ctx, tx, `
			INSERT INTO platform.person (id, legal_name)
			VALUES ($1, 'Tenant-created global identity')
		`, uuid.New()); err != nil {
			return fmt.Errorf("tenant person insert was not rejected: %w", err)
		}
		if err := expectRLSRejection(ctx, tx, `
			UPDATE platform.person SET legal_name = 'Cross-tenant mutation'
			WHERE id = $1
		`, f.PersonShared); err != nil {
			return fmt.Errorf("tenant person update was not rejected: %w", err)
		}
		if err := expectRLSRejection(ctx, tx, `
			INSERT INTO platform.user_account (id, person_id, status)
			VALUES ($1, $2, 'ACTIVE')
		`, uuid.New(), uuid.New()); err != nil {
			return fmt.Errorf("tenant user account insert was not rejected: %w", err)
		}
		if err := expectRLSRejection(ctx, tx, `
			UPDATE platform.user_account SET status = 'DISABLED'
			WHERE id = $1
		`, f.UserShared); err != nil {
			return fmt.Errorf("tenant user account update was not rejected: %w", err)
		}
		if err := expectRLSRejection(ctx, tx, `
			INSERT INTO platform.driver_profile (id, person_id, status)
			VALUES ($1, $2, 'ACTIVE')
		`, uuid.New(), f.PersonShared); err != nil {
			return fmt.Errorf("tenant driver profile insert was not rejected: %w", err)
		}
		if err := expectRLSRejection(ctx, tx, `
			UPDATE platform.driver_profile SET status = 'SUSPENDED'
			WHERE person_id = $1
		`, f.PersonShared); err != nil {
			return fmt.Errorf("tenant driver profile update was not rejected: %w", err)
		}
		return nil
	})

	withAccess(t, ctx, pool, tenancy.Tenant(f.CompanyB), func(tx pgx.Tx) error {
		var legalName, status, driverStatus string
		if err := tx.QueryRow(ctx, `SELECT legal_name FROM platform.person WHERE id = $1`, f.PersonShared).Scan(&legalName); err != nil {
			return err
		}
		if legalName != "Shared Person" {
			return fmt.Errorf("shared global identity was changed through A: %q", legalName)
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM platform.user_account WHERE id = $1`, f.UserShared).Scan(&status); err != nil {
			return err
		}
		if status != "ACTIVE" {
			return fmt.Errorf("shared account was changed through A: %q", status)
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM platform.driver_profile WHERE person_id = $1`, f.PersonShared).Scan(&driverStatus); err != nil {
			return err
		}
		if driverStatus != "ACTIVE" {
			return fmt.Errorf("shared driver profile was changed through A: %q", driverStatus)
		}
		return nil
	})
}

func expectRLSRejection(ctx context.Context, tx pgx.Tx, query string, args ...any) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT identity_rls_check`); err != nil {
		return err
	}
	result, statementErr := tx.Exec(ctx, query, args...)
	if statementErr == nil && result.RowsAffected() > 0 {
		_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT identity_rls_check`)
		_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT identity_rls_check`)
		return errors.New("statement unexpectedly succeeded")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT identity_rls_check`); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT identity_rls_check`)
	return nil
}

func testAuthorization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	auth := AuthorizationRepository{}
	allow := func(access tenancy.Context, request AuthorizationRequest, expected bool) {
		t.Helper()
		var decision AuthorizationDecision
		withAccess(t, ctx, pool, access, func(tx pgx.Tx) error {
			var err error
			decision, err = auth.Authorize(ctx, tx, request)
			return err
		})
		if decision.Allowed != expected {
			t.Fatalf("authorization %s: allowed=%v reason=%s expected=%v", request, decision.Allowed, decision.ReasonCode, expected)
		}
	}

	companyA := f.CompanyA
	companyB := f.CompanyB
	operationA := f.OperationA
	allow(tenancy.Tenant(companyA), AuthorizationRequest{Principal: Principal{UserAccountID: f.UserAAdmin, PersonID: f.PersonAAdmin, Active: true}, ScopeType: "COMPANY", CompanyID: &companyA, PermissionCode: "company.read"}, true)
	allow(tenancy.Tenant(companyA), AuthorizationRequest{Principal: Principal{UserAccountID: f.UserAAdmin, PersonID: f.PersonAAdmin, Active: true}, ScopeType: "COMPANY", CompanyID: &companyB, PermissionCode: "company.read"}, false)
	allow(tenancy.Tenant(companyA), AuthorizationRequest{Principal: Principal{UserAccountID: f.UserASupervisor, PersonID: f.PersonASupervisor, Active: true}, ScopeType: "COMPANY", CompanyID: &companyA, PermissionCode: "company.read"}, false)
	allow(tenancy.Tenant(companyA), AuthorizationRequest{Principal: Principal{UserAccountID: f.UserASupervisor, PersonID: f.PersonASupervisor, Active: true}, ScopeType: "OPERATION", CompanyID: &companyA, CompanyOperationID: &operationA, PermissionCode: "company.operation.read"}, true)
	allow(tenancy.Tenant(companyB), AuthorizationRequest{Principal: Principal{UserAccountID: f.UserShared, PersonID: f.PersonShared, Active: true}, ScopeType: "COMPANY", CompanyID: &companyB, PermissionCode: "company.read"}, true)

	global := AuthorizationRequest{Principal: Principal{UserAccountID: f.UserGlobal, PersonID: f.PersonGlobal, Active: true}, ScopeType: "PLATFORM", PermissionCode: "platform.company.create"}
	allow(tenancy.Platform(), global, true)
	identityManage := global
	identityManage.PermissionCode = "platform.identity.manage"
	allow(tenancy.Platform(), identityManage, true)

	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE platform.user_account SET status = 'DISABLED' WHERE id = $1`, f.UserGlobal)
		return err
	})
	allow(tenancy.Platform(), global, false)
	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE platform.user_account SET status = 'ACTIVE' WHERE id = $1`, f.UserGlobal)
		return err
	})
	nonexistentPerson := global
	nonexistentPerson.Principal.PersonID = uuid.New()
	allow(tenancy.Platform(), nonexistentPerson, false)
	nonexistentAccount := global
	nonexistentAccount.Principal.UserAccountID = uuid.New()
	allow(tenancy.Platform(), nonexistentAccount, false)
}

func testAuthorizedTransactionBoundary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	called := false
	decision, err := WithAuthorizedTenantTx(
		ctx,
		pool,
		tenancy.Tenant(f.CompanyA),
		AuthorizationRequest{
			Principal:      Principal{UserAccountID: f.UserADriver, PersonID: f.PersonADriver, Active: true},
			ScopeType:      "COMPANY",
			CompanyID:      ptrUUID(f.CompanyA),
			PermissionCode: "company.status.manage",
		},
		func(context.Context, pgx.Tx) error {
			called = true
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || called {
		t.Fatalf("unauthorized administrative transaction invoked callback: decision=%+v called=%v", decision, called)
	}

	called = false
	_, err = WithAuthorizedTenantTx(
		ctx,
		pool,
		tenancy.Context{},
		AuthorizationRequest{ScopeType: "COMPANY", CompanyID: ptrUUID(f.CompanyA), PermissionCode: "company.read"},
		func(context.Context, pgx.Tx) error {
			called = true
			return nil
		},
	)
	if err == nil || called {
		t.Fatalf("invalid context allowed administrative invocation: err=%v called=%v", err, called)
	}

	_, err = WithAuthorizedTenantTx(
		ctx,
		pool,
		tenancy.Tenant(f.CompanyA),
		AuthorizationRequest{ScopeType: "COMPANY", CompanyID: ptrUUID(f.CompanyB), PermissionCode: "company.read"},
		func(context.Context, pgx.Tx) error { return nil },
	)
	if err == nil {
		t.Fatal("mismatched tenant and authorization company were accepted")
	}
}

func testLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	now := time.Now().UTC()
	companyID := f.CompanyA
	operationID := f.OperationA
	var previousRole uuid.UUID
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT id FROM platform.role_assignment
			WHERE person_id = $1 AND role_code = 'SUPERVISOR' AND lifecycle_status = 'ACTIVE'
		`, f.PersonASupervisor).Scan(&previousRole); err != nil {
			return err
		}
		if err := RevokeRoleAssignment(ctx, tx, previousRole, now); err != nil {
			return err
		}
		if err := CreateRoleAssignment(ctx, tx, RoleAssignmentRecord{
			ID: uuid.New(), PersonID: f.PersonASupervisor, RoleCode: "SUPERVISOR", ScopeType: "OPERATION",
			CompanyID: &companyID, CompanyOperationID: &operationID, ValidFrom: now,
		}); err != nil {
			return err
		}
		validFrom := now.Add(-2 * time.Hour)
		validUntil := now.Add(-time.Hour)
		oldGrant := PermissionGrantRecord{ID: uuid.New(), PersonID: f.PersonADriver, PermissionCode: "company.read", ScopeType: "COMPANY", CompanyID: &companyID, ValidFrom: validFrom, ValidUntil: &validUntil}
		if err := CreatePermissionGrant(ctx, tx, oldGrant); err != nil {
			return err
		}
		if err := ExpirePermissionGrant(ctx, tx, oldGrant.ID, now); err != nil {
			return err
		}
		newGrant := oldGrant
		newGrant.ID = uuid.New()
		newGrant.ValidFrom = now
		newGrant.ValidUntil = nil
		return CreatePermissionGrant(ctx, tx, newGrant)
	})

	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		var active, historical int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.role_assignment WHERE person_id = $1 AND role_code = 'SUPERVISOR' AND lifecycle_status = 'ACTIVE'`, f.PersonASupervisor).Scan(&active); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.role_assignment WHERE person_id = $1 AND role_code = 'SUPERVISOR'`, f.PersonASupervisor).Scan(&historical); err != nil {
			return err
		}
		if active != 1 || historical != 2 {
			return fmt.Errorf("unexpected role history active=%d total=%d", active, historical)
		}
		var expired, grantRows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.permission_grant WHERE person_id = $1 AND lifecycle_status = 'EXPIRED'`, f.PersonADriver).Scan(&expired); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.permission_grant WHERE person_id = $1 AND permission_code = 'company.read'`, f.PersonADriver).Scan(&grantRows); err != nil {
			return err
		}
		if expired != 1 || grantRows != 2 {
			return fmt.Errorf("unexpected permission history expired=%d total=%d", expired, grantRows)
		}
		return nil
	})
}

func testStateInvariants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	auth := AuthorizationRepository{}
	companyID := f.CompanyA
	operationID := f.OperationA
	principal := Principal{UserAccountID: f.UserAAdmin, PersonID: f.PersonAAdmin, Active: true}
	setStatus := func(query string, args ...any) {
		t.Helper()
		withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, query, args...)
			return err
		})
	}
	check := func(expected bool) {
		t.Helper()
		var decision AuthorizationDecision
		withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
			var err error
			decision, err = auth.Authorize(ctx, tx, AuthorizationRequest{Principal: principal, ScopeType: "COMPANY", CompanyID: &companyID, PermissionCode: "company.read"})
			return err
		})
		if decision.Allowed != expected {
			t.Fatalf("expected company authorization=%v, got %v (%s)", expected, decision.Allowed, decision.ReasonCode)
		}
	}

	setStatus(`UPDATE platform.company_membership SET status = 'SUSPENDED' WHERE company_id = $1 AND person_id = $2`, companyID, f.PersonAAdmin)
	check(false)
	setStatus(`UPDATE platform.company_membership SET status = 'ACTIVE' WHERE company_id = $1 AND person_id = $2`, companyID, f.PersonAAdmin)
	check(true)

	setStatus(`UPDATE platform.company_operation SET status = 'SUSPENDED' WHERE id = $1`, operationID)
	var operationDecision AuthorizationDecision
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		var err error
		operationDecision, err = auth.Authorize(ctx, tx, AuthorizationRequest{Principal: Principal{UserAccountID: f.UserASupervisor, PersonID: f.PersonASupervisor, Active: true}, ScopeType: "OPERATION", CompanyID: &companyID, CompanyOperationID: &operationID, PermissionCode: "company.operation.read"})
		return err
	})
	if operationDecision.Allowed {
		t.Fatal("suspended company operation remained authorized")
	}
	setStatus(`UPDATE platform.company_operation SET status = 'ACTIVE' WHERE id = $1`, operationID)

	setStatus(`UPDATE platform.operation_membership SET status = 'SUSPENDED' WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3`, companyID, operationID, f.PersonASupervisor)
	var suspendedOperationDecision AuthorizationDecision
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		var err error
		suspendedOperationDecision, err = auth.Authorize(ctx, tx, AuthorizationRequest{Principal: Principal{UserAccountID: f.UserASupervisor, PersonID: f.PersonASupervisor, Active: true}, ScopeType: "OPERATION", CompanyID: &companyID, CompanyOperationID: &operationID, PermissionCode: "company.operation.read"})
		return err
	})
	if suspendedOperationDecision.Allowed {
		t.Fatal("suspended operation membership remained authorized")
	}
	setStatus(`UPDATE platform.operation_membership SET status = 'ACTIVE' WHERE company_id = $1 AND company_operation_id = $2 AND person_id = $3`, companyID, operationID, f.PersonASupervisor)

	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE platform.user_account SET status = 'DISABLED' WHERE id = $1`, f.UserAAdmin)
		return err
	})
	check(false)
	withAccess(t, ctx, pool, tenancy.Platform(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE platform.user_account SET status = 'ACTIVE' WHERE id = $1`, f.UserAAdmin)
		return err
	})
	check(true)
}

func testAuditAndPrivileges(t *testing.T, ctx context.Context, pool *pgxpool.Pool, adminDSN string, f foundationFixture) {
	companyID := f.CompanyA
	requestID := "audit-success-1"
	withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
		return WriteAuditEvent(ctx, tx, AuditEvent{ID: uuid.New(), CompanyID: &companyID, Action: "test.mutation", ResourceType: "test", RequestID: &requestID, Details: []byte(`{"safe":true}`)})
	})
	rollbackRequest := "audit-rollback-1"
	err := WithTenantTx(ctx, pool, tenancy.Tenant(companyID), func(ctx context.Context, tx pgx.Tx) error {
		if err := WriteAuditEvent(ctx, tx, AuditEvent{ID: uuid.New(), CompanyID: &companyID, Action: "test.rollback", ResourceType: "test", RequestID: &rollbackRequest}); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("forced rollback did not return an error")
	}

	deniedRequest := "deny-1"
	deniedEvent, err := NewDeniedAttempt(
		uuid.New(),
		Principal{UserAccountID: f.UserAAdmin, PersonID: f.PersonAAdmin, Active: true},
		AuthorizationRequest{ScopeType: "COMPANY", CompanyID: ptrUUID(uuid.New()), PermissionCode: "membership.manage"},
		"membership.manage",
		"company_membership",
		DeniedReasonPermissionDenied,
		&deniedRequest,
		nil,
		[]byte(`{"redacted":true}`),
	)
	if err != nil {
		t.Fatalf("build denied attempt: %v", err)
	}
	if err := RecordDeniedAttempt(ctx, pool, deniedEvent); err != nil {
		t.Fatalf("record denied attempt: %v", err)
	}
	// A retry of the same server-issued request is idempotent and cannot spam
	// duplicate rows, even when it uses a different event UUID.
	deniedEventRetry, err := NewDeniedAttempt(
		uuid.New(),
		Principal{UserAccountID: f.UserAAdmin, PersonID: f.PersonAAdmin, Active: true},
		AuthorizationRequest{ScopeType: "COMPANY", CompanyID: deniedEvent.requestedCompanyID, PermissionCode: "membership.manage"},
		"membership.manage",
		"company_membership",
		DeniedReasonPermissionDenied,
		&deniedRequest,
		nil,
		[]byte(`{"redacted":true}`),
	)
	if err != nil {
		t.Fatalf("build denied retry: %v", err)
	}
	if err := RecordDeniedAttempt(ctx, pool, deniedEventRetry); err != nil {
		t.Fatalf("record denied retry: %v", err)
	}
	if _, err := NewDeniedAttempt(uuid.New(), Principal{}, AuthorizationRequest{ScopeType: "PLATFORM"}, "x", "y", DeniedReasonCode("spoofed"), nil, nil, nil); err == nil {
		t.Fatal("unregistered denied reason was accepted")
	}

	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	var deniedCount, successCount, rollbackCount int
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM platform.admin_denied_attempt WHERE request_id = $1`, deniedRequest).Scan(&deniedCount); err != nil {
		t.Fatal(err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM platform.admin_audit_event WHERE request_id = $1`, requestID).Scan(&successCount); err != nil {
		t.Fatal(err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM platform.admin_audit_event WHERE request_id = $1`, rollbackRequest).Scan(&rollbackCount); err != nil {
		t.Fatal(err)
	}
	if deniedCount != 1 || successCount != 1 || rollbackCount != 0 {
		t.Fatalf("audit counts denied=%d success=%d rollback=%d", deniedCount, successCount, rollbackCount)
	}

	var superuser, bypassRLS, canReadDenied bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&superuser, &bypassRLS); err != nil {
		t.Fatal(err)
	}
	if superuser || bypassRLS {
		t.Fatalf("runtime role is too privileged superuser=%v bypassrls=%v", superuser, bypassRLS)
	}
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user, 'platform.admin_denied_attempt', 'SELECT')`).Scan(&canReadDenied); err != nil {
		t.Fatal(err)
	}
	if canReadDenied {
		t.Fatal("runtime can read denied attempts directly")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM platform.admin_audit_event`); err == nil {
		t.Fatal("runtime can delete audit events")
	}
}

func testOwnerForceRLS(t *testing.T, ctx context.Context, migratorDSN string) {
	migratorPool, err := pgxpool.New(ctx, migratorDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer migratorPool.Close()
	conn, err := migratorPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET ROLE tp_owner`); err != nil {
		t.Fatalf("assume owner role: %v", err)
	}
	var currentRole string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&currentRole); err != nil {
		t.Fatal(err)
	}
	if currentRole != "tp_owner" {
		t.Fatalf("expected tp_owner, got %s", currentRole)
	}
	var tableOwner string
	if err := conn.QueryRow(ctx, `
		SELECT pg_get_userbyid(c.relowner)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'platform' AND c.relname = 'company'
	`).Scan(&tableOwner); err != nil {
		t.Fatal(err)
	}
	if tableOwner != "tp_owner" {
		t.Fatalf("expected company table owner tp_owner, got %s", tableOwner)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM platform.company`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("owner/migrator bypassed FORCE RLS without context: count=%d", count)
	}
}

func testPoolContextSequence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f foundationFixture) {
	for _, companyID := range []uuid.UUID{f.CompanyA, f.CompanyB, f.CompanyA} {
		withAccess(t, ctx, pool, tenancy.Tenant(companyID), func(tx pgx.Tx) error {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.company`).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("expected one company in context, got %d", count)
			}
			return nil
		})
	}
	var tenantSetting, modeSetting string
	if err := pool.QueryRow(ctx, `SELECT current_setting('app.tenant_id', true), current_setting('app.access_mode', true)`).Scan(&tenantSetting, &modeSetting); err != nil {
		t.Fatal(err)
	}
	if tenantSetting != "" || modeSetting != "" {
		t.Fatalf("transaction-local context leaked into pool: tenant=%q mode=%q", tenantSetting, modeSetting)
	}
}

func ptrUUID(value uuid.UUID) *uuid.UUID { return &value }
