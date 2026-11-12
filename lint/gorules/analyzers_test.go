package gorules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// Each package under testdata/src/example.com/svc marks the findings an
// analyzer must report with // want; every other line, suppressed ones
// included, must report nothing.

func TestServerInterceptors(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ServerInterceptors, "example.com/svc/app", "example.com/svc/app/apptest")
}

func TestServerInterceptorsBFF(t *testing.T) {
	setFlag(t, ServerInterceptors, "bff", "true")
	analysistest.Run(t, analysistest.TestData(), ServerInterceptors, "example.com/svc/bff")
}

func TestErrorReasonDeclared(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ErrorReasonDeclared,
		"example.com/svc/internal/orders", "example.com/svc/internal/ping", "example.com/svc/internal/orders/orderstest")
}

func TestEventDeclared(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), EventDeclared, "example.com/svc/internal/shipping")
}

func TestIgnoreReason(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), IgnoreReason, "example.com/svc/ignore")
}

func TestHouse(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "src", "example.com", "svc")
	flag, err := HouseConfig{Root: root, Rules: []config.GoRule{{
		ID:           "HOUSE_GO_NO_PLATFORM_SDK",
		ForbidImport: "example.com/platform-sdk/...",
		AllowIn:      []string{"internal/legacy/**"},
		Message:      "Call the platform services directly.",
		Doc:          "https://example.com/rules#sdk",
	}}}.Flag()
	if err != nil {
		t.Fatal(err)
	}
	setFlag(t, House, "config", flag)
	analysistest.Run(t, analysistest.TestData(), House,
		"example.com/svc/internal/billing", "example.com/svc/internal/legacy")
}

// Without rules the house analyzer reports nothing.
func TestHouseWithoutRules(t *testing.T) {
	setFlag(t, House, "config", "")
	analysistest.Run(t, analysistest.TestData(), House, "example.com/svc/internal/legacy")
}

func TestHouseBadFlag(t *testing.T) {
	if _, err := parseHouseFlag("{"); err == nil {
		t.Error("a malformed -config is accepted")
	}
}

func setFlag(t *testing.T, a *analysis.Analyzer, name, value string) {
	t.Helper()
	prev := a.Flags.Lookup(name).Value.String()
	if err := a.Flags.Set(name, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Flags.Set(name, prev) })
}
