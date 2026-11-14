package restdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
)

const libraryNotes = "testdata/notes/library.notes.json"

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

// TestNotes: a note's paragraph follows the scopes and precedes the events,
// its extensions are the operation's, a note for an RPC the document does
// not have is ignored, and an RPC without a note is unchanged.
func TestNotes(t *testing.T) {
	without := operations(t, generate(t, ""))
	doc := generate(t, "notes="+libraryNotes)
	with := operations(t, doc)

	del := with["LibraryService_DeleteBook"]
	want := "Removes a book from its shelf for good.\n\n" +
		"Requires one of the token scopes: `library.book.write`.\n\n" +
		"Only for libraries on the `archive` plan. Any other library is refused with `PERMISSION_DENIED` and the reason `PLAN_NOT_ENTITLED`.\n\n" +
		"May publish the events: `raveesh_me.library.book.deleted`."
	if got := description(t, del); !strings.HasPrefix(got, want) {
		t.Errorf("description = %q\nwant it to start %q", got, want)
	}
	var plans struct {
		Plans  []string `json:"plans"`
		Reason string   `json:"reason"`
	}
	if err := json.Unmarshal(del["x-plans"], &plans); err != nil || len(plans.Plans) != 1 || plans.Plans[0] != "archive" || plans.Reason != "PLAN_NOT_ENTITLED" {
		t.Errorf("x-plans = %s (%v)", del["x-plans"], err)
	}
	for id, op := range with {
		if id == "LibraryService_DeleteBook" {
			continue
		}
		if string(mustJSON(t, op)) != string(mustJSON(t, without[id])) {
			t.Errorf("%s changed without a note", id)
		}
	}
	if strings.Contains(doc, "Ignored") {
		t.Error("a note for an RPC the document does not have was used")
	}
	// The document stays valid OpenAPI, and the extension is the
	// operation's own.
	d, err := libopenapi.NewDocument([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	model, err := d.BuildV3Model()
	if err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	item, _ := model.Model.Paths.PathItems.Get("/v1/shelves/{shelf}/books/{book}")
	if _, ok := item.Delete.Extensions.Get("x-plans"); !ok {
		t.Error("the DELETE operation has no x-plans extension")
	}
}

// TestNotesKeepKeyOrder: the extensions follow the operation's own fields,
// sorted, so the document is the same bytes for the same notes.
func TestNotesKeepKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.json")
	notes := `{"methods": {"raveesh_me.library.v1.LibraryService.GetBook": {"extensions": {"x-zeta": 1, "x-alpha": {"b": 2, "a": 1}}}}}`
	if err := os.WriteFile(path, []byte(notes), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := generate(t, "notes="+path)
	start := strings.Index(doc, `"operationId": "LibraryService_GetBook"`)
	op := doc[start:]
	op = op[:strings.Index(op, "\n    }")]
	procedure, alpha, zeta := strings.Index(op, `"x-rpc-procedure"`), strings.Index(op, `"x-alpha"`), strings.Index(op, `"x-zeta"`)
	if procedure < 0 || alpha < procedure || zeta < alpha {
		t.Errorf("want x-rpc-procedure, x-alpha, x-zeta in that order:\n%s", op)
	}
	if again := generate(t, "notes="+path); again != doc {
		t.Error("the same notes gave a different document")
	}
}

// TestNotesRejects: dedo's own extensions cannot be overwritten, and a
// notes file that cannot be read fails generation.
func TestNotesRejects(t *testing.T) {
	for _, key := range []string{"x-rpc-procedure", "x-error-reasons", "x-emits", "x-sunset", "x-successor", "x-destructive"} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "n.json")
			notes := `{"methods": {"raveesh_me.library.v1.LibraryService.GetBook": {"extensions": {"` + key + `": 1}}}}`
			if err := os.WriteFile(path, []byte(notes), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := generatePlugin(t, "notes="+path); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("err = %v, want one naming %s", err, key)
			}
		})
	}
	if _, err := generatePlugin(t, "notes=testdata/notes/missing.json"); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Errorf("err = %v, want the missing file named", err)
	}
	path := filepath.Join(t.TempDir(), "nox.json")
	if err := os.WriteFile(path, []byte(`{"methods": {"raveesh_me.library.v1.LibraryService.GetBook": {"extensions": {"plans": 1}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := generatePlugin(t, "notes="+path); err == nil || !strings.Contains(err.Error(), "x-") {
		t.Errorf("err = %v, want the x- prefix required", err)
	}
}

func TestConfigSetsNotes(t *testing.T) {
	var cfg Config
	for _, p := range []string{"a.json", "b.json"} {
		if err := cfg.Set("notes", p); err != nil {
			t.Fatal(err)
		}
	}
	if len(cfg.Notes) != 2 || cfg.Notes[0] != "a.json" || cfg.Notes[1] != "b.json" {
		t.Errorf("Notes = %v", cfg.Notes)
	}
	if err := cfg.Set("notes", " "); err == nil {
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
