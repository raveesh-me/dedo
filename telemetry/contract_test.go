package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/policy"
	"github.com/raveesh-me/dedo/telemetry"
)

const (
	thingService = "raveesh_me.thing.v1.ThingService"
	findThing    = "/" + thingService + "/FindThing"
	secret       = "secret.person@example.com"
)

// Reasons FindThing declares, and how the contract classifies them.
const (
	reasonBusy     = "THING_BUSY"     // UNAVAILABLE, but not a server error
	reasonVanished = "THING_VANISHED" // NOT_FOUND, but a server error
)

func fieldOpts(rules *dedov1.FieldRules) *descriptorpb.FieldOptions {
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, dedov1.E_Field, rules)
	return opts
}

func scalar(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, rules *dedov1.FieldRules) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name: proto.String(name), Number: proto.Int32(number), JsonName: proto.String(name),
		Type: typ.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Options: fieldOpts(rules),
	}
}

// thingFile declares FindThing, whose request records every kind of
// attribute. edit changes it before it is built.
func thingFile(edit func(*descriptorpb.FileDescriptorProto)) *descriptorpb.FileDescriptorProto {
	const (
		str  = descriptorpb.FieldDescriptorProto_TYPE_STRING
		i64  = descriptorpb.FieldDescriptorProto_TYPE_INT64
		bl   = descriptorpb.FieldDescriptorProto_TYPE_BOOL
		dbl  = descriptorpb.FieldDescriptorProto_TYPE_DOUBLE
		enum = descriptorpb.FieldDescriptorProto_TYPE_ENUM
		msg  = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	)
	value := func(key string) *dedov1.FieldRules { return &dedov1.FieldRules{Attribute: key} }
	tags := scalar("tags", 3, str, &dedov1.FieldRules{Attribute: "thing.tag_count", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT})
	tags.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	color := scalar("color", 6, enum, value("thing.color"))
	color.TypeName = proto.String(".raveesh_me.thing.v1.Color")
	inner := scalar("inner", 4, msg, nil)
	inner.TypeName = proto.String(".raveesh_me.thing.v1.Inner")
	inner.Options = nil
	request := &descriptorpb.DescriptorProto{
		Name: proto.String("FindThingRequest"),
		Field: []*descriptorpb.FieldDescriptorProto{
			scalar("kind", 1, str, value("thing.kind")),
			scalar("email", 2, str, &dedov1.FieldRules{Sensitive: true, Attribute: "thing.has_email", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE}),
			tags,
			inner,
			scalar("size", 5, i64, value("thing.size")),
			color,
			scalar("weight", 7, dbl, value("thing.weight")),
			scalar("note", 8, str, nil),
		},
	}
	innerMsg := &descriptorpb.DescriptorProto{
		Name:  proto.String("Inner"),
		Field: []*descriptorpb.FieldDescriptorProto{scalar("flag", 1, bl, value("thing.inner_flag"))},
	}
	methodOpts := &descriptorpb.MethodOptions{}
	proto.SetExtension(methodOpts, dedov1.E_Method, &dedov1.MethodRules{
		Scopes: []string{"thing.thing.read"},
		Errors: []*dedov1.ErrorReason{
			{Reason: reasonBusy, Code: code.Code_UNAVAILABLE},
			{Reason: reasonVanished, Code: code.Code_NOT_FOUND},
		},
		ErrorClassifications: []*dedov1.ErrorClassification{
			{Reason: reasonBusy, ServerError: false},
			{Reason: reasonVanished, ServerError: true},
		},
	})
	svcOpts := &descriptorpb.ServiceOptions{}
	proto.SetExtension(svcOpts, dedov1.E_Service, &dedov1.ServiceRules{Audience: dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC, OwnerTeam: "t"})
	f := &descriptorpb.FileDescriptorProto{
		Name: proto.String("raveesh_me/thing/v1/thing.proto"), Package: proto.String("raveesh_me.thing.v1"), Syntax: proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{request, innerMsg},
		EnumType: []*descriptorpb.EnumDescriptorProto{{Name: proto.String("Color"), Value: []*descriptorpb.EnumValueDescriptorProto{
			{Name: proto.String("COLOR_UNSPECIFIED"), Number: proto.Int32(0)}, {Name: proto.String("COLOR_RED"), Number: proto.Int32(1)},
		}}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("ThingService"), Options: svcOpts,
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name: proto.String("FindThing"), InputType: proto.String(".raveesh_me.thing.v1.FindThingRequest"),
				OutputType: proto.String(".raveesh_me.thing.v1.FindThingRequest"), Options: methodOpts,
			}},
		}},
	}
	if edit != nil {
		edit(f)
	}
	return f
}

