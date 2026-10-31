package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"transportation-platform/internal/config"
)

func TestHealthzAndRequestID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"), "<html>platform</html>")
	router, err := NewRouter(config.Config{WebDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d", response.Code)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id")
	}
	if !strings.Contains(response.Body.String(), "status") {
		t.Fatalf("health body = %q", response.Body.String())
	}
}

func TestStaticSPAFallbackAndUnknownAPI(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"), "<html>platform-shell</html>")
	writeFile(t, filepath.Join(root, "assets", "app-AbCdEf123.js"), "console.log('platform')")
	router, err := NewRouter(config.Config{WebDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/future-route"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "text/html")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "<html>platform-shell</html>" {
			t.Fatalf("%s = %d %q", path, response.Code, response.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "platform-shell") {
		t.Fatalf("API fallback = %d %q", response.Code, response.Body.String())
	}
}

func TestInvalidMethodReturnsStructuredError(t *testing.T) {
	router, err := NewRouter(config.Config{WebDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), "method_not_allowed") {
		t.Fatalf("method response = %d %q", response.Code, response.Body.String())
	}
}

func writeFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
