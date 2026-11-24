//go:build e2e

package netguard

import (
	"errors"
	"net/url"
	"testing"
)

func TestAllowLoopbackE2E(t *testing.T) {
	g := newGuard(&fakeTransport{})
	g.AllowLoopback("127.0.0.1:4411", GitHub)
	ok, _ := url.Parse("http://127.0.0.1:4411/graphql")
	if p, err := g.check(ok); err != nil || p != GitHub {
		t.Fatalf("%v %v", p, err)
	}
	for _, raw := range []string{"http://127.0.0.1:4412/x", "https://127.0.0.1:4411/x", "http://localhost:4411/x"} {
		u, _ := url.Parse(raw)
		if _, err := g.check(u); !errors.Is(err, ErrHostNotAllowed) {
			t.Errorf("%s: %v", raw, err)
		}
	}
}