func thingTable(t *testing.T, edit func(*descriptorpb.FileDescriptorProto)) (policy.Table, protoreflect.MessageDescriptor) {
	t.Helper()
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto), thingFile(edit),
	}})
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, thingService)
	if err != nil {
		t.Fatal(err)
	}
	d, err := files.FindDescriptorByName("raveesh_me.thing.v1.FindThingRequest")
	if err != nil {
		t.Fatal(err)
	}
	return table, d.(protoreflect.MessageDescriptor)
}

// thingServer serves FindThing behind the interceptor, logging once with
// the handler's context, and failing with fail when it is set.
type thingServer struct {
	*signals
	server *connect.Server
	desc   protoreflect.MessageDescriptor
	log    *bytes.Buffer
}

func newThingServer(t *testing.T, fail error) thingServer {
	t.Helper()
	s := newSignals(t)
	table, desc := thingTable(t, nil)
	cfg := s.cfg
	cfg.Table = table
	observe, err := telemetry.NewServerInterceptor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&buf, nil)))
	server := connect.NewServer(observe)
	server.Register(connect.Method{
		Spec: connect.Spec{Procedure: findThing, StreamType: connect.StreamTypeUnary},
		Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			req := dynamicpb.NewMessage(desc)
			if err := stream.Receive(req); err != nil {
				return err
			}
			log.InfoContext(ctx, "finding")
			if fail != nil {
				return fail
			}
			return stream.Send(dynamicpb.NewMessage(desc))
		},
	})
	return thingServer{signals: s, server: server, desc: desc, log: &buf}
}

// call sends a request built from protojson-like field values.
func (ts thingServer) call(t *testing.T, set func(m *dynamicpb.Message)) error {
	t.Helper()
	req := dynamicpb.NewMessage(ts.desc)
	if set != nil {
		set(req)
	}
	client := connect.NewClient(connectinprocess.New(ts.server))
	return client.CallUnary(t.Context(), connect.Spec{Procedure: findThing, StreamType: connect.StreamTypeUnary}, req, dynamicpb.NewMessage(ts.desc))
}

func setField(m *dynamicpb.Message, name string, v protoreflect.Value) {
	m.Set(m.Descriptor().Fields().ByName(protoreflect.Name(name)), v)
}

func fullRequest(m *dynamicpb.Message) {
	setField(m, "kind", protoreflect.ValueOfString("widget"))
	setField(m, "email", protoreflect.ValueOfString(secret))
	tags := m.Mutable(m.Descriptor().Fields().ByName("tags")).List()
	for _, tag := range []string{"a", "b", "c"} {
		tags.Append(protoreflect.ValueOfString(tag))
	}
	inner := m.Mutable(m.Descriptor().Fields().ByName("inner")).Message()
	inner.Set(inner.Descriptor().Fields().ByName("flag"), protoreflect.ValueOfBool(true))
	setField(m, "size", protoreflect.ValueOfInt64(7))
	setField(m, "color", protoreflect.ValueOfEnum(1))
	setField(m, "weight", protoreflect.ValueOfFloat64(1.5))
	setField(m, "note", protoreflect.ValueOfString("not declared"))
}

func TestFieldAttributesOnSpanAndLogs(t *testing.T) {
	ts := newThingServer(t, nil)
	if err := ts.call(t, fullRequest); err != nil {
		t.Fatal(err)
	}
	got := attrs(ts.only().Attributes())
	want := map[attribute.Key]attribute.Value{
		"thing.kind":       attribute.StringValue("widget"),
		"thing.has_email":  attribute.BoolValue(true),
		"thing.tag_count":  attribute.IntValue(3),
		"thing.inner_flag": attribute.BoolValue(true),
		"thing.size":       attribute.Int64Value(7),
		"thing.color":      attribute.StringValue("COLOR_RED"),
		"thing.weight":     attribute.Float64Value(1.5),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k].Emit(), v.Emit())
		}
	}
	for k, v := range got {
		if strings.Contains(v.Emit(), secret) || strings.Contains(v.Emit(), "not declared") {
			t.Errorf("%s holds an undeclared or sensitive value", k)
		}
	}
	var rec map[string]any
	if err := json.Unmarshal(ts.log.Bytes(), &rec); err != nil {
		t.Fatalf("%s: %v", ts.log, err)
	}
	if rec["thing.kind"] != "widget" || rec["thing.tag_count"] != float64(3) || rec["thing.has_email"] != true {
		t.Errorf("log record = %v, want the request's attributes", rec)
	}
	if strings.Contains(ts.log.String(), secret) {
		t.Error("the log holds the sensitive value")
	}
	// Never on a metric: a request's values are too many.
	for _, p := range ts.histogram("rpc.server.call.duration") {
		for _, kv := range p.Attributes.ToSlice() {
			if strings.HasPrefix(string(kv.Key), "thing.") {
				t.Errorf("metric attribute %s", kv.Key)
			}
		}
	}
}

