package rules

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"

	hplv1 "github.com/raveesh-me/http-proto-lint/gen/go/raveesh_me/http_proto_lint/v1"
)

var toolRequired = lintRule(
	"HOUSE_TOOL_REQUIRED",
	"Checks that every unary RPC declares a tool title and description, and that Delete and Remove RPCs are marked destructive.",
	checkutil.NewMethodRuleHandler(checkTool, checkutil.WithoutImports()),
)

var messageDescriptionRequired = lintRule(
	"HOUSE_MESSAGE_DESCRIPTION_REQUIRED",
	"Checks that every message declares raveesh_me.http_proto_lint.v1.message with a description.",
	checkutil.NewMessageRuleHandler(checkMessageDescription, checkutil.WithoutImports()),
)

// checkTool skips streaming RPCs, which MCP cannot expose as tools.
func checkTool(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	if m.IsStreamingClient() || m.IsStreamingServer() {
		return nil
	}
	tool := methodRules(m).GetTool()
	title := strings.TrimSpace(tool.GetTitle())
	description := strings.TrimSpace(tool.GetDescription())
	switch {
	case tool == nil:
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q declares no tool; set tool.title and tool.description in raveesh_me.http_proto_lint.v1.method.", m.Name()))
	case title == "":
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q declares no tool title.", m.Name()))
	case description == "":
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q declares no tool description.", m.Name()))
	case strings.EqualFold(description, title) || strings.EqualFold(description, string(m.Name())):
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q tool description repeats its name; say what it does and when to use it.", m.Name()))
	}
	if hasVerb(m, "Delete", "Remove") && tool != nil && !tool.GetDestructive() {
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q deletes data, so its tool must set destructive: true.", m.Name()))
	}
	return nil
}

func checkMessageDescription(_ context.Context, w check.ResponseWriter, _ check.Request, msg protoreflect.MessageDescriptor) error {
	if msg.IsMapEntry() {
		return nil
	}
	if strings.TrimSpace(messageRules(msg).GetDescription()) == "" {
		w.AddAnnotation(check.WithDescriptor(msg),
			check.WithMessagef("Message %q declares no raveesh_me.http_proto_lint.v1.message description.", msg.Name()))
	}
	return nil
}

// messageRules reads the raveesh_me.http_proto_lint.v1.message option, or returns nil.
func messageRules(msg protoreflect.MessageDescriptor) *hplv1.MessageRules {
	return extension[*hplv1.MessageRules](msg.Options(), hplv1.E_Message)
}
