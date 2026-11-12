package rules

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/raveesh-me/dedo/telemetry/objectives"
)

var serviceLevel = lintRule(
	idServiceLevel,
	"Checks that a service's level objectives are sane: shares between their bounds, latency thresholds on the duration metric's bucket boundaries, and a window of whole days.",
	checkutil.NewServiceRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, s protoreflect.ServiceDescriptor) error {
		if problems := objectives.ServiceProblems(serviceRules(s).GetServiceLevel()); len(problems) > 0 {
			w.AddAnnotation(check.WithDescriptor(s),
				messagef(idServiceLevel, "Service %q: %s.", s.Name(), strings.Join(problems, "; ")))
		}
		return nil
	}, checkutil.WithoutImports()),
)

var methodServiceLevel = lintRule(
	idMethodServiceLevel,
	"Checks that an RPC's own level objectives are sane, measured over its service's window, with no latency on a stream, and that an exempt RPC's service declares objectives.",
	checkutil.NewMethodRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
		level := methodRules(m).GetServiceLevel()
		if level == nil {
			return nil
		}
		service := serviceRules(m.Parent().(protoreflect.ServiceDescriptor)).GetServiceLevel()
		streaming := m.IsStreamingClient() || m.IsStreamingServer()
		if problems := objectives.MethodProblems(level, service, streaming); len(problems) > 0 {
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idMethodServiceLevel, "RPC %q: %s.", m.Name(), strings.Join(problems, "; ")))
		}
		return nil
	}, checkutil.WithoutImports()),
)
