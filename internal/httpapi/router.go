package httpapi

import (
	"log/slog"
	"net/http"

	"transportation-platform/internal/config"
)

func NewRouter(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	if logger == nil {
		return nil, errInvalidRouter("logger is required")
	}
	if cfg.WebDir == "" {
		return nil, errInvalidRouter("WEB_DIR is required")
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", onlyMethod(http.MethodGet, healthHandler{}))
	// Administrative routes remain closed until a real authentication boundary
	// is configured. Tests instantiate AdminHandler with an injected resolver;
	// production never accepts identity headers or synthetic principals.
	mux.Handle("/api/v1/admin/", ClosedAdminHandler())
	mux.Handle("/api/", onlyMethod(http.MethodGet, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "API endpoint not found.")
	})))
	mux.Handle("/", staticFiles(cfg.WebDir))

	return withRecovery(withRequestID(withLogging(mux, logger))), nil
}

type routerError string

func (e routerError) Error() string {
	return string(e)
}

func errInvalidRouter(message string) error {
	return routerError(message)
}

func onlyMethod(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type healthHandler struct{}

func (healthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{`status`: `ok`})
}
