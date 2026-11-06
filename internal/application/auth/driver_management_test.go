package auth

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

type managementStoreStub struct {
	calls int
	input DriverManagementInput
}

func (s *managementStoreStub) ManageCompanyDrivers(_ context.Context, _ []byte, _ uuid.UUID, _ string, i DriverManagementInput, _ uuid.UUID) (DriverManagementResult, error) {
	s.calls++
	s.input = i
	return DriverManagementResult{Status: "CREATED"}, nil
}
func TestDriverManagementValidation(t *testing.T) {
	store := &managementStoreStub{}
	service := DriverManagementService{Store: store}
	input := DriverManagementInput{GivenName: " Test ", FamilyName: " Driver ", Identifier: " DRIVER@EXAMPLE.TEST ", Phone: "+56912345678"}
	if _, err := service.Manage(context.Background(), make([]byte, 32), uuid.New(), "create", input, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if store.input.Identifier != "driver@example.test" || store.input.GivenName != "Test" {
		t.Fatal("identity normalization failed")
	}
	input.Phone = "not-a-phone"
	if _, err := service.Manage(context.Background(), make([]byte, 32), uuid.New(), "create", input, uuid.New()); err == nil {
		t.Fatal("invalid phone accepted")
	}
	if store.calls != 1 {
		t.Fatal("invalid input reached store")
	}
}
