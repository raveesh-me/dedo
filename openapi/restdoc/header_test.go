package restdoc

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
)

const versionHeaders = "testdata/headers/version.headers.json"

// v4Version is the Version header every operation of HighLevel's v4
// Marketplace specs declares, inline, as GoHighLevel/api-documentation
// writes it.
const v4Version = `{"name":"Version","in":"header","description":"API Version. Send ` + "`v4`" + `.","required":true,"schema":{"type":"string","enum":["v4"],"example":"v4"},"example":"v4"}`

// TestHeadersGolden: without headers= the document has no header
// parameter; with it, every operation declares the file's headers inline,
// after its path and query parameters.
func TestHeadersGolden(t *testing.T) {
	for _, c := range []struct{ name, params, golden string }{
		{"none", "", "vault.openapi.json"},
		{"version", "headers=" + versionHeaders, "vault.headers.openapi.json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := generate(t, c.params, vaultProto)
			checkGolden(t, got, filepath.Join("testdata", c.golden))
			if c.params == "" && strings.Contains(got, `"in": "header"`) {
				t.Error("a document generated without headers= declares a header parameter")
			}
		})
	}
}

// TestHeadersEveryOperation: each operation's header parameter is exactly
// the v4 Version object, on every binding, and the document is valid.
func TestHeadersEveryOperation(t *testing.T) {
	doc := generate(t, "headers="+versionHeaders, examplesProto, vaultProto)
	ops := operations(t, doc)
	if len(ops) < 3 {
		t.Fatalf("%d operations, want the examples' and the vault's", len(ops))
	}
	for id, op := range ops {
		headers := headerParams(t, op)
		if len(headers) != 1 || headers[0] != v4Version {
			t.Errorf("%s header parameters = %s\nwant [%s]", id, headers, v4Version)
		}
	}
	d, err := libopenapi.NewDocument([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
}

// TestHeadersOrder: several files give their headers in parameter order,
// then file order; a header without example or required is written
// without them.
func TestHeadersOrder(t *testing.T) {
	extra := writeAccess(t, "extra.json", `{"headers": [
		{"name": "X-Request-Id", "description": "Identifies the request.", "schema": {"type": "string"}},
		{"name": "x-retry-count", "description": "Retries so far.", "schema": {"type": "integer", "enum": [0, 1, 2]}, "example": 1}
	]}`)
	op := operations(t, generate(t, "headers="+versionHeaders+",headers="+extra, vaultProto))["VaultService_OpenVault"]
	want := []string{
		v4Version,
		`{"name":"X-Request-Id","in":"header","description":"Identifies the request.","schema":{"type":"string"}}`,
		`{"name":"x-retry-count","in":"header","description":"Retries so far.","schema":{"type":"integer","enum":[0,1,2]},"example":1}`,
	}
	if got := headerParams(t, op); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("header parameters =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestHeadersRejects: a file that cannot be read or is not the format, and
// a header that is not one or that the document cannot truthfully declare,
// fail generation with an error naming the file and the problem.
func TestHeadersRejects(t *testing.T) {
	header := func(fields string) string { return `{"headers": [{` + fields + `}]}` }
	valid := `"name": "Version", "description": "API Version.", "schema": {"type": "string"}`
	tests := []struct {
		name, content, wantErr string
	}{
		{"not JSON", `{"headers":`, "unexpected EOF"},
		{"an unknown field", header(valid + `, "in": "header"`), `unknown field "in"`},
		{"trailing data", `{"headers": []} {}`, "after the headers"},
		{"no header", `{"headers": []}`, "lists no header"},
		{"no name", header(`"description": "x", "schema": {"type": "string"}`), "has no name"},
		{"a name that is not a token", header(`"name": "API Version", "description": "x", "schema": {"type": "string"}`), `"API Version" is not a header name`},
		{"Authorization", header(`"name": "authorization", "description": "x", "schema": {"type": "string"}`), "OpenAPI ignores"},
		{"Content-Type", header(`"name": "Content-Type", "description": "x", "schema": {"type": "string"}`), "OpenAPI ignores"},
		{"no description", header(`"name": "Version", "schema": {"type": "string"}`), "Version has no description"},
		{"no schema", header(`"name": "Version", "description": "x"`), "Version has no schema type"},
		{"an object type", header(`"name": "Version", "description": "x", "schema": {"type": "object"}`), `type "object"`},
		{"an empty enum", header(valid[:len(valid)-1] + `, "enum": []}`), "empty enum"},
		{"an enum value of another type", header(`"name": "Version", "description": "x", "schema": {"type": "string", "enum": ["v4", 4]}`), "enum value 4 is not of type string"},
		{"an example of another type", header(valid + `, "example": 4`), "example 4 is not of type string"},
		{"a schema example of another type", header(`"name": "Version", "description": "x", "schema": {"type": "integer", "example": 1.5}`), "example 1.5 is not of type integer"},
		{"an example not in the enum", header(`"name": "Version", "description": "x", "schema": {"type": "string", "enum": ["v4"]}, "example": "v3"`), `example "v3" is not one of the enum`},
		{"one header twice", `{"headers": [{` + valid + `}, {"name": "version", "description": "x", "schema": {"type": "string"}}]}`, "version twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeAccess(t, "bad.headers.json", tt.content)
			_, err := generatePlugin(t, "headers="+path, vaultProto)
			if err == nil || !strings.Contains(err.Error(), "bad.headers.json") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want one naming bad.headers.json and containing %q", err, tt.wantErr)
			}
		})
	}
	if _, err := generatePlugin(t, "headers=testdata/headers/missing.json", vaultProto); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Errorf("err = %v, want the missing file named", err)
	}
	other := writeAccess(t, "other.json", `{"headers": [{"name": "VERSION", "description": "x", "schema": {"type": "string"}}]}`)
	if _, err := generatePlugin(t, "headers="+versionHeaders+",headers="+other, vaultProto); err == nil || !strings.Contains(err.Error(), "VERSION twice") {
		t.Errorf("err = %v, want one header in two files refused", err)
	}
}

// TestHeadersCollide: a header named like one of an operation's path or
// query parameters, ignoring case, fails generation, naming the RPC.
func TestHeadersCollide(t *testing.T) {
	for _, c := range []struct{ name, proto, rpc, kind string }{
		{"VaultsId", vaultProto, "OpenVault", "path"},
		{"UpdateMask", examplesProto, "UpdateWidget", "query"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeAccess(t, "collide.json", `{"headers": [{"name": "`+c.name+`", "description": "x", "schema": {"type": "string"}}]}`)
			_, err := generatePlugin(t, "headers="+path, c.proto)
			if err == nil || !strings.Contains(err.Error(), c.rpc) || !strings.Contains(err.Error(), c.kind+" parameter") {
				t.Errorf("err = %v, want %s's %s parameter named", err, c.rpc, c.kind)
			}
		})
	}
}

