package main

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"gopkg.in/yaml.v3"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// sloService builds acme.orders.v1.OrderService: GetOrder and ListOrders
// count toward the service's objectives, WatchOrders streams, ExportOrders
// is exempt and CancelOrder has objectives of its own.
func sloService(t *testing.T, svcLevel *dedov1.ServiceLevel, edit func(*descriptorpb.ServiceDescriptorProto)) protoreflect.ServiceDescriptor {
	t.Helper()
	svcOpts := &descriptorpb.ServiceOptions{}
	proto.SetExtension(svcOpts, dedov1.E_Service, &dedov1.ServiceRules{
		Audience: dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC, OwnerTeam: "Orders Team", ServiceLevel: svcLevel,
	})
	method := func(name string, level *dedov1.MethodServiceLevel, stream bool) *descriptorpb.MethodDescriptorProto {
		opts := &descriptorpb.MethodOptions{}
		proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{Scopes: []string{"orders.order.read"}, ServiceLevel: level})
		return &descriptorpb.MethodDescriptorProto{
			Name: proto.String(name), InputType: proto.String(".acme.orders.v1.Req"), OutputType: proto.String(".acme.orders.v1.Req"),
			ServerStreaming: proto.Bool(stream), Options: opts,
		}
	}
	exempt := &dedov1.MethodServiceLevel{Level: &dedov1.MethodServiceLevel_Exempt{Exempt: true}}
	own := &dedov1.MethodServiceLevel{Level: &dedov1.MethodServiceLevel_Objectives{Objectives: &dedov1.Objectives{Availability: 0.99}}}
	svc := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("OrderService"), Options: svcOpts,
		Method: []*descriptorpb.MethodDescriptorProto{
			method("GetOrder", nil, false),
			method("ListOrders", nil, false),
			method("WatchOrders", nil, true),
			method("ExportOrders", exempt, false),
			method("CancelOrder", own, false),
		},
	}
	if edit != nil {
		edit(svc)
	}
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("acme/orders/v1/orders.proto"), Package: proto.String("acme.orders.v1"), Syntax: proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Req")}},
		Service:     []*descriptorpb.ServiceDescriptorProto{svc},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Services().Get(0)
}

func standardLevel() *dedov1.ServiceLevel {
	return &dedov1.ServiceLevel{
		Objectives: &dedov1.Objectives{
			Availability: 0.999,
			Latency:      []*dedov1.LatencyObjective{{Percentile: 0.99, Threshold: durationpb.New(250_000_000)}},
		},
		Window: durationpb.New(28 * 24 * 3600 * 1_000_000_000),
	}
}

// docs decodes the YAML documents, by kind and name.
func sloDocs(t *testing.T, b []byte) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		if doc["apiVersion"] != "openslo/v1" {
			t.Errorf("apiVersion = %v", doc["apiVersion"])
		}
		meta := doc["metadata"].(map[string]any)
		out[doc["kind"].(string)+"/"+meta["name"].(string)] = doc
	}
}

func dig(doc any, path ...string) any {
	for _, p := range path {
		m, ok := doc.(map[string]any)
		if !ok {
			return nil
		}
		doc = m[p]
	}
	return doc
}

