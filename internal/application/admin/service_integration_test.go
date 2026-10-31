package admin

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
	"transportation-platform/internal/platform/postgres"
	"transportation-platform/internal/platform/tenancy"
)

// This suite is intentionally opt-in and uses a database dedicated to the
// administrative application tests. It never targets lmlogistics.
func TestAdminServicePostgres16(t *testing.T) {
	runtimeDSN := os.Getenv("TEST_ADMIN_DATABASE_URL")
	migratorDSN := os.Getenv("TEST_ADMIN_DATABASE_MIGRATOR_URL")
	adminDSN := os.Getenv("TEST_ADMIN_DATABASE_ADMIN_URL")
	if runtimeDSN == "" || migratorDSN == "" || adminDSN == "" {
		t.Skip("set TEST_ADMIN_DATABASE_URL, TEST_ADMIN_DATABASE_MIGRATOR_URL and TEST_ADMIN_DATABASE_ADMIN_URL for administrative PostgreSQL integration tests")
	}

	ctx := context.Background()
	migratorDB, err := migrations.Open(ctx, migratorDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer migratorDB.Close()

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	prepareAdminSchema(t, ctx, migratorDB, adminDB)
	fixture := seedAdminFixture(t, ctx, adminDB)

	pool, err := pgxpool.New(ctx, runtimeDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	service := NewService(pool)
	service.Clock = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }
	principalA := postgres.Principal{UserAccountID: fixture.UserAAdmin, PersonID: fixture.PersonAAdmin, Active: true}
	principalB := postgres.Principal{UserAccountID: fixture.UserBAdmin, PersonID: fixture.PersonBAdmin, Active: true}

	t.Run("delegation_policy", func(t *testing.T) {
		if err := service.AssignRole(ctx, principalA, RoleAssignmentInput{PersonID: fixture.PersonATarget, RoleCode: "ADMIN_OPERATOR", ScopeType: "COMPANY", CompanyID: &fixture.CompanyA}); err != nil {
			t.Fatalf("allowed delegated role: %v", err)
		}
		if err := service.AssignRole(ctx, principalA, RoleAssignmentInput{PersonID: fixture.PersonATarget, RoleCode: "COMPANY_ADMIN", ScopeType: "COMPANY", CompanyID: &fixture.CompanyA}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("company admin elevation error=%v, want ErrForbidden", err)
		}
		if err := service.AssignRole(ctx, principalA, RoleAssignmentInput{PersonID: fixture.PersonATarget, RoleCode: "AUDITOR", ScopeType: "OPERATION", CompanyID: &fixture.CompanyA, CompanyOperationID: &fixture.OperationA}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("operation delegation error=%v, want ErrForbidden", err)
		}
		if err := service.GrantPermission(ctx, principalA, PermissionGrantInput{PersonID: fixture.PersonATarget, PermissionCode: "company.read", ScopeType: "COMPANY", CompanyID: &fixture.CompanyA}); err != nil {
			t.Fatalf("allowed read grant: %v", err)
		}
		if err := service.GrantPermission(ctx, principalA, PermissionGrantInput{PersonID: fixture.PersonATarget, PermissionCode: "role.assignment.manage", ScopeType: "COMPANY", CompanyID: &fixture.CompanyA}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("management permission elevation error=%v, want ErrForbidden", err)
		}
	})

	t.Run("tenant_isolation_and_memberships", func(t *testing.T) {
		if _, err := service.GetCompany(ctx, principalB, fixture.CompanyA); !errors.Is(err, ErrForbidden) {
			t.Fatalf("cross-tenant company access error=%v, want ErrForbidden", err)
		}
		if err := service.SetOperationMembership(ctx, principalA, fixture.CompanyA, fixture.OperationA, fixture.PersonATarget, "SUSPENDED"); err != nil {
			t.Fatalf("suspend operation membership: %v", err)
		}
		if err := service.SetOperationMembership(ctx, principalA, fixture.CompanyA, fixture.OperationB, fixture.PersonATarget, "SUSPENDED"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign operation error=%v, want ErrNotFound", err)
		}
	})

	t.Run("revoke_reassign_and_idempotent_conflict", func(t *testing.T) {
		assignmentID := queryRoleAssignmentID(t, ctx, pool, fixture.CompanyA, fixture.PersonATarget, "ADMIN_OPERATOR")
		if err := service.RevokeRole(ctx, principalA, fixture.CompanyA, assignmentID); err != nil {
			t.Fatalf("revoke role: %v", err)
		}
		if err := service.AssignRole(ctx, principalA, RoleAssignmentInput{PersonID: fixture.PersonATarget, RoleCode: "ADMIN_OPERATOR", ScopeType: "COMPANY", CompanyID: &fixture.CompanyA}); err != nil {
			t.Fatalf("reassign role: %v", err)
		}
		beforeAudit := countAuditEvents(t, ctx, adminDB, fixture.UserAAdmin)
		if err := service.AssignRole(ctx, principalA, RoleAssignmentInput{PersonID: fixture.PersonATarget, RoleCode: "ADMIN_OPERATOR", ScopeType: "COMPANY", CompanyID: &fixture.CompanyA}); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate active role error=%v, want ErrConflict", err)
		}
		if afterAudit := countAuditEvents(t, ctx, adminDB, fixture.UserAAdmin); afterAudit != beforeAudit {
			t.Fatalf("failed duplicate mutation wrote audit: before=%d after=%d", beforeAudit, afterAudit)
		}
		var active, historical int
		if err := adminDB.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE lifecycle_status = 'ACTIVE'), count(*) FROM platform.role_assignment WHERE person_id = $1 AND role_code = 'ADMIN_OPERATOR'`, fixture.PersonATarget).Scan(&active, &historical); err != nil {
			t.Fatal(err)
		}
		if active != 1 || historical != 2 {
			t.Fatalf("role lifecycle active=%d historical=%d, want 1/2", active, historical)
		}
	})

	t.Run("suspended_account_and_audit", func(t *testing.T) {
		before := countAuditEvents(t, ctx, adminDB, fixture.UserAAdmin)
		if _, err := adminDB.ExecContext(ctx, `UPDATE platform.user_account SET status = 'DISABLED' WHERE id = $1`, fixture.UserAAdmin); err != nil {
			t.Fatal(err)
		}
		if err := service.SetMembershipStatus(ctx, principalA, fixture.CompanyA, fixture.CompanyMembershipID, "SUSPENDED"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("disabled account error=%v, want ErrForbidden", err)
		}
		if _, err := adminDB.ExecContext(ctx, `UPDATE platform.user_account SET status = 'ACTIVE' WHERE id = $1`, fixture.UserAAdmin); err != nil {
			t.Fatal(err)
		}
		after := countAuditEvents(t, ctx, adminDB, fixture.UserAAdmin)
		if after != before {
			t.Fatalf("denied mutation wrote success audit: before=%d after=%d", before, after)
		}
		var denied int
		if err := adminDB.QueryRowContext(ctx, `SELECT count(*) FROM platform.admin_denied_attempt WHERE actor_user_account_id = $1`, fixture.UserAAdmin).Scan(&denied); err != nil {
			t.Fatal(err)
		}
		if denied == 0 {
			t.Fatal("expected denied attempt audit")
		}
	})
}

type adminFixture struct {
	CompanyA, CompanyB          uuid.UUID
	OperationA, OperationB      uuid.UUID
	PersonAAdmin, PersonATarget uuid.UUID
	PersonBAdmin                uuid.UUID
	UserAAdmin, UserBAdmin      uuid.UUID
	CompanyMembershipID         uuid.UUID
}

func prepareAdminSchema(t *testing.T, ctx context.Context, db, adminDB *sql.DB) {
	t.Helper()
	directory, err := filepath.Abs(filepath.Join("..", "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if version, versionErr := migrations.Version(ctx, db); versionErr == nil && version > 0 {
		if _, err := adminDB.ExecContext(ctx, `
			TRUNCATE TABLE
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
		if err := migrations.DownTo(ctx, db, directory, 0); err != nil {
			t.Fatalf("reset migrations: %v", err)
		}
	}
	if err := migrations.Up(ctx, db, directory); err != nil {
		t.Fatalf("migrations up: %v", err)
	}
}

