package mcpserver

import "testing"

// A doc note is its own paragraph after the tool description and before the
// declared errors; without one the description is Describe's.
func TestDescribeWithNote(t *testing.T) {
	m := service(t).Methods().ByName("GetThing")
	tool, err := DescribeWithNote(m, "Only for accounts on the pro plan.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Returns a thing.\n\nOnly for accounts on the pro plan.\n\nErrors: THING_NOT_FOUND (NOT_FOUND)."; tool.Description != want {
		t.Errorf("description = %q, want %q", tool.Description, want)
	}
	plain, err := Describe(m)
	if err != nil {
		t.Fatal(err)
	}
	none, err := DescribeWithNote(m, "")
	if err != nil {
		t.Fatal(err)
	}
	if none.Description != plain.Description || plain.Description != "Returns a thing.\n\nErrors: THING_NOT_FOUND (NOT_FOUND)." {
		t.Errorf("without a note: %q, Describe: %q", none.Description, plain.Description)
	}
}
