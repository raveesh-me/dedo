package rules

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"buf.build/go/bufplugin/descriptor"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// sharedModule is the module name a service keeps for its own shared code:
// internal/shared/ in Go and the shared schema in Postgres.
const sharedModule = "shared"

// packageFieldNumber is FileDescriptorProto's package field, where the
// annotation points.
const packageFieldNumber = 2

var packageSharedReserved = lintRule(
	idPackageSharedReserved,
	"Checks that no proto package names its module shared, such as acme.shared.v1: the name is reserved for a service's shared code.",
	checkutil.NewFileRuleHandler(checkPackageShared, checkutil.WithoutImports()),
)

func checkPackageShared(_ context.Context, w check.ResponseWriter, _ check.Request, fd descriptor.FileDescriptor) error {
	f := fd.ProtoreflectFileDescriptor()
	if moduleName(f.Package()) != sharedModule {
		return nil
	}
	w.AddAnnotation(check.WithFileNameAndSourcePath(f.Path(), protoreflect.SourcePath{packageFieldNumber}),
		messagef(idPackageSharedReserved, "Package %q names its module %q, which is reserved for the service's shared code. Name the module after what it is responsible for; put shared value objects in <context>.type.", f.Package(), sharedModule))
	return nil
}

// moduleName returns a package's module segment: the one before the
// version (acme.orders.v1 gives orders), or the last of an unversioned
// package (acme.type gives type).
func moduleName(pkg protoreflect.FullName) string {
	segments := strings.Split(string(pkg), ".")
	if len(segments) > 1 && versionPattern.MatchString(segments[len(segments)-1]) {
		segments = segments[:len(segments)-1]
	}
	return segments[len(segments)-1]
}
