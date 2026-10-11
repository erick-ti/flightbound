package offlinetest

import (
	"net/http"
	"net/url"
	"testing"
)

func TestGuardDropsAndCountsConnections(t *testing.T) {
	g, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	proxy, err := url.Parse(g.URL())
	if err != nil {
		t.Fatal(err)
	}
	// Through a proxy the client connects only to the proxy, so this never
	// leaves the machine.
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	if resp, err := client.Get("https://www.gov.uk/"); err == nil {
		resp.Body.Close()
		t.Fatal("a request through the guard got a response")
	}
	if got := g.Connections(); got < 1 {
		t.Errorf("guard saw %d connections, want at least 1", got)
	}
}
