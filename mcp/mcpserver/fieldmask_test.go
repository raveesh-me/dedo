package mcpserver

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/fieldmaskpb"
)

// patchMessage builds dedo.test.v1.Patch: a field mask, a nested Patch and
// a repeated Patch.
func patchMessage(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	message := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("patch.proto"),
		Package:    proto.String("dedo.test.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/field_mask.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Patch"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: proto.String("update_mask"), JsonName: proto.String("updateMask"), Number: proto.Int32(1), Label: optional, Type: message, TypeName: proto.String(".google.protobuf.FieldMask")},
				{Name: proto.String("child"), JsonName: proto.String("child"), Number: proto.Int32(2), Label: optional, Type: message, TypeName: proto.String(".dedo.test.v1.Patch")},
				{Name: proto.String("items"), JsonName: proto.String("items"), Number: proto.Int32(3), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: message, TypeName: proto.String(".dedo.test.v1.Patch")},
			},
		}},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Messages().Get(0)
}

func TestNormalizeMasks(t *testing.T) {
	md := patchMessage(t)
	tests := map[string]string{
		`{"update_mask":"budget_amount, todo.assignee_email"}`: `{"update_mask":"budgetAmount,todo.assigneeEmail"}`,
		`{"updateMask":"budgetAmount"}`:                        `{"updateMask":"budgetAmount"}`,
		`{"child":{"update_mask":"a_b"}}`:                      `{"child":{"update_mask":"aB"}}`,
		`{"items":[{"update_mask":"a_b"},{"updateMask":"c"}]}`: `{"items":[{"update_mask":"aB"},{"updateMask":"c"}]}`,
		`{"unknown":"a_b"}`:                                    `{"unknown":"a_b"}`,
	}
	for in, want := range tests {
		got, err := normalizeMasks(md, json.RawMessage(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if string(got) != want {
			t.Errorf("normalizeMasks(%s) = %s, want %s", in, got, want)
		}
	}
	if _, err := normalizeMasks(md, json.RawMessage(`[1]`)); err == nil {
		t.Error("normalizeMasks accepted a JSON array")
	}
}

func TestNormalizeMasksWithoutMask(t *testing.T) {
	in := json.RawMessage(`{"name":"things/a_b"}`)
	got, err := normalizeMasks(service(t).Methods().ByName("GetThing").Input(), in)
	if err != nil || string(got) != string(in) {
		t.Errorf("normalizeMasks = %s, %v; want the arguments unchanged", got, err)
	}
}
