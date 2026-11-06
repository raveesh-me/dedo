// Package house runs a repository's house rules, the HOUSE_* rules in its
// dedo.yaml (see package config), as rules of buf-plugin-dedo-lint. Each
// rule is a CEL check of one kind of element; Buf sees it as a rule of its
// own, so lint.use, except and buf:lint:ignore work as for any other rule.
//
// A breaking rule, from a breaking section, runs in buf breaking on each
// element that has one of the same name in the version compared against.
// Its check reads that earlier element's variables as against, such as
// against.scopes.
package house

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"buf.build/go/bufplugin/descriptor"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// Category holds every house rule.
const Category = "HOUSE"

// CategorySpec is the HOUSE category.
var CategorySpec = &check.CategorySpec{ID: Category, Purpose: "Checks the organisation's own rules, from dedo.yaml."}

// variables are the names a check can use, per kind of element.
var variables = map[string][]string{
	"file":     {"name", "proto_package", "imports", "options"},
	"service":  {"name", "full_name", "file", "proto_package", "methods", "resources", "roots", "title", "options"},
	"method":   {"name", "full_name", "file", "proto_package", "service", "input", "output", "client_streaming", "server_streaming", "idempotency", "http_verb", "http_path", "http_bindings", "scopes", "errors", "deprecated", "options", "service_options"},
	"message":  {"name", "full_name", "file", "proto_package", "fields", "options"},
	"field":    {"name", "full_name", "file", "proto_package", "message", "type", "repeated", "optional", "behaviors", "json_name", "options"},
	"enum":     {"name", "full_name", "file", "proto_package", "values", "options"},
	"resource": {"name", "full_name", "file", "proto_package", "type", "patterns", "singular", "plural", "options"},
}

// Specs compiles house rules into rule specs. It fails on a check that does
// not compile, naming the rule and its file.
func Specs(rules []config.Rule) ([]*check.RuleSpec, error) {
	var specs []*check.RuleSpec
	for _, r := range rules {
		prg, err := compile(r)
		if err != nil {
			return nil, err
		}
		spec := &check.RuleSpec{
			ID:          r.ID,
			CategoryIDs: []string{Category},
			Default:     true,
			Purpose:     purpose(r.Message),
			Type:        check.RuleTypeLint,
			Handler:     handler(r, prg),
		}
		if r.Breaking {
			spec.Type, spec.Handler = check.RuleTypeBreaking, pairHandler(r, prg)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// boolVars, listVars and bindingVars are the variables that are not strings. Typed
// variables let a check that is not a bool, or compares the wrong types,
// fail when the rules load rather than at lint time.
var (
	boolVars = []string{"client_streaming", "server_streaming", "deprecated", "repeated", "optional"}
	listVars = []string{"imports", "methods", "resources", "roots", "scopes", "errors", "fields", "behaviors", "values", "patterns"}
	// bindingVars are lists of maps from string to string.
	bindingVars = []string{"http_bindings"}
	// optionVars map an extension's full name to its value, of any type.
	optionVars = []string{"options", "service_options"}
)

func compile(r config.Rule) (cel.Program, error) {
	// The strings extension adds split, lowerAscii, replace, trim and the
	// like, which rule authors reach for. Bindings adds cel.bind, which
	// names a value once instead of repeating the expression for it.
	opts := []cel.EnvOption{ext.Strings(), ext.Bindings()}
	for _, v := range variables[r.On] {
		t := cel.StringType
		switch {
		case slices.Contains(boolVars, v):
			t = cel.BoolType
		case slices.Contains(listVars, v):
			t = cel.ListType(cel.StringType)
		case slices.Contains(bindingVars, v):
			t = cel.ListType(cel.MapType(cel.StringType, cel.StringType))
		case slices.Contains(optionVars, v):
			t = cel.MapType(cel.StringType, cel.DynType)
		}
		opts = append(opts, cel.Variable(v, t))
	}
	if r.Breaking {
		// The earlier element has the same variables; their types are
		// dynamic, so a check compares them with the current ones.
		opts = append(opts, cel.Variable("against", cel.MapType(cel.StringType, cel.DynType)))
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	ast, iss := env.Compile(r.Check)
	if iss.Err() != nil {
		return nil, fmt.Errorf("%s: %s: check does not compile: %w", r.Source, r.ID, iss.Err())
	}
	if t := ast.OutputType(); t != cel.BoolType {
		return nil, fmt.Errorf("%s: %s: check is %s, want a bool", r.Source, r.ID, t)
	}
	return env.Program(ast)
}

// evaluate runs a compiled check and annotates where it fails.
func evaluate(r config.Rule, prg cel.Program, w check.ResponseWriter, vars map[string]any, at ...check.AddAnnotationOption) {
	out, _, err := prg.Eval(vars)
	if err != nil {
		w.AddAnnotation(append(at, check.WithMessage(fmt.Sprintf("%s: the check failed to run: %v", r.ID, err)))...)
		return
	}
	if ok, isBool := out.Value().(bool); isBool && ok {
		return
	}
	w.AddAnnotation(append(at, check.WithMessage(message(r)))...)
}

// handler evaluates the check on every element of the rule's kind.
func handler(r config.Rule, prg cel.Program) check.RuleHandler {
	eval := func(w check.ResponseWriter, req check.Request, d protoreflect.Descriptor, vars map[string]any) {
		if err := addOptions(req, false, d, vars); err != nil {
			w.AddAnnotation(check.WithDescriptor(d), check.WithMessage(fmt.Sprintf("%s: %v", r.ID, err)))
			return
		}
		evaluate(r, prg, w, vars, check.WithDescriptor(d))
	}
	opt := checkutil.WithoutImports()
	switch r.On {
	case "file":
		return checkutil.NewFileRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, f descriptor.FileDescriptor) error {
			fd := f.ProtoreflectFileDescriptor()
			eval(w, req, fd, fileVars(fd))
			return nil
		}, opt)
	case "service":
		return checkutil.NewServiceRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, s protoreflect.ServiceDescriptor) error {
			eval(w, req, s, serviceVars(s))
			return nil
		}, opt)
	case "method":
		return checkutil.NewMethodRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, m protoreflect.MethodDescriptor) error {
			eval(w, req, m, methodVars(m))
			return nil
		}, opt)
	case "message", "resource":
		return checkutil.NewMessageRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, m protoreflect.MessageDescriptor) error {
			if r.On == "message" {
				eval(w, req, m, messageVars(m))
				return nil
			}
			if res := resource(m); res != nil {
				eval(w, req, m, resourceVars(m, res))
			}
			return nil
		}, opt)
	case "field":
		return checkutil.NewFieldRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, f protoreflect.FieldDescriptor) error {
			if !f.IsExtension() {
				eval(w, req, f, fieldVars(f))
			}
			return nil
		}, opt)
	case "enum":
		return checkutil.NewEnumRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, e protoreflect.EnumDescriptor) error {
			eval(w, req, e, enumVars(e))
			return nil
		}, opt)
	}
	panic("house: unknown kind " + r.On) // config validates the kind
}

