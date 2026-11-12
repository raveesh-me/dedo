// Package fieldattr checks the telemetry attributes a contract declares on
// request fields ((dedo.v1.field).attribute and attribute_mode). The lint
// plugin reports what it finds, and package telemetry refuses to start on
// it, so a contract that slipped past lint still never records a value it
// should not.
//
// It depends on no OpenTelemetry package, so the lint plugin can use it.
package fieldattr

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// keyFormat is a lowercase, dot-separated key of at least two segments,
// each snake_case: a namespace, then the name.
var keyFormat = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ReservedNamespaces are the first segments an attribute key may not have:
// the namespaces of OpenTelemetry's semantic conventions registry, and
// dedo's and OpenTelemetry's own. A key in one could collide with, or be
// mistaken for, a standard attribute.
var ReservedNamespaces = []string{
	"android", "app", "artifact", "aspnetcore", "aws", "az", "azure", "browser",
	"cassandra", "cicd", "client", "cloud", "cloudevents", "cloudfoundry", "code",
	"container", "cpu", "cpython", "db", "dedo", "deployment", "destination",
	"device", "disk", "dns", "dotnet", "elasticsearch", "enduser", "error",
	"event", "exception", "faas", "feature_flag", "file", "gcp", "gen_ai", "geo",
	"go", "graphql", "heroku", "host", "http", "hw", "ios", "jsonrpc", "jvm",
	"k8s", "kestrel", "linux", "log", "mainframe", "mcp", "message", "messaging",
	"net", "network", "nodejs", "oci", "openai", "openshift", "opentracing",
	"os", "otel", "peer", "pprof", "process", "profile", "rpc", "security_rule",
	"server", "service", "session", "signalr", "source", "system", "telemetry",
	"test", "thread", "tls", "url", "user", "user_agent", "v8js", "vcs",
	"webengine", "zos",
}

// KeyProblem says what is wrong with an attribute key, or "".
func KeyProblem(key string) string {
	if !keyFormat.MatchString(key) {
		return fmt.Sprintf("attribute %q must be lowercase snake_case segments joined by dots, in a namespace of the service's own, such as \"todo.priority\"", key)
	}
	namespace, _, _ := strings.Cut(key, ".")
	if slices.Contains(ReservedNamespaces, namespace) {
		return fmt.Sprintf("attribute %q is in the namespace %q, which OpenTelemetry's semantic conventions or dedo use; put it in the service's own, such as the proto package's", key, namespace)
	}
	return ""
}

// Mode is the mode an attribute records: VALUE when unspecified.
func Mode(rules *dedov1.FieldRules) dedov1.AttributeMode {
	if m := rules.GetAttributeMode(); m != dedov1.AttributeMode_ATTRIBUTE_MODE_UNSPECIFIED {
		return m
	}
	return dedov1.AttributeMode_ATTRIBUTE_MODE_VALUE
}

// SensitiveProblem says why a sensitive field's attribute would leak it,
// or "": a sensitive field may only record its presence.
func SensitiveProblem(rules *dedov1.FieldRules) string {
	if !rules.GetSensitive() || rules.GetAttribute() == "" {
		return ""
	}
	if Mode(rules) != dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE {
		return fmt.Sprintf("attribute %q would record a sensitive field's %s; a sensitive field's attribute may only be ATTRIBUTE_MODE_PRESENCE", rules.GetAttribute(), modeWhat(Mode(rules)))
	}
	return ""
}

// ModeProblem says why the attribute's mode does not fit its field, or "".
func ModeProblem(fd protoreflect.FieldDescriptor, rules *dedov1.FieldRules) string {
	if rules.GetAttribute() == "" {
		if rules.GetAttributeMode() != dedov1.AttributeMode_ATTRIBUTE_MODE_UNSPECIFIED {
			return "attribute_mode is set, but the field has no attribute to record"
		}
		return ""
	}
	many := fd.IsList() || fd.IsMap()
	switch Mode(rules) {
	case dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT:
		if !many {
			return fmt.Sprintf("attribute %q counts items, but the field is not repeated or a map", rules.GetAttribute())
		}
	case dedov1.AttributeMode_ATTRIBUTE_MODE_VALUE:
		switch {
		case many:
			return fmt.Sprintf("attribute %q records a value, but the field is repeated or a map: use ATTRIBUTE_MODE_ITEM_COUNT or ATTRIBUTE_MODE_PRESENCE", rules.GetAttribute())
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			return fmt.Sprintf("attribute %q records a value, but the field is a message: put the attribute on one of its fields, or use ATTRIBUTE_MODE_PRESENCE", rules.GetAttribute())
		case fd.Kind() == protoreflect.BytesKind:
			return fmt.Sprintf("attribute %q records a value, but the field is bytes: use ATTRIBUTE_MODE_PRESENCE", rules.GetAttribute())
		}
	}
	return ""
}

func modeWhat(m dedov1.AttributeMode) string {
	if m == dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT {
		return "item count"
	}
	return "value"
}
