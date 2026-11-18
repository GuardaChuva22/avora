// Package webui serves the Avora app (web/), built into static/ by
// `npm run build` and embedded in the server binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:static
var files embed.FS

// Built reports whether the app was built into the binary.
func Built() bool {
	_, err := fs.Stat(files, "static/index.html")
	return err == nil
}

const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; base-uri 'none'; " +
	"form-action 'none'; frame-ancestors 'none'"

// Handler serves the app. Unknown paths get index.html (client-side routes);
// API paths never do, so a typo in /v1 stays a 404.
func Handler() http.Handler {
	root, _ := fs.Sub(files, "static")
	static := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		if !Built() {
			http.Error(w, "Avora app not built (run npm run build in web/)", http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			name = "index.html"
		}
		switch {
		case strings.HasPrefix(name, "assets/"):
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			h.Set("Cache-Control", "no-cache")
		}
		static.ServeHTTP(w, r)
	})
}
