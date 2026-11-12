package fieldattr_test

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/structpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/telemetry/fieldattr"
)

func TestKeyProblem(t *testing.T) {
	for key, wantOK := range map[string]bool{
		"todo.priority":     true,
		"acme.orders.count": true,
		"app_x.item_count":  true,
		"priority":          false, // no namespace
		"Todo.priority":     false, // upper case
		"todo..priority":    false,
		"todo.priority.":    false,
		".todo":             false,
		"todo-list.size":    false,
		"rpc.method_extra":  false, // a semantic-conventions namespace
		"http.thing":        false,
		"error.detail":      false,
		"server.region":     false,
		"user.id":           false,
		"dedo.flow_id":      false, // dedo's own
		"otel.anything":     false,
		"":                  false,
	} {
		problem := fieldattr.KeyProblem(key)
		if (problem == "") != wantOK {
			t.Errorf("KeyProblem(%q) = %q, want ok %v", key, problem, wantOK)
		}
	}
}

// fields of google.protobuf.Struct, Value and ListValue cover every shape:
// a map, a repeated field, a message, scalars and an enum.
func field(t *testing.T, msg protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
	t.Helper()
	fd := msg.Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		t.Fatalf("no field %s in %s", name, msg.FullName())
	}
	return fd
}

func TestModeProblem(t *testing.T) {
	value := (&structpb.Value{}).ProtoReflect().Descriptor()
	list := (&structpb.ListValue{}).ProtoReflect().Descriptor()
	st := (&structpb.Struct{}).ProtoReflect().Descriptor()
	file := (&descriptorpb.FileDescriptorProto{}).ProtoReflect().Descriptor()
	const (
		v = dedov1.AttributeMode_ATTRIBUTE_MODE_VALUE
		u = dedov1.AttributeMode_ATTRIBUTE_MODE_UNSPECIFIED
		p = dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE
		n = dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT
	)
	for _, tt := range []struct {
		fd     protoreflect.FieldDescriptor
		mode   dedov1.AttributeMode
		wantOK bool
	}{
		{field(t, value, "string_value"), v, true},
		{field(t, value, "string_value"), u, true},
		{field(t, value, "number_value"), v, true},
		{field(t, value, "bool_value"), v, true},
		{field(t, value, "null_value"), v, true}, // an enum
		{field(t, value, "struct_value"), v, false},
		{field(t, value, "struct_value"), p, true},
		{field(t, value, "struct_value"), n, false},
		{field(t, list, "values"), n, true},
		{field(t, list, "values"), v, false},
		{field(t, list, "values"), p, true},
		{field(t, st, "fields"), n, true}, // a map
		{field(t, st, "fields"), u, false},
		{field(t, file, "name"), n, false},
		{(&structpb.Value{}).ProtoReflect().Descriptor().Fields().ByName("kind"), v, true},
	} {
		if tt.fd == nil {
			continue
		}
		problem := fieldattr.ModeProblem(tt.fd, &dedov1.FieldRules{Attribute: "x.y", AttributeMode: tt.mode})
		if (problem == "") != tt.wantOK {
			t.Errorf("ModeProblem(%s, %v) = %q, want ok %v", tt.fd.FullName(), tt.mode, problem, tt.wantOK)
		}
	}
	bytesField := field(t, (&descriptorpb.FieldOptions{}).ProtoReflect().Descriptor(), "uninterpreted_option")
	if fieldattr.ModeProblem(bytesField, &dedov1.FieldRules{Attribute: "x.y"}) == "" {
		t.Error("VALUE on a repeated message field passed")
	}
	if got := fieldattr.ModeProblem(field(t, value, "string_value"), &dedov1.FieldRules{AttributeMode: n}); !strings.Contains(got, "no attribute") {
		t.Errorf("a mode without an attribute: %q", got)
	}
	if fieldattr.ModeProblem(field(t, value, "string_value"), &dedov1.FieldRules{}) != "" {
		t.Error("no attribute at all is fine")
	}
}

func TestBytesValueRefused(t *testing.T) {
	bytesField := field(t, (&descriptorpb.UninterpretedOption{}).ProtoReflect().Descriptor(), "string_value") // bytes
	if bytesField.Kind() != protoreflect.BytesKind {
		t.Fatalf("%s is %v", bytesField.FullName(), bytesField.Kind())
	}
	if fieldattr.ModeProblem(bytesField, &dedov1.FieldRules{Attribute: "x.y"}) == "" {
		t.Error("VALUE on bytes passed")
	}
	if fieldattr.ModeProblem(bytesField, &dedov1.FieldRules{Attribute: "x.y", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE}) != "" {
		t.Error("PRESENCE on bytes refused")
	}
}

func TestSensitiveProblem(t *testing.T) {
	for _, tt := range []struct {
		rules  *dedov1.FieldRules
		wantOK bool
	}{
		{&dedov1.FieldRules{Sensitive: true}, true},
		{&dedov1.FieldRules{Sensitive: true, Attribute: "x.y", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE}, true},
		{&dedov1.FieldRules{Sensitive: true, Attribute: "x.y"}, false},
		{&dedov1.FieldRules{Sensitive: true, Attribute: "x.y", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_VALUE}, false},
		{&dedov1.FieldRules{Sensitive: true, Attribute: "x.y", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT}, false},
		{&dedov1.FieldRules{Attribute: "x.y"}, true},
		{nil, true},
	} {
		if problem := fieldattr.SensitiveProblem(tt.rules); (problem == "") != tt.wantOK {
			t.Errorf("SensitiveProblem(%v) = %q, want ok %v", tt.rules, problem, tt.wantOK)
		}
	}
}
