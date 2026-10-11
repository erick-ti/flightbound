// Package offlinetest keeps a package's tests from reaching hosts outside
// the machine. Import it only from tests.
package offlinetest

import (
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
)

// Guard is a local listener that counts and drops every connection.
type Guard struct {
	ln          net.Listener
	connections atomic.Int64
}

// Start starts a guard on a loopback port.
func Start() (*Guard, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	g := &Guard{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			g.connections.Add(1)
			conn.Close()
		}
	}()
	return g, nil
}

// URL returns the guard's address as a proxy URL.
func (g *Guard) URL() string { return "http://" + g.ln.Addr().String() }

// Connections returns how many connections the guard has dropped.
func (g *Guard) Connections() int64 { return g.connections.Load() }

// Close stops the guard.
func (g *Guard) Close() error { return g.ln.Close() }

// proxyURL is the address of the guard Main started, or empty.
var proxyURL string

// ProxyURL returns the address of the guard that Main points the proxy
// variables at, or "" when the tests are not running under Main. Checks
// that a client sends outside requests to the guard compare with this
// rather than with the environment, which may already name another proxy.
func ProxyURL() string { return proxyURL }

// Main runs a package's tests with the HTTP proxy variables pointing at a
// guard, so that a client taking its proxy from the environment, as Go's
// default transport does, cannot reach an outside host. Go never sends a
// request for a loopback address through a proxy, so tests against local
// httptest servers still work. Main fails the run if anything connected to
// the guard. Call it from TestMain as os.Exit(offlinetest.Main(m)).
func Main(m *testing.M) int {
	g, err := Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "offlinetest:", err)
		return 1
	}
	defer g.Close()
	proxyURL = g.URL()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		os.Setenv(name, g.URL())
	}
	// NO_PROXY could exempt outside hosts, and REQUEST_METHOD makes Go ignore
	// HTTP_PROXY.
	for _, name := range []string{"NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		os.Unsetenv(name)
	}
	code := m.Run()
	if n := g.Connections(); n > 0 {
		fmt.Fprintf(os.Stderr, "offlinetest: tests tried to reach an outside host %d times\n", n)
		if code == 0 {
			code = 1
		}
	}
	return code
}
