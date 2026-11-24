package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const notBuiltPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>PR Dashboards</title></head>
<body><p>The UI is not built. Run scripts/start.sh.</p></body>
</html>
`

// NewStatic serves the exported UI. Directories without an index.html are not listed, csp.json is
// never served, and unknown paths get the export's 404.html when present.
func NewStatic(fsys fs.FS) http.Handler {
	if fsys == nil {
		return notBuilt()
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return notBuilt()
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		if !servable(fsys, name) {
			notFound(fsys, w, r)
			return
		}
		if strings.HasPrefix(name, "_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func servable(fsys fs.FS, name string) bool {
	if name == "csp.json" {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part != "." && strings.HasPrefix(part, ".") {
			return false
		}
	}
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err = fs.Stat(fsys, path.Join(name, "index.html"))
		return err == nil
	}
	return true
}

func notFound(fsys fs.FS, w http.ResponseWriter, r *http.Request) {
	page, err := fs.ReadFile(fsys, "404.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(page)
	}
}

func notBuilt() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(notBuiltPage))
		}
	})
}
