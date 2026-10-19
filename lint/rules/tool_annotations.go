package rules

import (
	"context"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var toolAnnotations = lintRule(
	idToolAnnotations,
	"Checks that every unary RPC declares the MCP ToolAnnotations hints, consistent with its idempotency_level.",
	checkutil.NewMethodRuleHandler(checkToolAnnotations, checkutil.WithoutImports()),
)

// checkToolAnnotations enforces the hints of ToolAnnotations in the MCP
// specification (2026-07-28), and only those. Streaming RPCs are not tools.
func checkToolAnnotations(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	if m.IsStreamingClient() || m.IsStreamingServer() {
		return nil
	}
	report := func(format string, args ...any) {
		w.AddAnnotation(check.WithDescriptor(m), messagef(idToolAnnotations, format, args...))
	}
	tool := methodRules(m).GetTool()
	if tool.GetDestructive() {
		report("RPC %q sets the deprecated tool.destructive; set tool.annotations.destructive_hint instead.", m.Name())
	}
	a := tool.GetAnnotations()
	if a == nil {
		report("RPC %q declares no tool.annotations; declare read_only_hint and open_world_hint, and for writes destructive_hint and idempotent_hint.", m.Name())
		return nil
	}
	if a.OpenWorldHint == nil {
		report("RPC %q declares no open_world_hint. The MCP default is true: set false if the tool only touches this service's own data.", m.Name())
	}
	level := idempotency(m)
	readOnly := level == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
	switch {
	case a.ReadOnlyHint == nil:
		report("RPC %q declares no read_only_hint; it must be %t to match idempotency_level %s.", m.Name(), readOnly, level)
	case a.GetReadOnlyHint() != readOnly:
		report("RPC %q read_only_hint is %t, but idempotency_level %s means %t.", m.Name(), a.GetReadOnlyHint(), level, readOnly)
	}
	if readOnly {
		if a.DestructiveHint != nil || a.IdempotentHint != nil {
			report("RPC %q is read-only, so destructive_hint and idempotent_hint mean nothing in MCP; remove them.", m.Name())
		}
		return nil
	}
	if a.DestructiveHint == nil {
		report("RPC %q declares no destructive_hint. The MCP default is true: set false only if the tool never overwrites or deletes.", m.Name())
	} else if hasVerb(m, "Delete", "Remove") && !a.GetDestructiveHint() {
		report("RPC %q deletes data, so destructive_hint must be true.", m.Name())
	}
	idempotent := level == descriptorpb.MethodOptions_IDEMPOTENT
	switch {
	case a.IdempotentHint == nil:
		report("RPC %q declares no idempotent_hint; it must be %t to match idempotency_level %s.", m.Name(), idempotent, level)
	case a.GetIdempotentHint() != idempotent:
		report("RPC %q idempotent_hint is %t, but idempotency_level %s means %t.", m.Name(), a.GetIdempotentHint(), level, idempotent)
	}
	return nil
}
