package rules

import (
	"context"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/telemetry/fieldattr"
)

// fieldRules reads the dedo.v1.field option, or returns nil.
func fieldRules(f protoreflect.FieldDescriptor) *dedov1.FieldRules {
	return extension[*dedov1.FieldRules](f.Options(), dedov1.E_Field)
}

var fieldAttributeKey = lintRule(
	idFieldAttributeKey,
	"Checks that a field's telemetry attribute key is lowercase, namespaced, outside the semantic conventions' and dedo's namespaces, and used once per message.",
	checkutil.NewMessageRuleHandler(checkAttributeKeys, checkutil.WithoutImports()),
)

func checkAttributeKeys(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MessageDescriptor) error {
	seen := map[string]protoreflect.Name{}
	fields := m.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		key := fieldRules(f).GetAttribute()
		if key == "" {
			continue
		}
		if problem := fieldattr.KeyProblem(key); problem != "" {
			w.AddAnnotation(check.WithDescriptor(f), messagef(idFieldAttributeKey, "Field %q: %s.", f.Name(), problem))
			continue
		}
		if first, ok := seen[key]; ok {
			w.AddAnnotation(check.WithDescriptor(f),
				messagef(idFieldAttributeKey, "Field %q records attribute %q, which field %q records already; give each field its own key.", f.Name(), key, first))
			continue
		}
		seen[key] = f.Name()
	}
	return nil
}

var fieldAttributeSensitive = lintRule(
	idFieldAttributeSensitive,
	"Checks that a sensitive field's telemetry attribute records only whether it is set.",
	checkutil.NewFieldRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, f protoreflect.FieldDescriptor) error {
		if problem := fieldattr.SensitiveProblem(fieldRules(f)); problem != "" {
			w.AddAnnotation(check.WithDescriptor(f), messagef(idFieldAttributeSensitive, "Field %q: %s.", f.Name(), problem))
		}
		return nil
	}, checkutil.WithoutImports()),
)

var fieldAttributeMode = lintRule(
	idFieldAttributeMode,
	"Checks that a field's telemetry attribute mode fits the field: VALUE on a scalar or enum, ITEM_COUNT on a repeated field or map, and no mode without an attribute.",
	checkutil.NewFieldRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, f protoreflect.FieldDescriptor) error {
		if problem := fieldattr.ModeProblem(f, fieldRules(f)); problem != "" {
			w.AddAnnotation(check.WithDescriptor(f), messagef(idFieldAttributeMode, "Field %q: %s.", f.Name(), problem))
		}
		return nil
	}, checkutil.WithoutImports()),
)

// serverErrorCodes are the codes a server span counts as errors, as
// package telemetry does.
var serverErrorCodes = map[string]bool{
	"UNKNOWN": true, "DEADLINE_EXCEEDED": true, "UNIMPLEMENTED": true,
	"INTERNAL": true, "UNAVAILABLE": true, "DATA_LOSS": true,
}

var errorClassificationDeclared = lintRule(
	idErrorClassificationDeclared,
	"Checks that each error classification names a reason the RPC declares, once, and changes how its code counts.",
	checkutil.NewMethodRuleHandler(checkErrorClassifications, checkutil.WithoutImports()),
)

func checkErrorClassifications(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	rules := methodRules(m)
	codes := map[string]string{}
	for _, e := range rules.GetErrors() {
		codes[e.GetReason()] = e.GetCode().String()
	}
	seen := map[string]bool{}
	for _, c := range rules.GetErrorClassifications() {
		reason := c.GetReason()
		code, declared := codes[reason]
		switch {
		case !declared:
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idErrorClassificationDeclared, "RPC %q classifies error reason %q, which it does not declare in errors.", m.Name(), reason))
		case seen[reason]:
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idErrorClassificationDeclared, "RPC %q classifies error reason %q more than once.", m.Name(), reason))
		case serverErrorCodes[code] == c.GetServerError():
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idErrorClassificationDeclared, "RPC %q classifies %q (%s) with server_error %v, as its code counts already; remove the classification.", m.Name(), reason, code, c.GetServerError()))
		}
		seen[reason] = true
	}
	return nil
}

var fieldAttributeKept = breakingRule(
	idFieldAttributeKept,
	"Checks that a field keeps the telemetry attribute it recorded, under the same key and mode, so dashboards, alerts and queries on it keep working.",
	checkutil.NewFieldPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, f, against protoreflect.FieldDescriptor) error {
		before := fieldRules(against)
		if before.GetAttribute() == "" {
			return nil
		}
		now := fieldRules(f)
		switch {
		case now.GetAttribute() == "":
			breakingAt(w, f, against, idFieldAttributeKept, "Field %q no longer records attribute %q, so queries, dashboards and alerts on it stop matching. Keep it.", f.FullName(), before.GetAttribute())
		case now.GetAttribute() != before.GetAttribute():
			breakingAt(w, f, against, idFieldAttributeKept, "Field %q records attribute %q, not %q, so queries, dashboards and alerts on the old key stop matching. Keep the key.", f.FullName(), now.GetAttribute(), before.GetAttribute())
		case fieldattr.Mode(now) != fieldattr.Mode(before):
			breakingAt(w, f, against, idFieldAttributeKept, "Attribute %q of field %q records its %s, not its %s, so queries on it break. Record the new mode under a new key.", now.GetAttribute(), f.FullName(), modeName(fieldattr.Mode(now)), modeName(fieldattr.Mode(before)))
		}
		return nil
	}, checkutil.WithoutImports()),
)

func modeName(m dedov1.AttributeMode) string {
	switch m {
	case dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE:
		return "presence"
	case dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT:
		return "item count"
	default:
		return "value"
	}
}