var rfc1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestOpenSLODocuments(t *testing.T) {
	b, err := openSLO(sloService(t, standardLevel(), nil), sloOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := sloDocs(t, b)
	const svc = "acme-orders-v1-orderservice"
	for _, key := range []string{
		"Service/" + svc,
		"AlertPolicy/" + svc + "-fast-burn", "AlertPolicy/" + svc + "-slow-burn",
		"SLI/" + svc + "-availability", "SLO/" + svc + "-availability",
		"SLI/" + svc + "-latency-p99", "SLO/" + svc + "-latency-p99",
		"SLI/" + svc + "-cancelorder-availability", "SLO/" + svc + "-cancelorder-availability",
	} {
		if got[key] == nil {
			t.Errorf("no %s among %v", key, keys(got))
		}
	}
	if len(got) != 9 {
		t.Errorf("got %d documents, want 9: %v", len(got), keys(got))
	}
	for key, doc := range got {
		name := dig(doc, "metadata", "name").(string)
		if len(name) > 63 || !rfc1123.MatchString(name) {
			t.Errorf("%s: name %q is not an RFC 1123 label", key, name)
		}
		if dn, _ := dig(doc, "metadata", "displayName").(string); len(dn) > 63 {
			t.Errorf("%s: displayName longer than 63", key)
		}
		if dig(doc, "metadata", "labels", "dedo.service") != "acme.orders.v1.OrderService" {
			t.Errorf("%s: labels = %v", key, dig(doc, "metadata", "labels"))
		}
	}

	slo := got["SLO/"+svc+"-availability"]
	if dig(slo, "spec", "service") != svc || dig(slo, "spec", "budgetingMethod") != "Occurrences" || dig(slo, "spec", "indicatorRef") != svc+"-availability" {
		t.Errorf("availability SLO spec = %v", dig(slo, "spec"))
	}
	window := dig(slo, "spec", "timeWindow").([]any)[0]
	if dig(window, "duration") != "28d" || dig(window, "isRolling") != true {
		t.Errorf("timeWindow = %v", window)
	}
	objective := dig(slo, "spec", "objectives").([]any)[0]
	if dig(objective, "target") != 0.999 {
		t.Errorf("objective = %v", objective)
	}
	policies := dig(slo, "spec", "alertPolicies").([]any)
	if len(policies) != 2 || dig(policies[0], "alertPolicyRef") != svc+"-fast-burn" {
		t.Errorf("alertPolicies = %v", policies)
	}

	// The service's SLIs count the RPCs that are not exempt and have no
	// objectives of their own; latency leaves the stream out too.
	sli := got["SLI/"+svc+"-availability"]
	good := dig(sli, "spec", "ratioMetric", "good", "metricSource")
	total := dig(sli, "spec", "ratioMetric", "total", "metricSource")
	if dig(sli, "spec", "ratioMetric", "counter") != true || dig(good, "type") != "OpenTelemetry" {
		t.Errorf("availability SLI = %v", dig(sli, "spec"))
	}
	if dig(good, "spec", "metric") != "rpc.server.call.duration" {
		t.Errorf("metric = %v", dig(good, "spec", "metric"))
	}
	wantAll := []any{"acme.orders.v1.OrderService/GetOrder", "acme.orders.v1.OrderService/ListOrders", "acme.orders.v1.OrderService/WatchOrders"}
	if got := dig(good, "spec", "attributes", "rpc.method"); !equalAny(got, wantAll) {
		t.Errorf("good rpc.method = %v, want %v", got, wantAll)
	}
	if got := dig(good, "spec", "absentAttributes"); !equalAny(got, []any{"error.type"}) {
		t.Errorf("good absentAttributes = %v", got)
	}
	if dig(total, "spec", "absentAttributes") != nil {
		t.Error("the total counts every call")
	}
	lat := dig(got["SLI/"+svc+"-latency-p99"], "spec", "ratioMetric", "good", "metricSource", "spec")
	if !equalAny(dig(lat, "attributes", "rpc.method"), wantAll[:2]) || dig(lat, "maxValueSeconds") != 0.25 {
		t.Errorf("latency good = %v", lat)
	}
	if dig(got["SLO/"+svc+"-latency-p99"], "spec", "objectives").([]any)[0].(map[string]any)["target"] != 0.99 {
		t.Error("latency target is not the percentile")
	}

	own := dig(got["SLI/"+svc+"-cancelorder-availability"], "spec", "ratioMetric", "good", "metricSource", "spec", "attributes", "rpc.method")
	if !equalAny(own, []any{"acme.orders.v1.OrderService/CancelOrder"}) {
		t.Errorf("CancelOrder's own SLI selects %v", own)
	}
	if dig(got["SLO/"+svc+"-cancelorder-availability"], "metadata", "labels", "dedo.rpc.method") != "acme.orders.v1.OrderService/CancelOrder" {
		t.Error("an RPC's own SLO is not labelled with its method")
	}

	fast := got["AlertPolicy/"+svc+"-fast-burn"]
	target := dig(fast, "spec", "notificationTargets").([]any)[0]
	if dig(target, "targetRef") != "orders-team" {
		t.Errorf("notification target = %v, want the owner team", target)
	}
	cond := dig(fast, "spec", "conditions").([]any)[0]
	if dig(cond, "kind") != "AlertCondition" || dig(cond, "spec", "condition", "kind") != "burnrate" || dig(cond, "spec", "severity") != "page" {
		t.Errorf("fast-burn condition = %v", cond)
	}
}

func TestOpenSLOMetricSourceRef(t *testing.T) {
	b, err := openSLO(sloService(t, standardLevel(), nil), sloOptions{DataSource: "otel-metrics"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "metricSourceRef: otel-metrics") || strings.Contains(string(b), "type: OpenTelemetry") {
		t.Errorf("the data source is not referenced:\n%s", b)
	}
}

func TestOpenSLONothingDeclared(t *testing.T) {
	svc := sloService(t, nil, func(s *descriptorpb.ServiceDescriptorProto) {
		s.Method = s.Method[:3]
	})
	b, err := openSLO(svc, sloOptions{})
	if err != nil || b != nil {
		t.Errorf("a service without objectives wrote %q, %v", b, err)
	}
}

func TestOpenSLORefusesInsaneValues(t *testing.T) {
	level := standardLevel()
	level.Objectives.Availability = 99.9
	if _, err := openSLO(sloService(t, level, nil), sloOptions{}); err == nil {
		t.Error("want an error for availability 99.9")
	}
	if _, err := openSLO(sloService(t, nil, nil), sloOptions{}); err == nil {
		t.Error("want an error for an exempt RPC of a service without objectives")
	}
}

func TestSLOName(t *testing.T) {
	long := strings.Repeat("a", 80)
	for in, want := range map[string]string{
		"acme.orders.v1.OrderService": "acme-orders-v1-orderservice",
		"Orders Team":                 "orders-team",
		"--x__y--":                    "x-y",
	} {
		if got := sloName(in); got != want {
			t.Errorf("sloName(%q) = %q, want %q", in, got, want)
		}
	}
	got := sloName(long)
	if len(got) > 63 || !rfc1123.MatchString(got) || got == sloName(long+"b") {
		t.Errorf("sloName of a long name = %q", got)
	}
}

func keys(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func equalAny(got any, want []any) bool {
	list, ok := got.([]any)
	if !ok || len(list) != len(want) {
		return false
	}
	for i := range list {
		if list[i] != want[i] {
			return false
		}
	}
	return true
}

func TestPercentileName(t *testing.T) {
	for p, want := range map[float64]string{0.5: "p50", 0.99: "p99", 0.999: "p99-9", 0.9999: "p99-99", 0.95: "p95"} {
		if got := percentileName(p); got != want {
			t.Errorf("percentileName(%v) = %q, want %q", p, got, want)
		}
	}
}
