package restdoc

import (
	"fmt"
	"strings"
	"unicode"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/raveesh-me/dedo/aggregate"
)

// warnTitles reports, through cfg.Warn, every service whose tag repeats its
// package's heading without the service being named after the root it
// manages. Only a document that spans packages has package headings.
func warnTitles(cfg Config, services []*protogen.Service, mods modules) {
	if cfg.Warn == nil || len(mods) < 2 {
		return
	}
	for _, svc := range services {
		d := svc.Desc
		heading := moduleTitle(d.ParentFile().Package())
		roots := aggregate.Roots(aggregate.Resources(d))
		if msg := titleWarning(string(d.FullName()), string(d.Name()), serviceTag(svc), heading, roots); msg != "" {
			cfg.Warn(msg)
		}
	}
}

// titleWarning is the warning for a service whose tag, title, repeats its
// package's heading, or "" when there is none to give. A repeated heading
// is fine when the service is named after the one root it manages, as
// OrderService titled "Orders" in package orders is; a service that manages
// no resource has no root to suggest.
func titleWarning(fullName, service, title, heading string, roots []aggregate.Resource) string {
	if !strings.EqualFold(title, heading) || len(roots) == 0 {
		return ""
	}
	lead := fmt.Sprintf("%s is titled %q, the same as its package's heading, so the reference reads %q.", fullName, title, heading+" › "+title)
	if len(roots) > 1 {
		types := make([]string, 0, len(roots))
		for _, r := range roots {
			types = append(types, r.Type)
		}
		return lead + fmt.Sprintf(" It manages %d roots (%s): give them one service per root, each named after its root.", len(roots), strings.Join(types, ", "))
	}
	root := roots[0]
	name := root.Type[strings.LastIndex(root.Type, "/")+1:]
	if service == name+"Service" {
		return ""
	}
	hint := name + "Service"
	if t := sentence(root.Plural); t != "" {
		hint += fmt.Sprintf(" titled %q", t)
	}
	return lead + fmt.Sprintf(" Name it after the root it manages, %s: %s.", root.Type, hint)
}

// sentence turns a lowerCamelCase plural into a heading in sentence case:
// taxQuotes reads "Tax quotes".
func sentence(lowerCamel string) string {
	var b strings.Builder
	for i, c := range lowerCamel {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(c))
		case unicode.IsUpper(c):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(c))
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
