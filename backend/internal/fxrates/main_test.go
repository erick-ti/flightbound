package fxrates

import (
	"os"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/offlinetest"
)

// offlinetest keeps any fetcher built in these tests from reaching the ECB.
func TestMain(m *testing.M) {
	os.Exit(offlinetest.Main(m))
}
