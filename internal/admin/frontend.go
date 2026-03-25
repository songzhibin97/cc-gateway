package admin

import (
	"io/fs"
	"net/http"
	"strings"

	frontend "github.com/songzhibin97/cc-gateway/web"
)

// frontendHandler serves the embedded admin SPA and falls back to index.html
// for client-side routes.
func frontendHandler() http.HandlerFunc {
	stripped, err := fs.Sub(frontend.DistFS, "dist")
	if err != nil {
		return http.NotFound
	}

	fileServer := http.FileServer(http.FS(stripped))

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if path != "index.html" {
			if f, err := stripped.Open(path); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		index, err := stripped.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_ = index.Close()

		r = r.Clone(r.Context())
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}