func TestConfigSetsHeaders(t *testing.T) {
	var cfg Config
	for _, p := range []string{"a.json", "b.json"} {
		if err := cfg.Set("headers", p); err != nil {
			t.Fatal(err)
		}
	}
	if len(cfg.Headers) != 2 || cfg.Headers[0] != "a.json" || cfg.Headers[1] != "b.json" {
		t.Errorf("Headers = %v", cfg.Headers)
	}
	if err := cfg.Set("headers", " "); err == nil {
		t.Error("want an error for an empty path")
	}
}

// headerParams returns an operation's header parameters, each compacted.
func headerParams(t *testing.T, op map[string]json.RawMessage) []string {
	t.Helper()
	var params []json.RawMessage
	if err := json.Unmarshal(op["parameters"], &params); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range params {
		var in struct{ In string }
		if err := json.Unmarshal(p, &in); err != nil {
			t.Fatal(err)
		}
		if in.In != "header" {
			continue
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, p); err != nil {
			t.Fatal(err)
		}
		out = append(out, buf.String())
	}
	return out
}

// checkGolden compares got with the golden file, which -update rewrites.
func checkGolden(t *testing.T, got, golden string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s; run go test ./restdoc -update and review the diff", golden)
	}
}

// TestHeaderValueTypes: a value decoded from a header file is of a type
// when JSON Schema says it is; an integer may be written 2.0.
func TestHeaderValueTypes(t *testing.T) {
	for _, c := range []struct {
		value any
		typ   string
		want  bool
	}{
		{"v4", "string", true},
		{json.Number("4"), "string", false},
		{true, "boolean", true},
		{"true", "boolean", false},
		{json.Number("1.5"), "number", true},
		{"1.5", "number", false},
		{json.Number("2.0"), "integer", true},
		{json.Number("2.5"), "integer", false},
		{"2", "integer", false},
		{"v4", "array", false},
	} {
		if got := ofType(c.value, c.typ); got != c.want {
			t.Errorf("ofType(%#v, %s) = %v, want %v", c.value, c.typ, got, c.want)
		}
	}
	if !sameValue(json.Number("1"), json.Number("1.0")) || sameValue(json.Number("1"), "1") {
		t.Error("sameValue compares numbers by value and nothing else as a number")
	}
}
