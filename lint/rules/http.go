package rules

import (
	"context"
	"regexp"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var httpResponseBody = lintRule(
	idHTTPResponseBody,
	"Checks that HTTP rules of Get, Create and Update RPCs set response_body.",
	checkutil.NewMethodRuleHandler(checkResponseBody, checkutil.WithoutImports()),
)

var httpPathVersion = lintRule(
	idHTTPPathVersion,
	"Checks that every HTTP path contains the package version, such as /v1/, after a fixed root shared by the whole service.",
	checkutil.NewMethodRuleHandler(checkPathVersion, checkutil.WithoutImports()),
)

var versionPattern = regexp.MustCompile(`^v\d+((alpha|beta)\d*)?$`)

func checkResponseBody(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	rule := httpRule(m)
	if rule == nil || !hasVerb(m, "Get", "Create", "Update") {
		return nil
	}
	if rule.GetResponseBody() == "" {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idHTTPResponseBody, "RPC %q HTTP rule must set response_body.", m.Name()))
	}
	return nil
}

// checkPathVersion leaves the root before the version to the owning platform,
// such as /commerce/pim. It only requires the root to be fixed and the same
// for every RPC of the service, so one service never spreads over two roots.
func checkPathVersion(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	rule := httpRule(m)
	version := packageVersion(m.ParentFile().Package())
	if rule == nil || version == "" {
		return nil
	}
	want, wantFrom := serviceRoot(m.Parent().(protoreflect.ServiceDescriptor), version)
	for _, path := range httpPaths(rule) {
		root, ok := pathRoot(path, version)
		switch {
		case !ok:
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idHTTPPathVersion, "RPC %q HTTP path %q must contain the package version as a segment, such as \"/%s/\".", m.Name(), path, version))
		case strings.Contains(root, "{"):
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idHTTPPathVersion, "RPC %q HTTP path %q must not use variables before \"/%s/\".", m.Name(), path, version))
		case root != want:
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idHTTPPathVersion, "RPC %q HTTP path root %q differs from %q, used by %s.", m.Name(), root, want, wantFrom))
		}
	}
	return nil
}

// serviceRoot returns the root of the first HTTP path in the service, and the
// RPC that declares it. Every other path of the service must use the same one.
func serviceRoot(svc protoreflect.ServiceDescriptor, version string) (string, protoreflect.Name) {
	methods := svc.Methods()
	for i := 0; i < methods.Len(); i++ {
		rule := httpRule(methods.Get(i))
		if rule == nil {
			continue
		}
		for _, path := range httpPaths(rule) {
			if root, ok := pathRoot(path, version); ok {
				return root, methods.Get(i).Name()
			}
		}
	}
	return "", ""
}

// pathRoot returns the part of path before its first "/<version>/" segment.
func pathRoot(path, version string) (string, bool) {
	i := strings.Index(path+"/", "/"+version+"/")
	if i < 0 {
		return "", false
	}
	return path[:i], true
}

// packageVersion returns the version component of a package such as
// raveesh_me.library.v1, or "" when the package is unversioned. The
// unversioned case is left to Buf's PACKAGE_VERSION_SUFFIX rule.
func packageVersion(pkg protoreflect.FullName) string {
	last := string(pkg.Name())
	if versionPattern.MatchString(last) {
		return last
	}
	return ""
}

// httpPaths lists the path of the rule and of every additional binding.
func httpPaths(rule *annotations.HttpRule) []string {
	var paths []string
	if p := rulePath(rule); p != "" {
		paths = append(paths, p)
	}
	for _, binding := range rule.GetAdditionalBindings() {
		paths = append(paths, httpPaths(binding)...)
	}
	return paths
}

func rulePath(rule *annotations.HttpRule) string {
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return p.Get
	case *annotations.HttpRule_Put:
		return p.Put
	case *annotations.HttpRule_Post:
		return p.Post
	case *annotations.HttpRule_Delete:
		return p.Delete
	case *annotations.HttpRule_Patch:
		return p.Patch
	case *annotations.HttpRule_Custom:
		return p.Custom.GetPath()
	}
	return ""
}
