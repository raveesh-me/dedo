package restdoc

import (
	"fmt"
	"sort"

	"github.com/raveesh-me/dedo/docnotes"
)

// ownExtensions are the specification extensions the generator writes on
// an operation itself, which a doc note may not set.
var ownExtensions = []string{"x-sunset", "x-successor", "x-destructive", "x-rpc-procedure", "x-error-reasons", "x-emits"}

// readNotes reads the doc notes files and refuses a note that would set one
// of the generator's own extensions.
func readNotes(paths []string) (docnotes.Notes, error) {
	notes, err := docnotes.Read(paths)
	if err != nil {
		return nil, err
	}
	methods := make([]string, 0, len(notes))
	for m := range notes {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	for _, m := range methods {
		for _, own := range ownExtensions {
			if _, ok := notes[m].Extensions[own]; ok {
				return nil, fmt.Errorf("doc notes: %s: %s is the generator's own extension", m, own)
			}
		}
	}
	return notes, nil
}
