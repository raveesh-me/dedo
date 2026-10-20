// Package aip runs Google's API linter (github.com/googleapis/api-linter)
// as Buf lint rules, so buf-plugin-dedo-lint checks the AIPs in the same run as
// its own rules.
//
// Rule IDs and categories match hestiatechnology/buf-plugin-aip, whose
// approach this follows (Apache-2.0; see NOTICE), so buf.yaml files that
// used it keep working: core::0131::http-method is AIP_0131_HTTP_METHOD, in
// the categories AIP, AIP_0131, AIP_CORE, AIP_CRUD, AIP_NOLANG and
// AIP_RECOMMENDED.
//
// The linter runs once per request, in Before; each rule then reports its
// share of the problems.
package aip

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/option"
	"github.com/googleapis/api-linter/v2/lint"
	"github.com/googleapis/api-linter/v2/rules"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Categories every AIP rule can belong to, besides AIP_<number>.
const (
	CategoryAll         = "AIP"
	CategoryCore        = "AIP_CORE"
	CategoryClientLibs  = "AIP_CLIENT_LIBRARIES"
	CategoryRecommended = "AIP_RECOMMENDED"
	CategoryCRUD        = "AIP_CRUD"
	CategoryNoLang      = "AIP_NOLANG"
)

// crudAIPs are the standard method AIPs, 131 to 136.
var crudAIPs = map[string]bool{"0131": true, "0132": true, "0133": true, "0134": true, "0135": true, "0136": true}

type problemsKey struct{}

type problem struct {
	filePath string
	problem  lint.Problem
}

