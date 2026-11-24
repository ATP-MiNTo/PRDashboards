package mw

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

var hashSource = regexp.MustCompile(`^'sha256-[A-Za-z0-9+/]+={0,2}'$`)

// CSP builds the Content-Security-Policy value from the inline script hashes of the web build.
// With no hashes, scripts are limited to 'self'.
func CSP(scriptHashes []string) string {
	script := "script-src 'self'"
	if len(scriptHashes) > 0 {
		script += " " + strings.Join(scriptHashes, " ")
	}
	return "default-src 'none'; " + script + "; style-src 'self'; " +
		"img-src 'self' data: https://avatars.githubusercontent.com; font-src 'self'; " +
		"connect-src 'self'; manifest-src 'self'; worker-src 'self'; frame-ancestors 'none'; " +
		"base-uri 'none'; form-action 'self'"
}

// LoadScriptHashes reads the inline script hashes written by the web build to csp.json. A missing
// or malformed file yields no hashes. Entries that are not sha256 source expressions are dropped.
func LoadScriptHashes(fsys fs.FS) []string {
	if fsys == nil {
		return nil
	}
	data, err := fs.ReadFile(fsys, "csp.json")
	if err != nil {
		return nil
	}
	var doc struct {
		ScriptHashes []string `json:"scriptHashes"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	hashes := doc.ScriptHashes[:0]
	for _, h := range doc.ScriptHashes {
		if hashSource.MatchString(h) {
			hashes = append(hashes, h)
		}
	}
	return hashes
}

// SecurityHeaders sets the response headers of docs/TECH_SPEC.md section 8.5 on every response.
func SecurityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			if Protected(r) {
				h.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}
