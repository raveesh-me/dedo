package rules

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"buf.build/go/bufplugin/descriptor"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/raveesh-me/dedo/aggregate"
)

// structureTerms are the words for how a service's code is built, which
// domains.md maps a domain onto. A reader of the reference looks for the
// domain's nouns instead.
var structureTerms = []string{"service", "module", "aggregate", "microservice"}

// serviceIdentifier matches a proto service's name in prose, such as
// OrderService or HTTPService: a capitalised identifier ending in Service.
var serviceIdentifier = regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*Service\b`)

var docsNoCodeStructure = lintRule(
	idDocsNoCodeStructure,
	"Checks that the reference's headings and descriptions name no code structure: a service's heading says no service, module or aggregate, and no description names a proto service.",
	checkutil.NewFileRuleHandler(checkDocsNoCodeStructure, checkutil.WithoutImports()),
)

func checkDocsNoCodeStructure(_ context.Context, w check.ResponseWriter, _ check.Request, fd descriptor.FileDescriptor) error {
	f := fd.ProtoreflectFileDescriptor()
	c := docsChecker{w: w, domainNames: declaredNames(f)}
	pkg := f.SourceLocations().ByPath(protoreflect.SourcePath{packageFieldNumber})
	if name := c.serviceNamed(pkg.LeadingComments); name != "" {
		w.AddAnnotation(check.WithFileNameAndSourcePath(f.Path(), protoreflect.SourcePath{packageFieldNumber}),
			messagef(idDocsNoCodeStructure, "The comment on package %q names the service %s; the reference shows it as the package's description. Say what the package is responsible for instead.", f.Package(), name))
	}
	for i := range f.Services().Len() {
		c.service(f.Services().Get(i))
	}
	c.messages(f.Messages())
	c.enums(f.Enums())
	return nil
}

// docsChecker reports the text of one file that reaches readers.
type docsChecker struct {
	w check.ResponseWriter
	// domainNames are the messages and enums the file can see: a name
	// ending in Service that is one of them is the domain's noun.
	domainNames map[string]bool
}

func (c docsChecker) service(s protoreflect.ServiceDescriptor) {
	heading, fromTitle := serviceHeading(s)
	if term := structureTerm(heading, resourceWords(s)); term != "" {
		where := "its name without the Service suffix"
		if fromTitle {
			where = "its title"
		}
		c.w.AddAnnotation(check.WithDescriptor(s),
			messagef(idDocsNoCodeStructure, "Service %q is headed %q in the reference, from %s, and %q names code structure. Set a dedo.v1.service title with the noun readers look for, such as \"Orders\" for OrderService.", s.Name(), heading, where, term))
	}
	c.text(s, "service")
	for i := range s.Methods().Len() {
		m := s.Methods().Get(i)
		tool := methodRules(m).GetTool()
		c.text(m, "RPC", tool.GetTitle(), tool.GetDescription())
	}
}

func (c docsChecker) messages(ms protoreflect.MessageDescriptors) {
	for i := range ms.Len() {
		m := ms.Get(i)
		if m.IsMapEntry() {
			continue
		}
		c.text(m, "message", messageRules(m).GetDescription())
		for j := range m.Fields().Len() {
			c.text(m.Fields().Get(j), "field")
		}
		c.messages(m.Messages())
		c.enums(m.Enums())
	}
}

func (c docsChecker) enums(es protoreflect.EnumDescriptors) {
	for i := range es.Len() {
		e := es.Get(i)
		c.text(e, "enum")
		for j := range e.Values().Len() {
			c.text(e.Values().Get(j), "enum value")
		}
	}
}

// text reports a descriptor whose leading comment, or one of extra (its
// option texts), names a proto service.
func (c docsChecker) text(d protoreflect.Descriptor, kind string, extra ...string) {
	texts := append([]string{d.ParentFile().SourceLocations().ByDescriptor(d).LeadingComments}, extra...)
	for _, t := range texts {
		name := c.serviceNamed(t)
		switch {
		case name == "":
			continue
		case kind == "service" && name == string(d.Name()):
			c.w.AddAnnotation(check.WithDescriptor(d),
				messagef(idDocsNoCodeStructure, "The comment on service %q names it by its proto name, which callers of the REST API never see; the reference shows the comment as its description. Start with what it does instead, such as \"Manages orders.\"", d.Name()))
		default:
			c.w.AddAnnotation(check.WithDescriptor(d),
				messagef(idDocsNoCodeStructure, "The text of %s %q names the service %s; readers of the reference call operations, not services. Say what the API does instead.", kind, d.Name(), name))
		}
		return
	}
}

// serviceNamed returns the first proto service name in text, or "".
func (c docsChecker) serviceNamed(text string) string {
	for _, name := range serviceIdentifier.FindAllString(text, -1) {
		if !c.domainNames[name] {
			return name
		}
	}
	return ""
}

// declaredNames are the top-level and nested messages and enums of f and of
// the files it imports.
func declaredNames(f protoreflect.FileDescriptor) map[string]bool {
	names := map[string]bool{}
	var walk func(ms protoreflect.MessageDescriptors, es protoreflect.EnumDescriptors)
	walk = func(ms protoreflect.MessageDescriptors, es protoreflect.EnumDescriptors) {
		for i := range ms.Len() {
			names[string(ms.Get(i).Name())] = true
			walk(ms.Get(i).Messages(), ms.Get(i).Enums())
		}
		for i := range es.Len() {
			names[string(es.Get(i).Name())] = true
		}
	}
	walk(f.Messages(), f.Enums())
	for i := range f.Imports().Len() {
		imp := f.Imports().Get(i).FileDescriptor
		walk(imp.Messages(), imp.Enums())
	}
	return names
}

// serviceHeading is the service's heading in the reference, as the REST
// generator names its tag: its title, else its name without the Service
// suffix.
func serviceHeading(s protoreflect.ServiceDescriptor) (heading string, fromTitle bool) {
	if title := strings.TrimSpace(serviceRules(s).GetTitle()); title != "" {
		return title, true
	}
	name := string(s.Name())
	if trimmed := strings.TrimSuffix(name, "Service"); trimmed != "" {
		name = trimmed
	}
	return name, false
}

// structureTerm returns the first structure term among heading's words,
// singular or plural, that the domain does not use for a resource of its
// own, or "".
func structureTerm(heading string, domain []string) string {
	for _, word := range words(heading) {
		for _, term := range structureTerms {
			if (word == term || word == term+"s") && !slices.Contains(domain, term) {
				return word
			}
		}
	}
	return ""
}

// resourceWords are the words of the type names of the resources s
// manages: library.example.com/ServiceArea gives service and area.
func resourceWords(s protoreflect.ServiceDescriptor) []string {
	var out []string
	for _, r := range aggregate.Resources(s) {
		name := r.Type[strings.LastIndex(r.Type, "/")+1:]
		for _, word := range words(name) {
			out = append(out, strings.TrimSuffix(word, "s"))
		}
	}
	return out
}

// words splits text into lower-case words, at spaces and punctuation and
// where a capital letter starts a word in CamelCase: "CatalogModule" and
// "Catalog module" both give catalog and module.
func words(text string) []string {
	var b strings.Builder
	r := []rune(text)
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]) ||
			unicode.IsUpper(r[i-1]) && i+1 < len(r) && unicode.IsLower(r[i+1])) {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return strings.FieldsFunc(strings.ToLower(b.String()), func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	})
}
