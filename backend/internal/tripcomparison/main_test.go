package tripcomparison

import (
	"os"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/offlinetest"
)

// Comparisons take rates and advice sources, so a test could wire in one
// that fetches; offlinetest keeps it from reaching the ECB or GOV.UK.
func TestMain(m *testing.M) {
	os.Exit(offlinetest.Main(m))
}
