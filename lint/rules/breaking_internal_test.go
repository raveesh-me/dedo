package rules

import (
	"slices"
	"testing"
)

// Two resource name patterns are the same when they differ only in the
// names of their variables: a name resolves the same under both.
func TestSamePattern(t *testing.T) {
	for _, tt := range []struct {
		name, before, now string
		same              bool
	}{
		{"identical", "tax-rates/{tax_rate}", "tax-rates/{tax_rate}", true},
		{"variable renamed", "tax-rates/{tax_rate}", "tax-rates/{taxRate}", true},
		{"nested variables renamed", "parents/{parent}/children/{child}", "parents/{parentId}/children/{childId}", true},
		{"nested, one variable renamed", "locations/{location}/tax-rates/{tax_rate}", "locations/{location}/tax-rates/{taxRate}", true},
		{"variables swapped", "shelves/{a}/books/{b}", "shelves/{b}/books/{a}", true},
		{"complex segment renamed", "projects/{project}/locations/{a}~{b}", "projects/{project}/locations/{x}~{y}", true},
		{"no variables", "config", "config", true},

		{"literal changed", "tax-rates/{tax_rate}", "taxRates/{taxRate}", false},
		{"nested literal changed", "parents/{parent}/children/{child}", "parents/{parent}/kids/{child}", false},
		{"literal added", "books/{book}", "shelves/{shelf}/books/{book}", false},
		{"literal removed", "shelves/{shelf}/books/{book}", "books/{book}", false},
		{"trailing literal added", "users/{user}", "users/{user}/settings", false},
		{"variable added", "books/{book}", "books/{book}/{edition}", false},
		{"variable removed", "members/{member}/loans/{loan}", "members/{member}/loans", false},
		{"variable added to a segment", "locations/{location}", "locations/{project}~{location}", false},
		{"complex segment separator changed", "locations/{a}~{b}", "locations/{a}.{b}", false},
		{"segments reordered", "libraries/{library}/copies/{copy}", "copies/{copy}/libraries/{library}", false},
		{"variable became literal", "books/{book}", "books/default", false},
		{"literal became variable", "books/default", "books/{book}", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := samePattern(tt.before, tt.now); got != tt.same {
				t.Errorf("samePattern(%q, %q) = %v, want %v", tt.before, tt.now, got, tt.same)
			}
		})
	}
}

// A resource loses a pattern only when no pattern it has now is the same
// as it, whatever its position or the number of patterns.
func TestMissingPatterns(t *testing.T) {
	for _, tt := range []struct {
		name        string
		before, now []string
		missing     []string
	}{
		{"rename only", []string{"tax-rates/{tax_rate}"}, []string{"tax-rates/{taxRate}"}, nil},
		{"nested rename", []string{"parents/{parent}/children/{child}"}, []string{"parents/{parentId}/children/{childId}"}, nil},
		{"pattern added", []string{"books/{book}"}, []string{"books/{book}", "shelves/{shelf}/books/{book}"}, nil},
		{
			"multi-pattern, every variable renamed",
			[]string{"authors/{author_id}", "publishers/{publisher_id}/authors/{author_id}"},
			[]string{"authors/{authorId}", "publishers/{publisherId}/authors/{authorId}"},
			nil,
		},
		{
			"multi-pattern, reordered and renamed",
			[]string{"authors/{author_id}", "publishers/{publisher_id}/authors/{author_id}"},
			[]string{"publishers/{publisherId}/authors/{authorId}", "authors/{authorId}"},
			nil,
		},
		{"literal changed", []string{"tax-rates/{tax_rate}"}, []string{"taxRates/{taxRate}"}, []string{"tax-rates/{tax_rate}"}},
		{"variable added", []string{"books/{book}"}, []string{"books/{book}/{edition}"}, []string{"books/{book}"}},
		{"variable removed", []string{"members/{member}/loans/{loan}"}, []string{"members/{member}/loans"}, []string{"members/{member}/loans/{loan}"}},
		{"pattern removed", []string{"shelves/{shelf}/books/{book}"}, []string{"books/{book}"}, []string{"shelves/{shelf}/books/{book}"}},
		{
			"multi-pattern, one renamed, one dropped",
			[]string{"authors/{author_id}", "publishers/{publisher_id}/authors/{author_id}"},
			[]string{"authors/{authorId}"},
			[]string{"publishers/{publisher_id}/authors/{author_id}"},
		},
		{
			"fewer patterns, one dropped",
			[]string{"authors/{author_id}", "writers/{writer_id}"},
			[]string{"authors/{authorId}"},
			[]string{"writers/{writer_id}"},
		},
		{"all patterns removed", []string{"books/{book}"}, nil, []string{"books/{book}"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := missingPatterns(tt.before, tt.now); !slices.Equal(got, tt.missing) {
				t.Errorf("missingPatterns(%q, %q) = %q, want %q", tt.before, tt.now, got, tt.missing)
			}
		})
	}
}
