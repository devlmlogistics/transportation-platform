package auth

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type DriverManagementInput struct {
	GivenName    string      `json:"given_name"`
	FamilyName   string      `json:"family_name"`
	Identifier   string      `json:"identifier"`
	Phone        string      `json:"phone"`
	OperationIDs []uuid.UUID `json:"operation_ids"`
	PersonID     uuid.UUID   `json:"person_id"`
	Status       string      `json:"status"`
	Version      int64       `json:"version"`
	Search       string      `json:"search"`
}

type DriverManagementResult struct {
	Status    string          `json:"status"`
	PersonID  *uuid.UUID      `json:"person_id,omitempty"`
	RequestID *uuid.UUID      `json:"request_id,omitempty"`
	Drivers   json.RawMessage `json:"drivers,omitempty"`
}

type DriverManagementStore interface {
	ManageCompanyDrivers(context.Context, []byte, uuid.UUID, string, DriverManagementInput, uuid.UUID) (DriverManagementResult, error)
}

type DriverManagementService struct{ Store DriverManagementStore }

var driverIdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+@-]{2,319}$`)
var driverPhonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func (s *DriverManagementService) Manage(ctx context.Context, token []byte, company uuid.UUID, action string, input DriverManagementInput, command uuid.UUID) (DriverManagementResult, error) {
	if s == nil || s.Store == nil {
		return DriverManagementResult{}, errors.New("driver management unavailable")
	}
	if len(token) != 32 || company == uuid.Nil {
		return DriverManagementResult{}, ErrInvalidInput
	}
	input.GivenName = strings.TrimSpace(input.GivenName)
	input.FamilyName = strings.TrimSpace(input.FamilyName)
	input.Identifier = strings.ToLower(strings.TrimSpace(input.Identifier))
	input.Phone = strings.TrimSpace(input.Phone)
	input.Search = strings.TrimSpace(input.Search)
	if len(input.Search) > 100 || len(input.OperationIDs) > 20 {
		return DriverManagementResult{}, ErrInvalidInput
	}
	for _, id := range input.OperationIDs {
		if id == uuid.Nil {
			return DriverManagementResult{}, ErrInvalidInput
		}
	}
	switch action {
	case "list":
	case "create":
		if command == uuid.Nil || len(input.GivenName) == 0 || len(input.GivenName) > 100 || len(input.FamilyName) == 0 || len(input.FamilyName) > 100 || !driverIdentifierPattern.MatchString(input.Identifier) || !driverPhonePattern.MatchString(input.Phone) {
			return DriverManagementResult{}, ErrInvalidInput
		}
	case "update":
		if command == uuid.Nil || input.PersonID == uuid.Nil || input.Version < 0 || (input.Status != "ACTIVE" && input.Status != "SUSPENDED") {
			return DriverManagementResult{}, ErrInvalidInput
		}
	default:
		return DriverManagementResult{}, ErrInvalidInput
	}
	if input.OperationIDs == nil {
		input.OperationIDs = []uuid.UUID{}
	}
	return s.Store.ManageCompanyDrivers(ctx, HashSessionToken(token), company, action, input, command)
}