func seedAdminFixture(t *testing.T, ctx context.Context, db *sql.DB) adminFixture {
	t.Helper()
	f := adminFixture{
		CompanyA:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		CompanyB:            uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		OperationA:          uuid.MustParse("11111111-1111-1111-1111-000000000001"),
		OperationB:          uuid.MustParse("22222222-2222-2222-2222-000000000001"),
		PersonAAdmin:        uuid.MustParse("11111111-1111-1111-1111-100000000001"),
		PersonATarget:       uuid.MustParse("11111111-1111-1111-1111-100000000002"),
		PersonBAdmin:        uuid.MustParse("22222222-2222-2222-2222-100000000001"),
		UserAAdmin:          uuid.MustParse("11111111-1111-1111-1111-200000000001"),
		UserBAdmin:          uuid.MustParse("22222222-2222-2222-2222-200000000001"),
		CompanyMembershipID: uuid.MustParse("11111111-1111-1111-1111-300000000001"),
	}
	queries := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO platform.company (id, slug, legal_name) VALUES ($1, $2, $3), ($4, $5, $6)`, []any{f.CompanyA, "admin-test-a", "Admin Test A", f.CompanyB, "admin-test-b", "Admin Test B"}},
		{`INSERT INTO platform.company_operation (id, company_id, operation_type_code) VALUES ($1, $2, 'DRAYAGE'), ($3, $4, 'TRANSPORTATION')`, []any{f.OperationA, f.CompanyA, f.OperationB, f.CompanyB}},
		{`INSERT INTO platform.person (id, legal_name) VALUES ($1, 'A Admin'), ($2, 'A Target'), ($3, 'B Admin')`, []any{f.PersonAAdmin, f.PersonATarget, f.PersonBAdmin}},
		{`INSERT INTO platform.user_account (id, person_id, status) VALUES ($1, $2, 'ACTIVE'), ($3, $4, 'ACTIVE')`, []any{f.UserAAdmin, f.PersonAAdmin, f.UserBAdmin, f.PersonBAdmin}},
		{`INSERT INTO platform.company_membership (id, company_id, person_id, status) VALUES ($1, $2, $3, 'ACTIVE'), ($4, $5, $6, 'ACTIVE'), ($7, $8, $9, 'ACTIVE')`, []any{f.CompanyMembershipID, f.CompanyA, f.PersonAAdmin, uuid.New(), f.CompanyA, f.PersonATarget, uuid.New(), f.CompanyB, f.PersonBAdmin}},
		{`INSERT INTO platform.operation_membership (id, company_id, company_operation_id, person_id, status) SELECT $1, $2, $3, person_id, 'ACTIVE' FROM platform.company_membership WHERE id = $4`, []any{uuid.New(), f.CompanyA, f.OperationA, f.CompanyMembershipID}},
		{`INSERT INTO platform.role_assignment (id, person_id, role_code, scope_type, company_id, valid_from) VALUES ($1, $2, 'COMPANY_ADMIN', 'COMPANY', $3, now() - interval '1 hour'), ($4, $5, 'COMPANY_ADMIN', 'COMPANY', $6, now() - interval '1 hour')`, []any{uuid.New(), f.PersonAAdmin, f.CompanyA, uuid.New(), f.PersonBAdmin, f.CompanyB}},
	}
	for _, item := range queries {
		if _, err := db.ExecContext(ctx, item.query, item.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return f
}

func queryRoleAssignmentID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID, personID uuid.UUID, roleCode string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := postgres.WithTenantTx(ctx, pool, tenancy.Tenant(companyID), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM platform.role_assignment WHERE company_id = $1 AND person_id = $2 AND role_code = $3 AND lifecycle_status = 'ACTIVE'`, companyID, personID, roleCode).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func countAuditEvents(t *testing.T, ctx context.Context, db *sql.DB, actorID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM platform.admin_audit_event WHERE actor_user_account_id = $1`, actorID).Scan(&count); err != nil {
		t.Fatal(fmt.Errorf("count audit: %w", err))
	}
	return count
}
