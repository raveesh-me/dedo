package aip

import (
	"strings"

	"github.com/googleapis/api-linter/v2/lint"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// collectionSuffixRule lets api-linter compare kebab-case collection names
// with RPC nouns. Only this rule sees the normalized copy of the HTTP options;
// other rules and diagnostics retain the original descriptor and paths.
func collectionSuffixRule(upstream *lint.MethodRule) *lint.MethodRule {
	rule := *upstream
	rule.LintMethod = func(method protoreflect.MethodDescriptor) []lint.Problem {
		problems := upstream.LintMethod(method)
		if len(problems) == 0 || !proto.HasExtension(method.Options(), annotations.E_Http) {
			return problems
		}
		options := proto.Clone(method.Options())
		httpRule := proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
		normalizeCollection(httpRule, string(method.Name()))
		if len(upstream.LintMethod(methodWithOptions{method, options})) == 0 {
			return nil
		}
		return problems
	}
	return &rule
}

type methodWithOptions struct {
	protoreflect.MethodDescriptor
	options protoreflect.ProtoMessage
}

func (m methodWithOptions) Options() protoreflect.ProtoMessage { return m.options }

func normalizeCollection(rule *annotations.HttpRule, method string) {
	normalize := func(uri string) string {
		path, suffix, ok := strings.Cut(uri, ":")
		if !ok {
			return uri
		}
		start := strings.LastIndex(path, "/") + 1
		segment := path[start:]
		noun := strings.ReplaceAll(segment, "-", "")
		// Only literal collections qualify, never path-template variables.
		if !strings.Contains(segment, "-") || strings.ContainsAny(segment, "{}*") ||
			!strings.HasSuffix(strings.ToLower(method), strings.ToLower(noun)) {
			return uri
		}
		return path[:start] + noun + ":" + suffix
	}
	switch pattern := rule.Pattern.(type) {
	case *annotations.HttpRule_Get:
		pattern.Get = normalize(pattern.Get)
	case *annotations.HttpRule_Post:
		pattern.Post = normalize(pattern.Post)
	case *annotations.HttpRule_Put:
		pattern.Put = normalize(pattern.Put)
	case *annotations.HttpRule_Patch:
		pattern.Patch = normalize(pattern.Patch)
	case *annotations.HttpRule_Delete:
		pattern.Delete = normalize(pattern.Delete)
	case *annotations.HttpRule_Custom:
		pattern.Custom.Path = normalize(pattern.Custom.Path)
	}
	for _, binding := range rule.AdditionalBindings {
		normalizeCollection(binding, method)
	}
}
