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
	"RAVEESH_ME_HTTP_RESPONSE_BODY",
	"Checks that HTTP rules of Get, Create and Update RPCs set response_body.",
	checkutil.NewMethodRuleHandler(checkResponseBody, checkutil.WithoutImports()),
)

var httpPathVersion = lintRule(
	"RAVEESH_ME_HTTP_PATH_VERSION",
	"Checks that every HTTP path starts with the package's major version, such as /v1/.",
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
			check.WithMessagef("RPC %q HTTP rule must set response_body.", m.Name()))
	}
	return nil
}

func checkPathVersion(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	rule := httpRule(m)
	version := packageVersion(m.ParentFile().Package())
	if rule == nil || version == "" {
		return nil
	}
	prefix := "/" + version + "/"
	for _, path := range httpPaths(rule) {
		if !strings.HasPrefix(path, prefix) {
			w.AddAnnotation(check.WithDescriptor(m),
				check.WithMessagef("RPC %q HTTP path %q must start with %q.", m.Name(), path, prefix))
		}
	}
	return nil
}

// packageVersion returns the version component of a package such as
// raveesh_me.library.v1, or "" when the package is unversioned.
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
