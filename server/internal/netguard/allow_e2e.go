//go:build e2e

package netguard

// AllowLoopback lets the guard reach hostPort (for example "127.0.0.1:4411") over plain http as
// part of provider. It exists only in the e2e build, for the in-process fake servers.
func (g *Guard) AllowLoopback(hostPort string, p Provider) {
	g.mu.Lock()
	g.extra[hostPort] = p
	g.mu.Unlock()
}
