package restdoc

import (
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// packageField is the field number of package in FileDescriptorProto: the
// source path of a file's package statement and its comments.
const packageField = 2

// versionSegment matches the version a package ends in, as buf's
// PACKAGE_VERSION_SUFFIX accepts it: v1, v2beta1, v1p1alpha1, v1test.
var versionSegment = regexp.MustCompile(`^v[0-9]+(test.*|(alpha|beta)[0-9]*|p[0-9]+(alpha|beta)[0-9]*)?$`)

// module is one proto package: a module of the domain model, in the sense of
// guidance/domains.md, and the services of the API it holds.
type module struct {
	pkg         protoreflect.FullName
	description string
	services    []Tag
}

// modules are the packages of the documented services, in the order their
// first service appears.
type modules []module

// with returns mods with the tag of a service of file f added to the
// module of f's package. The module's description is the first leading
// comment of a package statement among its files.
func (mods modules) with(f *protogen.File, t Tag) modules {
	pkg := f.Desc.Package()
	out := make(modules, 0, len(mods)+1)
	found := false
	for _, m := range mods {
		if m.pkg == pkg {
			found = true
			m = module{
				pkg:         m.pkg,
				description: firstNonEmpty(m.description, packageComment(f)),
				services:    append(append([]Tag(nil), m.services...), t),
			}
		}
		out = append(out, m)
	}
	if !found {
		out = append(out, module{pkg: pkg, description: packageComment(f), services: []Tag{t}})
	}
	return out
}

// tags are the document's tags. A document whose services share one package
// lists one tag per service, as before. One that spans packages also lists
// a navigation tag per package, named after the package, and nests each
// service's tag under it. Operations name their tag by name alone, so two
// tags with one name are an error: retitle one of the services.
func (mods modules) tags() ([]Tag, error) {
	var out []Tag
	for _, m := range mods {
		if len(mods) > 1 {
			if m.pkg == "" {
				return nil, fmt.Errorf("services %s are in a file without a package statement; the reference cannot group them with the other packages' services", serviceNames(m.services))
			}
			out = append(out, Tag{
				Name:        string(m.pkg),
				Summary:     moduleTitle(m.pkg),
				Description: m.description,
				Kind:        "nav",
			})
		}
		for _, t := range m.services {
			if len(mods) > 1 {
				t.Parent = string(m.pkg)
			}
			out = append(out, t)
		}
	}
	return out, uniqueNames(out)
}

// uniqueNames reports tags that share a name.
func uniqueNames(tags []Tag) error {
	seen := map[string]Tag{}
	for _, t := range tags {
		if prev, ok := seen[t.Name]; ok {
			return fmt.Errorf("two tags are named %q (in %s and %s): give one of the services a different dedo.v1.service title", t.Name, tagPlace(prev), tagPlace(t))
		}
		seen[t.Name] = t
	}
	return nil
}

// tagPlace says where a tag comes from, for error messages.
func tagPlace(t Tag) string {
	switch {
	case t.Kind == "nav":
		return "package " + t.Name
	case t.Parent != "":
		return "a service of package " + t.Parent
	default:
		return "a service"
	}
}

// serviceNames lists the names of service tags, for error messages.
func serviceNames(tags []Tag) string {
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, fmt.Sprintf("%q", t.Name))
	}
	return strings.Join(names, ", ")
}

// moduleTitle is how a reference shows a package: its last segment before
// the version, in sentence case, so taxes.content.v1 reads "Content" and
// shop.tax_profile.v1 reads "Tax profile".
func moduleTitle(pkg protoreflect.FullName) string {
	parts := strings.Split(string(pkg), ".")
	if len(parts) > 1 && versionSegment.MatchString(parts[len(parts)-1]) {
		parts = parts[:len(parts)-1]
	}
	name := strings.ReplaceAll(parts[len(parts)-1], "_", " ")
	if name == "" {
		return string(pkg)
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// packageComment is the leading comment of f's package statement.
func packageComment(f *protogen.File) string {
	loc := f.Desc.SourceLocations().ByPath(protoreflect.SourcePath{packageField})
	return cleanComment(protogen.Comments(loc.LeadingComments))
}
