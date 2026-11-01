package httpapi

import (
	"log/slog"
	"net/http"

	"transportation-platform/internal/config"
)

func NewRouter(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	return NewRouterWithDependencies(cfg, logger, RouterDependencies{})
}

type RouterDependencies struct {
	Auth  OnlineAuthHTTPService
	Admin AdminService
}

func NewRouterWithDependencies(cfg config.Config, logger *slog.Logger, dependencies RouterDependencies) (http.Handler, error) {
	if logger == nil {
		return nil, errInvalidRouter("logger is required")
	}
	if cfg.WebDir == "" {
		return nil, errInvalidRouter("WEB_DIR is required")
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", onlyMethod(http.MethodGet, healthHandler{}))
	var authHandler http.Handler = ClosedAuthHandler()
	var authSecurity *AuthHandler
	if cfg.AuthOnlineEnabled && dependencies.Auth != nil {
		configured, err := NewAuthHandler(dependencies.Auth, cfg.AllowedOrigins, cfg.AuthCookieSecure)
		if err != nil {
			return nil, err
		}
		authHandler = configured
		authSecurity = configured
	}
	mux.Handle("/api/v1/auth/", authHandler)
	adminHandler := ClosedAdminHandler()
	if cfg.AdminHTTPEnabled && dependencies.Admin != nil && authSecurity != nil {
		resolver := SessionPrincipalResolver{Service: dependencies.Auth}
		configured, err := NewAdminHandler(dependencies.Admin, resolver)
		if err != nil {
			return nil, err
		}
		adminHandler = protectAdministrativeMutations(configured, authSecurity)
	}
	// Administrative routes remain closed until both explicit configuration and
	// a real session resolver are present. Identity headers are never accepted.
	mux.Handle("/api/v1/admin/", adminHandler)
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