// pairHandler evaluates a breaking rule's check on every element of its
// kind that the version compared against also has, with that element's
// variables as against.
func pairHandler(r config.Rule, prg cel.Program) check.RuleHandler {
	eval := func(w check.ResponseWriter, req check.Request, d, against protoreflect.Descriptor, vars, before map[string]any) {
		err := addOptions(req, false, d, vars)
		if err == nil {
			err = addOptions(req, true, against, before)
		}
		if err != nil {
			w.AddAnnotation(check.WithDescriptor(d), check.WithAgainstDescriptor(against), check.WithMessage(fmt.Sprintf("%s: %v", r.ID, err)))
			return
		}
		vars["against"] = before
		evaluate(r, prg, w, vars, check.WithDescriptor(d), check.WithAgainstDescriptor(against))
	}
	opt := checkutil.WithoutImports()
	switch r.On {
	case "file":
		return checkutil.NewFilePairRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, f, a descriptor.FileDescriptor) error {
			fd, ad := f.ProtoreflectFileDescriptor(), a.ProtoreflectFileDescriptor()
			eval(w, req, fd, ad, fileVars(fd), fileVars(ad))
			return nil
		}, opt)
	case "service":
		return checkutil.NewServicePairRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, s, a protoreflect.ServiceDescriptor) error {
			eval(w, req, s, a, serviceVars(s), serviceVars(a))
			return nil
		}, opt)
	case "method":
		return checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, m, a protoreflect.MethodDescriptor) error {
			eval(w, req, m, a, methodVars(m), methodVars(a))
			return nil
		}, opt)
	case "message", "resource":
		return checkutil.NewMessagePairRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, m, a protoreflect.MessageDescriptor) error {
			if r.On == "message" {
				eval(w, req, m, a, messageVars(m), messageVars(a))
				return nil
			}
			// Both versions must be resources: a message that becomes one,
			// or stops being one, is no resource to compare.
			if res, before := resource(m), resource(a); res != nil && before != nil {
				eval(w, req, m, a, resourceVars(m, res), resourceVars(a, before))
			}
			return nil
		}, opt)
	case "field":
		return checkutil.NewFieldPairRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, f, a protoreflect.FieldDescriptor) error {
			if !f.IsExtension() {
				eval(w, req, f, a, fieldVars(f), fieldVars(a))
			}
			return nil
		}, opt)
	case "enum":
		return checkutil.NewEnumPairRuleHandler(func(_ context.Context, w check.ResponseWriter, req check.Request, e, a protoreflect.EnumDescriptor) error {
			eval(w, req, e, a, enumVars(e), enumVars(a))
			return nil
		}, opt)
	}
	panic("house: unknown kind " + r.On) // config validates the kind
}

// purpose is a rule's message as Buf's rule list shows it: a sentence that
// starts with a capital letter and ends with a period.
func purpose(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return msg
	}
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

func message(r config.Rule) string {
	if r.Doc != "" {
		return fmt.Sprintf("%s (%s)", r.Message, r.Doc)
	}
	return r.Message
}

