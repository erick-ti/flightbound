package traveladvice

import (
	"net/http"
	"os"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/offlinetest"
)

func TestMain(m *testing.M) {
	os.Exit(offlinetest.Main(m))
}

// offlinetest.Main points the proxy variables at its guard, so a request to
// GOV.UK through NewClient goes to the guard instead of out. Comparing with
// the guard's own address keeps this check meaningful where a proxy is
// already set.
func TestNewClientUsesTheEnvironmentProxy(t *testing.T) {
	guard := offlinetest.ProxyURL()
	if guard == "" {
		t.Fatal("tests are not running under offlinetest.Main")
	}
	req, err := http.NewRequest(http.MethodGet, "https://www.gov.uk/api/content/foreign-travel-advice/portugal", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewClient().Transport.(*http.Transport).Proxy(req)
	if err != nil || proxy == nil || proxy.String() != guard {
		t.Errorf("proxy for GOV.UK = %v, %v; want the test guard %s", proxy, err, guard)
	}
}
