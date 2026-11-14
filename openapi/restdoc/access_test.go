package restdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
)

const (
	libraryAccess = "testdata/access/library.access.json"
	vaultProto    = "acme/vault/v1/vault.proto"
)

// operations returns the document's operations by operationId.
func operations(t *testing.T, doc string) map[string]map[string]json.RawMessage {
	t.Helper()
	var d struct {
		Paths map[string]map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]json.RawMessage{}
	for _, item := range d.Paths {
		for _, op := range item {
			var id string
			if err := json.Unmarshal(op["operationId"], &id); err != nil {
				t.Fatal(err)
			}
			out[id] = op
		}
	}
	return out
}

func description(t *testing.T, op map[string]json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(op["description"], &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func writeAccess(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAccess: every operation declaring a scope gets that scope's
// paragraph after the scopes and before the events, and its extensions;
// MCP meta stays out of the document; an operation whose scopes no file
// names is unchanged; a scope no RPC declares is ignored.
func TestAccess(t *testing.T) {
	without := operations(t, generate(t, ""))
	doc := generate(t, "access="+libraryAccess)
	with := operations(t, doc)

	want := "Removes a book from its shelf for good.\n\n" +
		"Requires one of the token scopes: `library.book.write`.\n\n" +
		"Only for: Libraries on the Archive plan (`archive`). Other callers get 403 `PERMISSION_DENIED` with reason `CONDITION_NOT_MET`.\n\n" +
		"May publish the events: `raveesh_me.library.book.deleted`."
	if got := description(t, with["LibraryService_DeleteBook"]); !strings.HasPrefix(got, want) {
		t.Errorf("description = %q\nwant it to start %q", got, want)
	}
	// RemoveBook, the other RPC with the scope, has no REST binding.
	for _, id := range []string{"LibraryService_DeleteBook"} {
		var conditions []struct{ ID, Label string }
		if err := json.Unmarshal(with[id]["x-commerce-conditions"], &conditions); err != nil || len(conditions) != 1 || conditions[0].ID != "archive" {
			t.Errorf("%s x-commerce-conditions = %s (%v)", id, with[id]["x-commerce-conditions"], err)
		}
		if with[id]["x-commerce-permissions"] == nil || !strings.Contains(description(t, with[id]), "`CONDITION_NOT_MET`") {
			t.Errorf("%s lacks the scope's requirements", id)
		}
	}
	if got, want := string(mustJSON(t, with["LibraryService_GetBook"])), string(mustJSON(t, without["LibraryService_GetBook"])); got != want {
		t.Error("GetBook, whose scope no file names, changed")
	}
	for _, unwanted := range []string{"Ignored", `"conditions"`, `"meta"`, "_meta"} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("the document carries %s", unwanted)
		}
	}
	d, err := libopenapi.NewDocument([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	model, err := d.BuildV3Model()
	if err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	item, _ := model.Model.Paths.PathItems.Get("/v1/shelves/{shelf}/books/{book}")
	if _, ok := item.Delete.Extensions.Get("x-commerce-conditions"); !ok {
		t.Error("the DELETE operation has no x-commerce-conditions extension")
	}
}

// TestAccessOrder: an RPC with several scopes gets their paragraphs in
// scope order; one scope in several files gets the files' paragraphs in
// parameter order; extensions follow the operation's own fields, sorted,
// and the same files always give the same bytes.
func TestAccessOrder(t *testing.T) {
	first := writeAccess(t, "first.json", `{"scopes": {
		"vault.admin": {"description": "Admins only.", "extensions": {"x-zeta": 1}},
		"vault.open": {"description": "Open vaults only.", "extensions": {"x-alpha": {"b": 2, "a": 1}}}
	}}`)
	second := writeAccess(t, "second.json", `{"scopes": {"vault.open": {"description": "Audited."}}}`)
	params := "access=" + first + ",access=" + second
	doc := generate(t, params, vaultProto)
	op := operations(t, doc)["VaultService_OpenVault"]
	want := "OpenVault needs either scope.\n\n" +
		"Requires one of the token scopes: `vault.open`, `vault.admin`.\n\n" +
		"Open vaults only.\n\nAudited.\n\nAdmins only."
	if got := description(t, op); got != want {
		t.Errorf("description = %q\nwant %q", got, want)
	}
	text := doc[strings.Index(doc, `"operationId": "VaultService_OpenVault"`):]
	procedure, alpha, zeta := strings.Index(text, `"x-rpc-procedure"`), strings.Index(text, `"x-alpha"`), strings.Index(text, `"x-zeta"`)
	if procedure < 0 || alpha < procedure || zeta < alpha {
		t.Errorf("want x-rpc-procedure, x-alpha, x-zeta in that order:\n%s", text)
	}
	if again := generate(t, params, vaultProto); again != doc {
		t.Error("the same access files gave a different document")
	}
}

// TestAccessRejects: one extension twice for an RPC, dedo's own
// extensions, an extension not named x-, and a file that cannot be read
// fail generation.
func TestAccessRejects(t *testing.T) {
	for _, key := range []string{"x-rpc-procedure", "x-error-reasons", "x-emits", "x-sunset", "x-successor", "x-destructive"} {
		t.Run(key, func(t *testing.T) {
			path := writeAccess(t, "own.json", `{"scopes": {"library.book.read": {"extensions": {"`+key+`": 1}}}}`)
			if _, err := generatePlugin(t, "access="+path); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("err = %v, want one naming %s", err, key)
			}
		})
	}
	twice := writeAccess(t, "twice.json", `{"scopes": {"vault.open": {"extensions": {"x-plans": 1}}, "vault.admin": {"extensions": {"x-plans": 2}}}}`)
	if _, err := generatePlugin(t, "access="+twice, vaultProto); err == nil || !strings.Contains(err.Error(), "OpenVault") || !strings.Contains(err.Error(), "x-plans") {
		t.Errorf("err = %v, want OpenVault's x-plans twice", err)
	}
	nox := writeAccess(t, "nox.json", `{"scopes": {"library.book.read": {"extensions": {"plans": 1}}}}`)
	if _, err := generatePlugin(t, "access="+nox); err == nil || !strings.Contains(err.Error(), "x-") {
		t.Errorf("err = %v, want the x- prefix required", err)
	}
	if _, err := generatePlugin(t, "access=testdata/access/missing.json"); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Errorf("err = %v, want the missing file named", err)
	}
}

func TestConfigSetsAccess(t *testing.T) {
	var cfg Config
	for _, p := range []string{"a.json", "b.json"} {
		if err := cfg.Set("access", p); err != nil {
			t.Fatal(err)
		}
	}
	if len(cfg.Access) != 2 || cfg.Access[0] != "a.json" || cfg.Access[1] != "b.json" {
		t.Errorf("Access = %v", cfg.Access)
	}
	if err := cfg.Set("access", " "); err == nil {
		t.Error("want an error for an empty path")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
