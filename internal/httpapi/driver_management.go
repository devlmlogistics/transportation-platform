package httpapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"transportation-platform/internal/application/auth"
)

type DriverManagementHTTPService interface {
	Manage(context.Context, []byte, uuid.UUID, string, auth.DriverManagementInput, uuid.UUID) (auth.DriverManagementResult, error)
}
type DriverManagementHandler struct {
	Service  DriverManagementHTTPService
	Security *AuthHandler
}

func (h DriverManagementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var token []byte
	var ok bool
	if r.Method == http.MethodGet {
		token, _, ok = h.Security.session(w, r)
	} else {
		token, _, ok = h.Security.requireMutationSession(w, r)
	}
	if !ok {
		return
	}
	company, err := uuid.Parse(r.URL.Query().Get("company_id"))
	if err != nil || company == uuid.Nil {
		writeError(w, r, 400, "invalid_input", "A company is required.")
		return
	}
	action := "list"
	var command uuid.UUID
	var input auth.DriverManagementInput
	switch r.Method {
	case http.MethodGet:
		input.Search = r.URL.Query().Get("search")
	case http.MethodPost, http.MethodPatch:
		command, err = uuid.Parse(r.Header.Get("Idempotency-Key"))
		if err != nil || command == uuid.Nil {
			writeError(w, r, 400, "invalid_input", "A UUID Idempotency-Key is required.")
			return
		}
		if decodeJSON(w, r, &input) != nil {
			return
		}
		action = "create"
		if r.Method == http.MethodPatch {
			action = "update"
		}
	default:
		writeError(w, r, 405, "method_not_allowed", "Method not allowed.")
		return
	}
	result, err := h.Service.Manage(r.Context(), token, company, action, input, command)
	if errors.Is(err, auth.ErrInvalidInput) {
		writeError(w, r, 422, "invalid_input", "Check the driver fields.")
		return
	}
	if err != nil {
		writeError(w, r, 503, "drivers_unavailable", "Driver management is unavailable.")
		return
	}
	switch result.Status {
	case "FORBIDDEN":
		writeError(w, r, 403, "driver_management_forbidden", "Driver management is not authorized.")
	case "INVALID":
		writeError(w, r, 422, "invalid_input", "Check the driver fields.")
	case "CONFLICT":
		writeError(w, r, 409, "driver_conflict", "The driver or version conflicts with an existing record.")
	case "RATE_LIMITED":
		writeError(w, r, 429, "driver_rate_limited", "Please retry later.")
	case "NOT_FOUND":
		writeError(w, r, 404, "driver_not_found", "Driver not found.")
	case "REVIEW_REQUIRED":
		writeJSON(w, 202, result)
	case "CREATED":
		writeJSON(w, 201, result)
	case "OK", "UPDATED":
		writeJSON(w, 200, result)
	default:
		writeError(w, r, 503, "drivers_unavailable", "Driver management is unavailable.")
	}
}
