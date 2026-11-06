package postgres

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"transportation-platform/internal/application/auth"
)

func (r *OnlineAuthRepository) ManageCompanyDrivers(ctx context.Context, tokenHash []byte, company uuid.UUID, action string, input auth.DriverManagementInput, command uuid.UUID) (auth.DriverManagementResult, error) {
	var result auth.DriverManagementResult
	data, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	var raw []byte
	err = r.authPool.QueryRow(ctx, `SELECT platform.manage_company_drivers($1,$2,$3,$4::jsonb,$5)`, tokenHash, company, action, data, command).Scan(&raw)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