func TestFieldAttributesOfAnEmptyRequest(t *testing.T) {
	ts := newThingServer(t, nil)
	if err := ts.call(t, nil); err != nil {
		t.Fatal(err)
	}
	got := attrs(ts.only().Attributes())
	if v, ok := got["thing.has_email"]; !ok || v.AsBool() {
		t.Errorf("thing.has_email = %v, want false", v.Emit())
	}
	if _, ok := got["thing.inner_flag"]; ok {
		t.Error("an attribute inside an unset message was recorded")
	}
	if got["thing.tag_count"].AsInt64() != 0 {
		t.Errorf("thing.tag_count = %v", got["thing.tag_count"].Emit())
	}
}

func TestErrorClassificationOverridesTheCode(t *testing.T) {
	for _, tt := range []struct {
		reason  string
		code    connect.Code
		isError bool
	}{
		{reasonBusy, connect.CodeUnavailable, false},
		{reasonVanished, connect.CodeNotFound, true},
		{"", connect.CodeUnavailable, true},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			ts := newThingServer(t, fails(tt.code, tt.reason)(context.Background()))
			_ = ts.call(t, nil)
			span := ts.only()
			if (span.Status().Code == codes.Error) != tt.isError {
				t.Errorf("status = %v, want error %v", span.Status(), tt.isError)
			}
			_, hasType := attrs(span.Attributes())["error.type"]
			if hasType != tt.isError {
				t.Errorf("error.type set %v, want %v", hasType, tt.isError)
			}
			p := ts.histogram("rpc.server.call.duration")[0]
			if _, ok := p.Attributes.Value("error.type"); ok != tt.isError {
				t.Errorf("metric error.type set %v, want %v", ok, tt.isError)
			}
		})
	}
}

func TestServerRefusesAContractThatWouldLeakOrMislead(t *testing.T) {
	field := func(f *descriptorpb.FileDescriptorProto, name string) *descriptorpb.FieldDescriptorProto {
		for _, fd := range f.MessageType[0].Field {
			if fd.GetName() == name {
				return fd
			}
		}
		return nil
	}
	setRules := func(name string, rules *dedov1.FieldRules) func(*descriptorpb.FileDescriptorProto) {
		return func(f *descriptorpb.FileDescriptorProto) { field(f, name).Options = fieldOpts(rules) }
	}
	for name, edit := range map[string]func(*descriptorpb.FileDescriptorProto){
		"sensitive value": setRules("email", &dedov1.FieldRules{Sensitive: true, Attribute: "thing.email"}),
		"reserved key":    setRules("kind", &dedov1.FieldRules{Attribute: "rpc.kind"}),
		"bad key":         setRules("kind", &dedov1.FieldRules{Attribute: "Kind"}),
		"count of scalar": setRules("kind", &dedov1.FieldRules{Attribute: "thing.kind", AttributeMode: dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT}),
		"duplicate key":   setRules("note", &dedov1.FieldRules{Attribute: "thing.kind"}),
		"undeclared reason": func(f *descriptorpb.FileDescriptorProto) {
			opts := f.Service[0].Method[0].Options
			rules := proto.GetExtension(opts, dedov1.E_Method).(*dedov1.MethodRules)
			rules.ErrorClassifications = append(rules.ErrorClassifications, &dedov1.ErrorClassification{Reason: "NOT_DECLARED"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			table, _ := thingTable(t, edit)
			_, err := telemetry.NewServerInterceptor(telemetry.Config{Table: table})
			if err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// A table without descriptors, as a hand-built one is, has no field
// attributes but still classifies reasons.
func TestServerAcceptsATableWithoutDescriptors(t *testing.T) {
	table := policy.Table{findThing: {Procedure: findThing, Rules: &dedov1.MethodRules{}}}
	if _, err := telemetry.NewServerInterceptor(telemetry.Config{Table: table}); err != nil {
		t.Fatal(err)
	}
}
