package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, nil, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	requestID := w.Header().Get("X-Request-ID")
	if requestID == "" && r != nil {
		requestID = r.Header.Get("X-Request-ID")
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":       code,
			"message":    message,
			"request_id": requestID,
		},
	})
}
