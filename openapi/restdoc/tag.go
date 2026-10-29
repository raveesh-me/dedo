package restdoc

import (
	"strings"
	"unicode"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// serviceTag is the tag of svc's operations.
func serviceTag(svc *protogen.Service) string {
	rules, _ := proto.GetExtension(svc.Desc.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	return tagName(string(svc.Desc.Name()), rules.GetTitle())
}

// tagName is the tag of a service's operations: its declared title, else the
// service name without the Service suffix buf requires, split into words.
// References show it as the group's heading, so CatalogEntryService reads
// "Catalog Entry".
func tagName(service, title string) string {
	if title != "" {
		return title
	}
	name := service
	if trimmed := strings.TrimSuffix(service, "Service"); trimmed != "" {
		name = trimmed
	}
	r := []rune(name)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			// A word starts at an upper-case letter after a lower-case
			// letter or digit (EntryService), or at the last capital of an
			// acronym followed by a lower-case letter (HTTPProxy).
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(c)
	}
	return b.String()
}
