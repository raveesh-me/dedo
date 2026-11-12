package main

import (
	"os"
	"strings"
	"testing"
)

// telemetryFuncs set up the OpenTelemetry SDK in a service's own main.go,
// and shut the server down so that the SDK flushes.
var telemetryFuncs = []string{"serve", "setupTelemetry", "exporterOn", "propagatorFromEnv"}

// funcSource returns a top-level function of a Go file, or Go template,
// with its doc comment: from the comment to the closing brace at the start
// of a line.
func funcSource(src, name string) string {
	i := strings.Index(src, "\nfunc "+name+"(")
	if i < 0 {
		return ""
	}
	end := strings.Index(src[i:], "\n}\n")
	if end < 0 {
		return ""
	}
	start := i
	for {
		prev := strings.LastIndex(src[:start], "\n")
		if prev < 0 || !strings.HasPrefix(src[prev+1:start], "//") {
			break
		}
		start = prev
	}
	return src[start : i+end+3]
}

// The OpenTelemetry setup dedo init writes is the same in the service's
// and the BFF's main.go, and in the examples, which test it.
func TestTelemetrySetupInSync(t *testing.T) {
	sources := map[string][]byte{
		"templates/main.go.tmpl":     asset("templates/main.go.tmpl"),
		"templates/bff/main.go.tmpl": asset("templates/bff/main.go.tmpl"),
	}
	for _, ex := range []string{"todo", "contacts"} {
		path := "../../examples/" + ex + "/main.go"
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources[path] = b
	}
	todo := string(sources["../../examples/todo/main.go"])
	for name, src := range sources {
		for _, fn := range telemetryFuncs {
			want := funcSource(todo, fn)
			if want == "" {
				t.Fatalf("examples/todo/main.go has no %s", fn)
			}
			if got := funcSource(string(src), fn); got != want {
				t.Errorf("%s: %s differs from examples/todo/main.go's:\n%s", name, fn, got)
			}
		}
	}
}
