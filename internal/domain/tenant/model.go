package tenant

import (
	"errors"
	"strings"
)

type CompanyID string
type OperationType string

const (
	OperationTypeDrayage        OperationType = "DRAYAGE"
	OperationTypeTransportation OperationType = "TRANSPORTATION"
)

type Company struct {
	ID   CompanyID
	Name string
}

type CompanyOperation struct {
	ID           string
	CompanyID    CompanyID
	Operation    OperationType
	Status       string
	Capabilities []string
}

func (operation CompanyOperation) Validate() error {
	if strings.TrimSpace(operation.ID) == "" {
		return errors.New("company operation id is required")
	}
	if strings.TrimSpace(string(operation.CompanyID)) == "" {
		return errors.New("company id is required")
	}
	if strings.TrimSpace(string(operation.Operation)) == "" {
		return errors.New("operation type is required")
	}
	if operation.Status == "" {
		return errors.New("company operation status is required")
	}
	return nil
}
