package rules

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"buf.build/go/bufplugin/descriptor"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// grpcHealthProto is gRPC's health checking protocol, which a service
// imports to declare grpc.health.v1.Health in its own contract.
const grpcHealthProto = "grpc/health/v1/health.proto"

// importFieldNumber is FileDescriptorProto's dependency field, where an
// import's annotation points.
const importFieldNumber = 3

// healthMessage ends every DEDO_NO_HEALTH_RPC annotation: what to do
// instead.
const healthMessage = "Health checks aren't part of the API: the server already serves them (GET /healthz). Remove it from the proto."

// healthNames are the names of a health check, as a service (without its
// Service suffix), an RPC, or a message (without a Request or Response
// suffix).
var healthNames = map[string]bool{
	"Health":      true,
	"HealthCheck": true,
	"Healthz":     true,
	"Ping":        true,
	"Liveness":    true,
	"Readiness":   true,
}

// healthServiceMethods are the RPCs of gRPC's health service, flagged on a
// service that is itself a health check.
var healthServiceMethods = map[string]bool{"Check": true, "Watch": true, "List": true}

var noHealthRPC = lintRule(
	idNoHealthRPC,
	"Checks that no service, RPC or message is a health check and no file imports grpc/health/v1/health.proto: the server serves health itself, at GET /healthz.",
	checkutil.NewFileRuleHandler(checkNoHealth, checkutil.WithoutImports()),
)

func checkNoHealth(_ context.Context, w check.ResponseWriter, _ check.Request, fd descriptor.FileDescriptor) error {
	f := fd.ProtoreflectFileDescriptor()
	imports := f.Imports()
	for i := range imports.Len() {
		if imports.Get(i).Path() == grpcHealthProto {
			w.AddAnnotation(check.WithFileNameAndSourcePath(f.Path(), protoreflect.SourcePath{importFieldNumber, int32(i)}),
				messagef(idNoHealthRPC, "%q imports gRPC's health service. %s", f.Path(), healthMessage))
		}
	}
	services := f.Services()
	for i := range services.Len() {
		checkHealthService(w, services.Get(i))
	}
	checkHealthMessages(w, f.Messages())
	return nil
}

func checkHealthService(w check.ResponseWriter, s protoreflect.ServiceDescriptor) {
	isHealth := healthNames[strings.TrimSuffix(string(s.Name()), "Service")]
	if isHealth {
		w.AddAnnotation(check.WithDescriptor(s),
			messagef(idNoHealthRPC, "Service %q is a health check. %s", s.Name(), healthMessage))
	}
	methods := s.Methods()
	for i := range methods.Len() {
		m := methods.Get(i)
		if healthNames[string(m.Name())] || (isHealth && healthServiceMethods[string(m.Name())]) {
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idNoHealthRPC, "RPC %q is a health check. %s", m.FullName(), healthMessage))
		}
	}
}

func checkHealthMessages(w check.ResponseWriter, messages protoreflect.MessageDescriptors) {
	for i := range messages.Len() {
		m := messages.Get(i)
		if m.IsMapEntry() {
			continue
		}
		if isHealthMessage(string(m.Name())) {
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idNoHealthRPC, "Message %q is a health check. %s", m.FullName(), healthMessage))
		}
		checkHealthMessages(w, m.Messages())
	}
}

// isHealthMessage reports whether a message name is a health check's:
// a health name with an optional Request or Response suffix, such as
// HealthCheckResponse or PingRequest, or a part of one, such as
// HealthCheckComponent. A domain's own Health* messages, such as
// HealthRecord, pass.
func isHealthMessage(name string) bool {
	if strings.HasPrefix(name, "HealthCheck") {
		return true
	}
	for _, suffix := range []string{"Request", "Response"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return healthNames[name]
}
