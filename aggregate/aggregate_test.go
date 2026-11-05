package aggregate_test

import (
	"slices"
	"testing"

	_ "google.golang.org/genproto/googleapis/api/annotations" // registers google.api.resource
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/raveesh-me/dedo/aggregate"
)

// fixture has a root with a child (OrderService), two roots
// (CheckoutService) and a domain service over a recursive value object
// (AddressService).
const fixture = `
name: "shop.proto" package: "shop.v1" syntax: "proto3"
dependency: "google/api/resource.proto"
message_type {
  name: "Order"
  options { [google.api.resource] { type: "shop.example.com/Order" pattern: "orders/{order}" plural: "orders" } }
  field { name: "name" number: 1 type: TYPE_STRING json_name: "name" }
  field { name: "lines" number: 2 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".shop.v1.Line" json_name: "lines" }
}
message_type {
  name: "Line"
  options { [google.api.resource] { type: "shop.example.com/Line" pattern: "orders/{order}/lines/{line}" plural: "lines" } }
}
message_type {
  name: "Basket"
  options { [google.api.resource] { type: "shop.example.com/Basket" pattern: "baskets/{basket}" } }
}
message_type { name: "Req" }
message_type {
  name: "GetOrderResponse"
  field { name: "order" number: 1 type: TYPE_MESSAGE type_name: ".shop.v1.Order" json_name: "order" }
}
message_type {
  name: "ListLinesResponse"
  field { name: "lines" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".shop.v1.Line" json_name: "lines" }
}
message_type {
  name: "SearchOrdersResponse"
  field { name: "by_name" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".shop.v1.SearchOrdersResponse.ByNameEntry" json_name: "byName" }
  nested_type {
    name: "ByNameEntry"
    options { map_entry: true }
    field { name: "key" number: 1 type: TYPE_STRING json_name: "key" }
    field { name: "value" number: 2 type: TYPE_MESSAGE type_name: ".shop.v1.Order" json_name: "value" }
  }
}
message_type {
  name: "Part"
  field { name: "parts" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".shop.v1.Part" json_name: "parts" }
}
service {
  name: "OrderService"
  method { name: "GetOrder" input_type: ".shop.v1.Req" output_type: ".shop.v1.GetOrderResponse" }
  method { name: "ListLines" input_type: ".shop.v1.Req" output_type: ".shop.v1.ListLinesResponse" }
  method { name: "SearchOrders" input_type: ".shop.v1.Req" output_type: ".shop.v1.SearchOrdersResponse" }
}
service {
  name: "CheckoutService"
  method { name: "GetBasket" input_type: ".shop.v1.Req" output_type: ".shop.v1.Basket" }
  method { name: "GetOrder" input_type: ".shop.v1.Req" output_type: ".shop.v1.GetOrderResponse" }
}
service {
  name: "AddressService"
  method { name: "ResolveAddress" input_type: ".shop.v1.Req" output_type: ".shop.v1.Part" }
}
`

func services(t *testing.T) protoreflect.ServiceDescriptors {
	t.Helper()
	var fdp descriptorpb.FileDescriptorProto
	if err := prototext.Unmarshal([]byte(fixture), &fdp); err != nil {
		t.Fatal(err)
	}
	fd, err := protodesc.NewFile(&fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Services()
}

func types(rs []aggregate.Resource) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Type)
	}
	return out
}

// A service's resources are found at any depth, through response
// wrappers, maps and embedded resources, once each in the order a walk
// first meets them. Its roots are those not nested under another's pattern.
func TestResourcesAndRoots(t *testing.T) {
	svcs := services(t)
	for _, c := range []struct {
		service   protoreflect.Name
		resources []string
		roots     []string
	}{
		{"OrderService", []string{"shop.example.com/Order", "shop.example.com/Line"}, []string{"shop.example.com/Order"}},
		{"CheckoutService", []string{"shop.example.com/Basket", "shop.example.com/Order", "shop.example.com/Line"}, []string{"shop.example.com/Basket", "shop.example.com/Order"}},
		{"AddressService", []string{}, []string{}},
	} {
		all := aggregate.Resources(svcs.ByName(c.service))
		if got := types(all); !slices.Equal(got, c.resources) {
			t.Errorf("%s: resources = %v, want %v", c.service, got, c.resources)
		}
		if got := types(aggregate.Roots(all)); !slices.Equal(got, c.roots) {
			t.Errorf("%s: roots = %v, want %v", c.service, got, c.roots)
		}
	}
	order := aggregate.Resources(svcs.ByName("OrderService"))[0]
	if order.Plural != "orders" || !slices.Equal(order.Patterns, []string{"orders/{order}"}) {
		t.Errorf("Order = %+v, want its plural and patterns", order)
	}
}