func base(d protoreflect.Descriptor) map[string]any {
	return map[string]any{
		"name":          string(d.Name()),
		"full_name":     string(d.FullName()),
		"file":          d.ParentFile().Path(),
		"proto_package": string(d.ParentFile().Package()),
	}
}

func fileVars(fd protoreflect.FileDescriptor) map[string]any {
	var imports []string
	for i := range fd.Imports().Len() {
		imports = append(imports, fd.Imports().Get(i).Path())
	}
	return map[string]any{"name": fd.Path(), "proto_package": string(fd.Package()), "imports": strs(imports)}
}

func serviceVars(s protoreflect.ServiceDescriptor) map[string]any {
	v := base(s)
	var methods []string
	for i := range s.Methods().Len() {
		methods = append(methods, string(s.Methods().Get(i).Name()))
	}
	v["methods"] = strs(methods)
	maps.Copy(v, aggregateVars(s))
	return v
}

func methodVars(m protoreflect.MethodDescriptor) map[string]any {
	v := base(m)
	opts, _ := m.Options().(*descriptorpb.MethodOptions)
	rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
	verb, path := binding(rule)
	rules, _ := proto.GetExtension(m.Options(), dedov1.E_Method).(*dedov1.MethodRules)
	var reasons []string
	for _, e := range rules.GetErrors() {
		reasons = append(reasons, e.GetReason())
	}
	v["service"] = string(m.Parent().FullName())
	v["input"] = string(m.Input().FullName())
	v["output"] = string(m.Output().FullName())
	v["client_streaming"] = m.IsStreamingClient()
	v["server_streaming"] = m.IsStreamingServer()
	v["idempotency"] = strings.TrimPrefix(opts.GetIdempotencyLevel().String(), "IDEMPOTENCY_")
	v["http_verb"] = verb
	v["http_path"] = path
	v["http_bindings"] = bindings(rule)
	v["scopes"] = strs(rules.GetScopes())
	v["errors"] = strs(reasons)
	v["deprecated"] = opts.GetDeprecated()
	return v
}

// bindings lists the verb and path of a google.api.http rule and of each
// of its additional bindings, in order: the rule itself first.
func bindings(rule *annotations.HttpRule) []map[string]string {
	out := []map[string]string{}
	if verb, path := binding(rule); path != "" {
		out = append(out, map[string]string{"verb": verb, "path": path})
	}
	for _, b := range rule.GetAdditionalBindings() {
		out = append(out, bindings(b)...)
	}
	return out
}

// binding returns the verb and path of a google.api.http rule, or empty
// strings.
func binding(rule *annotations.HttpRule) (verb, path string) {
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "GET", p.Get
	case *annotations.HttpRule_Post:
		return "POST", p.Post
	case *annotations.HttpRule_Put:
		return "PUT", p.Put
	case *annotations.HttpRule_Patch:
		return "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		return "DELETE", p.Delete
	case *annotations.HttpRule_Custom:
		return p.Custom.GetKind(), p.Custom.GetPath()
	}
	return "", ""
}

func messageVars(m protoreflect.MessageDescriptor) map[string]any {
	v := base(m)
	var fields []string
	for i := range m.Fields().Len() {
		fields = append(fields, string(m.Fields().Get(i).Name()))
	}
	v["fields"] = strs(fields)
	return v
}

func fieldVars(f protoreflect.FieldDescriptor) map[string]any {
	v := base(f)
	typ := f.Kind().String()
	switch {
	case f.Message() != nil:
		typ = string(f.Message().FullName())
	case f.Enum() != nil:
		typ = string(f.Enum().FullName())
	}
	behaviors, _ := proto.GetExtension(f.Options(), annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	var names []string
	for _, b := range behaviors {
		names = append(names, b.String())
	}
	v["message"] = string(f.Parent().FullName())
	v["type"] = typ
	v["repeated"] = f.IsList()
	v["optional"] = f.HasOptionalKeyword()
	v["behaviors"] = strs(names)
	v["json_name"] = f.JSONName()
	return v
}

func enumVars(e protoreflect.EnumDescriptor) map[string]any {
	v := base(e)
	var values []string
	for i := range e.Values().Len() {
		values = append(values, string(e.Values().Get(i).Name()))
	}
	v["values"] = strs(values)
	return v
}

func resource(m protoreflect.MessageDescriptor) *annotations.ResourceDescriptor {
	res, _ := proto.GetExtension(m.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
	return res
}

func resourceVars(m protoreflect.MessageDescriptor, res *annotations.ResourceDescriptor) map[string]any {
	v := base(m)
	v["type"] = res.GetType()
	v["patterns"] = strs(res.GetPattern())
	v["singular"] = res.GetSingular()
	v["plural"] = res.GetPlural()
	return v
}

// strs returns a non-nil list, so checks like scopes.size() work on none.
func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// addOptions adds the options variables of d, resolved with the extensions
// of one side of the request.
func addOptions(req check.Request, against bool, d protoreflect.Descriptor, vars map[string]any) error {
	types, err := requestTypes(req, against)
	if err != nil {
		return fmt.Errorf("the request's extensions could not be read: %w", err)
	}
	return withOptions(vars, d, types)
}
