package restdoc

import (
	"reflect"
	"testing"
)

func TestOpenAPIPath(t *testing.T) {
	resourceNames := func(seg segment) []string {
		if seg.field == "name" || seg.field == "todo.name" {
			return []string{"todo"}
		}
		return nil
	}
	tests := []struct {
		template   string
		wantPath   string
		wantParams []pathParam
	}{
		{
			template:   "/commerce/todo/v1/{name=todos/*}",
			wantPath:   "/commerce/todo/v1/todos/{todo}",
			wantParams: []pathParam{{name: "todo", field: "name", full: "todos/{todo}"}},
		},
		{
			template:   "/v1/{todo.name=todos/*}",
			wantPath:   "/v1/todos/{todo}",
			wantParams: []pathParam{{name: "todo", field: "todo.name", full: "todos/{todo}"}},
		},
		{
			template:   "/v1/{name=todos/*}:remove",
			wantPath:   "/v1/todos/{todo}:remove",
			wantParams: []pathParam{{name: "todo", field: "name", full: "todos/{todo}"}},
		},
		{
			template:   "/v1/{parent=shelves/*}/books",
			wantPath:   "/v1/shelves/{shelvesId}/books",
			wantParams: []pathParam{{name: "shelvesId", field: "parent", full: "shelves/{shelvesId}"}},
		},
		{
			template:   "/v1/books/{book_id}",
			wantPath:   "/v1/books/{book_id}",
			wantParams: []pathParam{{name: "book_id", field: "book_id", full: "{book_id}"}},
		},
		{template: "/v1/todos", wantPath: "/v1/todos"},
		{template: "/v1/todos:batchGet", wantPath: "/v1/todos:batchGet"},
	}
	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			tmpl, err := parseTemplate(tt.template)
			if err != nil {
				t.Fatal(err)
			}
			path, params := tmpl.openAPIPath(resourceNames)
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
			if !reflect.DeepEqual(params, tt.wantParams) {
				t.Errorf("params = %+v, want %+v", params, tt.wantParams)
			}
		})
	}
}

func TestParseTemplateErrors(t *testing.T) {
	for _, bad := range []string{"v1/todos", "/v1/{name=todos/*"} {
		if _, err := parseTemplate(bad); err == nil {
			t.Errorf("parseTemplate(%q): want error", bad)
		}
	}
}
