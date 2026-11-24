package api_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticServing(t *testing.T) {
	g := newRig(t, webFS())

	w := g.do(http.MethodGet, "/", "")
	if w.Code != http.StatusOK || w.Body.String() != "<html>home</html>" {
		t.Fatalf("/: %d %q", w.Code, w.Body)
	}
	if w := g.do(http.MethodHead, "/", ""); w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("HEAD /: %d %q", w.Code, w.Body)
	}
	if w := g.do(http.MethodGet, "/auth/launch/", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "launch") {
		t.Fatalf("/auth/launch/: %d %q", w.Code, w.Body)
	}
	if w := g.do(http.MethodGet, "/auth/launch", ""); w.Code != http.StatusMovedPermanently || !strings.HasSuffix(w.Header().Get("Location"), "launch/") {
		t.Fatalf("/auth/launch: %d Location %q", w.Code, w.Header().Get("Location"))
	}
	if w := g.do(http.MethodGet, "/_next/static/a.js", ""); w.Code != http.StatusOK ||
		w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestStaticNotFound(t *testing.T) {
	g := newRig(t, webFS())
	for _, p := range []string{"/missing", "/auth/missing/", "/csp.json", "/_next/static/", "/_next", "/.hidden"} {
		w := g.do(http.MethodGet, p, "")
		if w.Code != http.StatusNotFound || w.Body.String() != "<html>missing</html>" {
			t.Errorf("%s: %d %q", p, w.Code, w.Body)
		}
	}
	if w := g.do(http.MethodHead, "/missing", ""); w.Code != http.StatusNotFound || w.Body.Len() != 0 {
		t.Errorf("HEAD /missing: %d %q", w.Code, w.Body)
	}
	if w := g.do(http.MethodPost, "/", "{}"); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /: %d Allow %q", w.Code, w.Header().Get("Allow"))
	}
}

func TestStaticNotFoundWithoutPage(t *testing.T) {
	g := newRig(t, fstest.MapFS{"index.html": {Data: []byte("x")}, ".gitkeep": {}})
	if w := g.do(http.MethodGet, "/missing", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status %d", w.Code)
	}
	if w := g.do(http.MethodGet, "/.gitkeep", ""); w.Code != http.StatusNotFound {
		t.Fatalf(".gitkeep status %d", w.Code)
	}
}

func TestStaticNotBuilt(t *testing.T) {
	for name, web := range map[string]fstest.MapFS{
		"only placeholder": {".gitkeep": {}},
		"empty":            {},
	} {
		t.Run(name, func(t *testing.T) {
			g := newRig(t, web)
			for _, p := range []string{"/", "/anything", "/auth/launch/"} {
				w := g.do(http.MethodGet, p, "")
				if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "scripts/start.sh") {
					t.Errorf("%s: %d %q", p, w.Code, w.Body)
				}
				if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
					t.Errorf("%s: content type %q", p, w.Header().Get("Content-Type"))
				}
			}
			if w := g.do(http.MethodHead, "/", ""); w.Body.Len() != 0 {
				t.Error("HEAD returned a body")
			}
		})
	}
}

func TestStaticNilFS(t *testing.T) {
	g := newRig(t, nil)
	if w := g.do(http.MethodGet, "/", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}
