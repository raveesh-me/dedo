package gorules

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

var update = flag.Bool("update", false, "rewrite the generated testdata packages")

// The generated packages in testdata stand in for protoc-gen-go's output:
// Go types named after the messages, and the file's descriptor in a
// file_*_rawDesc constant (a []byte variable for the older generator), as
// the analyzers read it. go test -run TestGeneratedTestdata -update rewrites
// them from the descriptors below.
func TestGeneratedTestdata(t *testing.T) {
	for _, g := range []struct {
		path  string
		file  *descriptorpb.FileDescriptorProto
		bytes bool
	}{
		{"testdata/src/example.com/svc/gen/svcv1/svc.pb.go", svcFile(), false},
		{"testdata/src/example.com/svc/gen/oldv1/old.pb.go", oldFile(), true},
	} {
		want, err := generated(g.file, g.bytes)
		if err != nil {
			t.Fatal(err)
		}
		if *update {
			if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(g.path, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(g.path)
		if err != nil || string(got) != want {
			t.Errorf("%s is stale: run go test -run TestGeneratedTestdata -update (%v)", g.path, err)
		}
	}
}

func msg(name string, nested ...*descriptorpb.DescriptorProto) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{Name: proto.String(name), NestedType: nested}
}

func method(name, in, out string, rules *dedov1.MethodRules) *descriptorpb.MethodDescriptorProto {
	opts := &descriptorpb.MethodOptions{}
	if rules != nil {
		proto.SetExtension(opts, dedov1.E_Method, rules)
	}
	return &descriptorpb.MethodDescriptorProto{Name: proto.String(name), InputType: proto.String(in), OutputType: proto.String(out), Options: opts}
}

func reasons(names ...string) []*dedov1.ErrorReason {
	var out []*dedov1.ErrorReason
	for _, n := range names {
		out = append(out, &dedov1.ErrorReason{Reason: n})
	}
	return out
}

func dedoService(name string, methods ...*descriptorpb.MethodDescriptorProto) *descriptorpb.ServiceDescriptorProto {
	opts := &descriptorpb.ServiceOptions{}
	proto.SetExtension(opts, dedov1.E_Service, &dedov1.ServiceRules{Audience: dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL, OwnerTeam: "orders"})
	return &descriptorpb.ServiceDescriptorProto{Name: proto.String(name), Method: methods, Options: opts}
}

// svcFile is example.svc.v1: a dedo service, OrderService, and a service
// that is not one, LegacyService.
func svcFile() *descriptorpb.FileDescriptorProto {
	const p = ".example.svc.v1."
	watch := method("WatchOrders", p+"GetOrderRequest", p+"GetOrderResponse", &dedov1.MethodRules{Errors: reasons("ORDER_NOT_FOUND")})
	watch.ServerStreaming = proto.Bool(true)
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("example/svc/v1/svc.proto"),
		Package: proto.String("example.svc.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			msg("Order", msg("Line")),
			msg("GetOrderRequest"), msg("GetOrderResponse"),
			msg("CreateOrderRequest"), msg("CreateOrderResponse"),
			msg("ShipOrderRequest"), msg("ShipOrderResponse"),
			msg("GetLegacyRequest"), msg("GetLegacyResponse"),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			dedoService("OrderService",
				method("GetOrder", p+"GetOrderRequest", p+"GetOrderResponse", &dedov1.MethodRules{Errors: reasons("ORDER_NOT_FOUND")}),
				method("CreateOrder", p+"CreateOrderRequest", p+"CreateOrderResponse", &dedov1.MethodRules{
					Errors: reasons("ORDER_EXISTS", "EVENT_NOT_PUBLISHED"),
					Emits:  []string{"example.svc.order.created"},
				}),
				method("ShipOrder", p+"ShipOrderRequest", p+"ShipOrderResponse", nil),
				watch,
			),
			// A second dedo service with an RPC of the same name and messages:
			// a method serving GetOrder may serve either.
			dedoService("ArchiveService",
				method("GetOrder", p+"GetOrderRequest", p+"GetOrderResponse", &dedov1.MethodRules{
					Errors: reasons("ORDER_ARCHIVED"),
					Emits:  []string{"example.svc.order.restored"},
				}),
			),
			{Name: proto.String("LegacyService"), Method: []*descriptorpb.MethodDescriptorProto{
				method("GetLegacy", p+"GetLegacyRequest", p+"GetLegacyResponse", nil),
			}},
		},
	}
}

// oldFile is example.old.v1, generated as protoc-gen-go did before v1.36.1.
func oldFile() *descriptorpb.FileDescriptorProto {
	const p = ".example.old.v1."
	return &descriptorpb.FileDescriptorProto{
		Name:        proto.String("example/old/v1/old.proto"),
		Package:     proto.String("example.old.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{msg("PingRequest"), msg("PingResponse")},
		Service: []*descriptorpb.ServiceDescriptorProto{
			dedoService("PingService", method("Ping", p+"PingRequest", p+"PingResponse", &dedov1.MethodRules{Errors: reasons("PING_FAILED")})),
		},
	}
}

// generated renders a stand-in for protoc-gen-go's output.
func generated(fd *descriptorpb.FileDescriptorProto, asBytes bool) (string, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(fd)
	if err != nil {
		return "", err
	}
	pkg := strings.ReplaceAll(strings.TrimPrefix(fd.GetPackage(), "example."), ".", "")
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gorules' TestGeneratedTestdata. DO NOT EDIT.\n\npackage %s\n\n", pkg)
	var types func(prefix string, msgs []*descriptorpb.DescriptorProto)
	types = func(prefix string, msgs []*descriptorpb.DescriptorProto) {
		for _, m := range msgs {
			fmt.Fprintf(&b, "type %s%s struct{}\n\n", prefix, m.GetName())
			types(prefix+m.GetName()+"_", m.GetNestedType())
		}
	}
	types("", fd.GetMessageType())
	name := "file_" + strings.NewReplacer("/", "_", ".", "_").Replace(fd.GetName()) + "_rawDesc"
	if asBytes {
		fmt.Fprintf(&b, "var %s = []byte{", name)
		for i, c := range raw {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%#02x", c)
		}
		b.WriteString("}\n")
	} else {
		fmt.Fprintf(&b, "const %s = %q\n", name, raw)
	}
	return b.String(), nil
}
