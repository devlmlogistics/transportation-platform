package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"transportation-platform/internal/application/admin"
	"transportation-platform/internal/config"
	"transportation-platform/internal/platform/postgres"
)

func TestAdministrativeRoutesAreClosedWithoutAuthentication(t *testing.T) {
	router, err := NewRouter(config.Config{WebDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/companies", nil)
	request.Header.Set("X-User-ID", uuid.NewString())
	request.Header.Set("X-Person-ID", uuid.NewString())
	request.Header.Set("X-Tenant-ID", uuid.NewString())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("closed admin route status = %d body=%q", response.Code, response.Body.String())
	}
}

func TestAdminHandlerUsesOnlyInjectedTestBoundary(t *testing.T) {
	companyID := uuid.New()
	principal := postgres.Principal{UserAccountID: uuid.New(), PersonID: uuid.New(), Active: true}
	service := &fakeAdminService{company: postgres.CompanyRecord{ID: companyID, Slug: "acme", LegalName: "Acme"}}
	resolver := fakePrincipalResolver{principal: principal}
	handler, err := NewAdminHandler(service, resolver)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/companies/"+companyID.String(), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !service.getCompanyCalled {
		t.Fatalf("injected admin handler status=%d called=%v body=%q", response.Code, service.getCompanyCalled, response.Body.String())
	}

	deniedHandler, err := NewAdminHandler(service, fakePrincipalResolver{err: errors.New("no authenticated principal")})
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	deniedHandler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("resolver failure status = %d", response.Code)
	}
}

func TestAdminHandlerRejectsDriverSessionIntent(t *testing.T) {
	companyID := uuid.New()
	service := &fakeAdminService{company: postgres.CompanyRecord{ID: companyID}}
	handler, err := NewAdminHandler(service, fakePrincipalResolver{principal: postgres.Principal{
		UserAccountID: uuid.New(), PersonID: uuid.New(), Active: true, SessionPolicy: "DRIVER",
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/companies/"+companyID.String(), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.getCompanyCalled {
		t.Fatalf("driver session admin status=%d called=%v body=%q", response.Code, service.getCompanyCalled, response.Body.String())
	}
}

func TestAdminHandlerRejectsAssignmentScopeMismatchesBeforeService(t *testing.T) {
	companyID := uuid.New()
	otherCompanyID := uuid.New()
	principal := postgres.Principal{UserAccountID: uuid.New(), PersonID: uuid.New(), Active: true}
	service := &fakeAdminService{}
	handler, err := NewAdminHandler(service, fakePrincipalResolver{principal: principal})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body string
	}{
		{name: "company mismatch", body: `{"person_id":"00000000-0000-0000-0000-000000000001","role_code":"AUDITOR","scope_type":"COMPANY","company_id":"` + otherCompanyID.String() + `"}`},
		{name: "platform on tenant endpoint", body: `{"person_id":"00000000-0000-0000-0000-000000000001","role_code":"AUDITOR","scope_type":"PLATFORM"}`},
		{name: "company with operation id", body: `{"person_id":"00000000-0000-0000-0000-000000000001","role_code":"AUDITOR","scope_type":"COMPANY","company_operation_id":"00000000-0000-0000-0000-000000000002"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/companies/"+companyID.String()+"/role-assignments", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			if service.assignRoleCalled {
				t.Fatal("service was called for invalid URL/payload scope")
			}
		})
	}
	permissionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/companies/"+companyID.String()+"/permission-grants", bytes.NewBufferString(`{"person_id":"00000000-0000-0000-0000-000000000001","permission_code":"company.read","scope_type":"COMPANY","company_id":"`+otherCompanyID.String()+`"}`))
	permissionRequest.Header.Set("Content-Type", "application/json")
	permissionResponse := httptest.NewRecorder()
	handler.ServeHTTP(permissionResponse, permissionRequest)
	if permissionResponse.Code != http.StatusUnprocessableEntity || service.grantPermissionCalled {
		t.Fatalf("permission mismatch status=%d grant_called=%v body=%q", permissionResponse.Code, service.grantPermissionCalled, permissionResponse.Body.String())
	}
}

func TestAdminHandlerRejectsUnsupportedIdempotencyKey(t *testing.T) {
	companyID := uuid.New()
	principal := postgres.Principal{UserAccountID: uuid.New(), PersonID: uuid.New(), Active: true}
	service := &fakeAdminService{}
	handler, err := NewAdminHandler(service, fakePrincipalResolver{principal: principal})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/companies/"+companyID.String(), bytes.NewBufferString(`{"status":"SUSPENDED"}`))
	request.Header.Set("Idempotency-Key", "synthetic-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

type fakePrincipalResolver struct {
	principal postgres.Principal
	err       error
}

func (f fakePrincipalResolver) Resolve(context.Context, *http.Request) (postgres.Principal, error) {
	return f.principal, f.err
}

type fakeAdminService struct {
	company               postgres.CompanyRecord
	getCompanyCalled      bool
	assignRoleCalled      bool
	grantPermissionCalled bool
}

func (f *fakeAdminService) GetCompany(context.Context, postgres.Principal, uuid.UUID) (postgres.CompanyRecord, error) {
	f.getCompanyCalled = true
	return f.company, nil
}

func (f *fakeAdminService) CreateCompany(context.Context, postgres.Principal, admin.CompanyInput) (postgres.CompanyRecord, error) {
	return f.company, nil
}

func (f *fakeAdminService) UpdateCompany(context.Context, postgres.Principal, uuid.UUID, admin.CompanyUpdateInput) error {
	return nil
}

func (f *fakeAdminService) ListOperations(context.Context, postgres.Principal, uuid.UUID) ([]admin.OperationView, error) {
	return nil, nil
}

func (f *fakeAdminService) EnableOperation(context.Context, postgres.Principal, uuid.UUID, string) (admin.OperationView, error) {
	return admin.OperationView{}, nil
}

func (f *fakeAdminService) SetOperationStatus(context.Context, postgres.Principal, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeAdminService) ListMemberships(context.Context, postgres.Principal, uuid.UUID) ([]admin.MembershipView, error) {
	return nil, nil
}

func (f *fakeAdminService) SetMembershipStatus(context.Context, postgres.Principal, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeAdminService) SetOperationMembership(context.Context, postgres.Principal, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeAdminService) AssignRole(context.Context, postgres.Principal, admin.RoleAssignmentInput) error {
	f.assignRoleCalled = true
	return nil
}

func (f *fakeAdminService) RevokeRole(context.Context, postgres.Principal, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeAdminService) GrantPermission(context.Context, postgres.Principal, admin.PermissionGrantInput) error {
	f.grantPermissionCalled = true
	return nil
}

func (f *fakeAdminService) RevokePermission(context.Context, postgres.Principal, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeAdminService) ListAudit(context.Context, postgres.Principal, uuid.UUID) ([]admin.AuditView, error) {
	return nil, nil
}