// Rules returns the AIP rules, their categories, and the Before hook that
// runs the linter.
func Rules() ([]*check.RuleSpec, []*check.CategorySpec, func(context.Context, check.Request) (context.Context, check.Request, error), error) {
	registry := lint.NewRuleRegistry()
	if err := rules.Add(registry); err != nil {
		return nil, nil, nil, fmt.Errorf("aip: registering api-linter rules: %w", err)
	}
	aipNumbers := map[string]bool{}
	var specs []*check.RuleSpec
	for name := range registry {
		parts := strings.Split(string(name), "::")
		if len(parts) != 3 {
			return nil, nil, nil, fmt.Errorf("aip: unexpected rule name %q", name)
		}
		group, number := parts[0], parts[1]
		id := RuleID(name)
		aipNumbers[number] = true
		specs = append(specs, &check.RuleSpec{
			ID:          id,
			CategoryIDs: categories(group, number, id),
			Default:     group == "core",
			Purpose:     fmt.Sprintf("Checks AIP rule %s.", name),
			Type:        check.RuleTypeLint,
			Handler:     handler(id),
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs, categorySpecs(aipNumbers), before(registry), nil
}

func categories(group, number, id string) []string {
	out := []string{CategoryAll, "AIP_" + number}
	if group == "core" {
		out = append(out, CategoryCore)
	}
	if group == "client-libraries" {
		out = append(out, CategoryClientLibs)
	}
	if crudAIPs[number] {
		out = append(out, CategoryCRUD)
	}
	language := false
	for _, lang := range []string{"JAVA", "CSHARP", "PHP", "RUBY"} {
		language = language || strings.HasPrefix(id, "AIP_0191_"+lang+"_")
	}
	if group == "core" && !language {
		out = append(out, CategoryNoLang)
		comment := strings.HasPrefix(id, "AIP_0192_")
		preposition := id == "AIP_0140_PREPOSITIONS" || id == "AIP_0136_PREPOSITIONS"
		if !comment && !preposition {
			out = append(out, CategoryRecommended)
		}
	}
	return out
}

func categorySpecs(numbers map[string]bool) []*check.CategorySpec {
	out := []*check.CategorySpec{
		{ID: CategoryAll, Purpose: "Checks all API Improvement Proposals (https://aip.dev)."},
		{ID: CategoryCore, Purpose: "Checks core API Improvement Proposals."},
		{ID: CategoryClientLibs, Purpose: "Checks client library API Improvement Proposals."},
		{ID: CategoryRecommended, Purpose: "Checks recommended core rules, without comment and language-specific packaging checks."},
		{ID: CategoryCRUD, Purpose: "Checks standard resource methods (AIP-131 to AIP-136)."},
		{ID: CategoryNoLang, Purpose: "Checks core rules without language-specific packaging requirements."},
	}
	for n := range numbers {
		trimmed := strings.TrimLeft(n, "0")
		out = append(out, &check.CategorySpec{
			ID:      "AIP_" + n,
			Purpose: fmt.Sprintf("Checks AIP-%s (https://aip.dev/%s).", trimmed, trimmed),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RuleID converts an api-linter rule name to its Buf rule ID:
// core::0131::http-method becomes AIP_0131_HTTP_METHOD.
func RuleID(name lint.RuleName) string {
	parts := strings.Split(string(name), "::")
	if len(parts) < 3 {
		s := strings.NewReplacer("::", "_", "-", "_").Replace(string(name))
		return "AIP_" + strings.ToUpper(s)
	}
	return "AIP_" + parts[1] + "_" + strings.ToUpper(strings.ReplaceAll(parts[2], "-", "_"))
}

func handler(id string) check.RuleHandler {
	return check.RuleHandlerFunc(func(ctx context.Context, w check.ResponseWriter, _ check.Request) error {
		problems, ok := ctx.Value(problemsKey{}).(map[string][]problem)
		if !ok {
			return errors.New("aip: lint results missing; the Before hook did not run")
		}
		for _, p := range problems[id] {
			report(w, p)
		}
		return nil
	})
}

// before runs the API linter once over the files being linted. The plugin
// option "config" names an api-linter configuration file.
func before(registry lint.RuleRegistry) func(context.Context, check.Request) (context.Context, check.Request, error) {
	return func(ctx context.Context, req check.Request) (context.Context, check.Request, error) {
		var files []protoreflect.FileDescriptor
		for _, fd := range req.FileDescriptors() {
			if !fd.IsImport() {
				files = append(files, fd.ProtoreflectFileDescriptor())
			}
		}
		problems := map[string][]problem{}
		if len(files) == 0 {
			return context.WithValue(ctx, problemsKey{}, problems), req, nil
		}
		configs := lint.Configs{{EnabledRules: []string{"all"}}}
		path, err := option.GetStringValue(req.Options(), "config")
		if err != nil {
			return nil, nil, err
		}
		if path != "" {
			if configs, err = lint.ReadConfigsFromFile(path); err != nil {
				return nil, nil, fmt.Errorf("aip: reading api-linter config %q: %w", path, err)
			}
		}
		responses, err := lint.New(registry, configs).LintProtos(files...)
		if err != nil {
			return nil, nil, fmt.Errorf("aip: api-linter: %w", err)
		}
		for _, resp := range responses {
			for _, p := range resp.Problems {
				// api-linter predates Protobuf Editions and wants proto3.
				if p.RuleID == "core::0191::proto-version" {
					if f, ok := p.Descriptor.(protoreflect.FileDescriptor); ok && f.Syntax() == protoreflect.Editions {
						continue
					}
				}
				path := resp.FilePath
				if path == "" && p.Descriptor != nil && p.Descriptor.ParentFile() != nil {
					path = p.Descriptor.ParentFile().Path()
				}
				id := RuleID(p.RuleID)
				problems[id] = append(problems[id], problem{filePath: path, problem: p})
			}
		}
		return context.WithValue(ctx, problemsKey{}, problems), req, nil
	}
}

func report(w check.ResponseWriter, p problem) {
	message := p.problem.Message
	if uri := p.problem.GetRuleURI(); uri != "" {
		message = fmt.Sprintf("%s (%s)", message, uri)
	}
	opts := []check.AddAnnotationOption{check.WithMessage(message)}
	switch loc := p.problem.Location; {
	case loc != nil && len(loc.GetPath()) > 0:
		path := make(protoreflect.SourcePath, len(loc.GetPath()))
		for i, v := range loc.GetPath() {
			path[i] = v
		}
		opts = append(opts, check.WithFileNameAndSourcePath(p.filePath, path))
	case p.problem.Descriptor != nil:
		opts = append(opts, check.WithDescriptor(p.problem.Descriptor))
	case p.filePath != "":
		opts = append(opts, check.WithFileName(p.filePath))
	}
	w.AddAnnotation(opts...)
}
