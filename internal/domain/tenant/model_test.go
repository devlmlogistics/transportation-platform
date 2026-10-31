package tenant

import "testing"

func TestCompanyOperationValidate(t *testing.T) {
	operation := CompanyOperation{
		ID:        "company-operation-1",
		CompanyID: "company-1",
		Operation: OperationTypeDrayage,
		Status:    "ENABLED",
	}
	if err := operation.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
