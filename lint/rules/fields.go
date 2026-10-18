package rules

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const moneyType = "google.type.Money"

var noJSONName = lintRule(
	idNoJSONName,
	"Checks that no field sets json_name.",
	checkutil.NewFieldRuleHandler(checkJSONName, checkutil.WithoutImports()),
)

var moneyFieldType = lintRule(
	idMoneyFieldType,
	"Checks that fields named price, amount, *_price or *_amount use google.type.Money.",
	checkutil.NewFieldRuleHandler(checkMoneyField, checkutil.WithoutImports()),
)

// checkJSONName compares against the protojson default, since compilers
// always fill json_name in descriptors whether or not the source set it.
func checkJSONName(_ context.Context, w check.ResponseWriter, _ check.Request, f protoreflect.FieldDescriptor) error {
	if f.IsExtension() {
		return nil
	}
	if f.JSONName() != defaultJSONName(string(f.Name())) {
		w.AddAnnotation(check.WithDescriptor(f),
			messagef(idNoJSONName, "Field %q sets json_name %q; use the protojson default.", f.Name(), f.JSONName()))
	}
	return nil
}

func checkMoneyField(_ context.Context, w check.ResponseWriter, _ check.Request, f protoreflect.FieldDescriptor) error {
	if !isMoneyName(string(f.Name())) {
		return nil
	}
	if f.Message() == nil || f.Message().FullName() != moneyType || f.IsMap() {
		w.AddAnnotation(check.WithDescriptor(f),
			messagef(idMoneyFieldType, "Field %q must use %s.", f.Name(), moneyType))
	}
	return nil
}

func isMoneyName(name string) bool {
	return name == "price" || name == "amount" ||
		strings.HasSuffix(name, "_price") || strings.HasSuffix(name, "_amount")
}

// defaultJSONName mirrors protoc's lowerCamelCase conversion: drop each
// underscore and upper-case the letter after it.
func defaultJSONName(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		switch {
		case r == '_':
			upper = true
		case upper && r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
			upper = false
		default:
			b.WriteRune(r)
			upper = false
		}
	}
	return b.String()
}
