package httpapi

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var versionedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.`)

func init() {
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".mjs", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
}

func staticFiles(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}

		requestPath := filepath.FromSlash(strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/"))
		if requestPath == "." || requestPath == "" {
			requestPath = "index.html"
		}
		if requestPath == ".." || strings.HasPrefix(requestPath, ".."+string(filepath.Separator)) {
			writeError(w, r, http.StatusNotFound, "not_found", "Resource not found.")
			return
		}

		candidate := filepath.Join(root, requestPath)
		file, err := os.Open(candidate)
		if err != nil && os.IsNotExist(err) && wantsHTML(r) && filepath.Ext(requestPath) == "" {
			candidate = filepath.Join(root, "index.html")
			file, err = os.Open(candidate)
		}
		if err != nil {
			writeError(w, r, http.StatusNotFound, "not_found", "Resource not found.")
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, r, http.StatusNotFound, "not_found", "Resource not found.")
			return
		}
		if filepath.Base(candidate) == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if versionedAsset.MatchString(filepath.Base(candidate)) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if contentType := mime.TypeByExtension(filepath.Ext(candidate)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}

func isAPIPath(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") ||
		requestPath == "/healthz" || strings.HasPrefix(requestPath, "/healthz/")
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
