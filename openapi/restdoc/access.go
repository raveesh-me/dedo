package restdoc

import (
	"fmt"
	"sort"

	"github.com/raveesh-me/dedo/accessdoc"
)

// ownExtensions are the specification extensions the generator writes on
// an operation itself, which an access file may not set.
var ownExtensions = []string{"x-sunset", "x-successor", "x-destructive", "x-rpc-procedure", "x-error-reasons", "x-emits"}

// readAccess reads the access files and refuses an entry that would set
// one of the generator's own extensions.
func readAccess(paths []string) (accessdoc.Requirements, error) {
	reqs, err := accessdoc.Read(paths)
	if err != nil {
		return nil, err
	}
	scopes := make([]string, 0, len(reqs))
	for s := range reqs {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)
	for _, s := range scopes {
		for _, own := range ownExtensions {
			if _, ok := reqs[s].Extensions[own]; ok {
				return nil, fmt.Errorf("access: scope %s: %s is the generator's own extension", s, own)
			}
		}
	}
	return reqs, nil
}
