package restdoc

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

const (
	bearerScheme = "bearer"
	statusSchema = "Status"
)

// binding is one HTTP verb and path of an RPC.
type binding struct {
	method string
	path   string
	rule   *annotations.HttpRule
}

// bindings lists the main HTTP rule of a method and its additional bindings.
func bindings(rule *annotations.HttpRule) []binding {
	if rule == nil {
		return nil
	}
	var out []binding
	if method, path := verbAndPath(rule); path != "" {
		out = append(out, binding{method: method, path: path, rule: rule})
	}
	for _, extra := range rule.GetAdditionalBindings() {
		out = append(out, bindings(extra)...)
	}
	return out
}

func verbAndPath(rule *annotations.HttpRule) (string, string) {
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return http.MethodGet, p.Get
	case *annotations.HttpRule_Put:
		return http.MethodPut, p.Put
	case *annotations.HttpRule_Post:
		return http.MethodPost, p.Post
	case *annotations.HttpRule_Delete:
		return http.MethodDelete, p.Delete
	case *annotations.HttpRule_Patch:
		return http.MethodPatch, p.Patch
	}
	// Custom verbs have no OpenAPI operation.
	return "", ""
}

// operation documents one binding of a method.
func (g *generator) operation(svc *protogen.Service, m *protogen.Method, b binding) (string, *Operation, error) {
	tmpl, err := parseTemplate(b.path)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", m.Desc.FullName(), err)
	}
	path, params := tmpl.openAPIPath(func(seg segment) []string { return g.paramNames(m.Input, seg) })
	rules, _ := proto.GetExtension(m.Desc.Options(), dedov1.E_Method).(*dedov1.MethodRules)

	op := &Operation{
		OperationID: fmt.Sprintf("%s_%s", svc.Desc.Name(), m.Desc.Name()),
		Summary:     rules.GetTool().GetTitle(),
		Description: operationDescription(m, rules),
		Tags:        []string{serviceTag(svc)},
		Procedure:   fmt.Sprintf("/%s/%s", svc.Desc.FullName(), m.Desc.Name()),
		Destructive: rules.GetTool().GetAnnotations().GetDestructiveHint(),
		Responses:   map[string]*Response{},
	}
	if op.Summary == "" {
		op.Summary = string(m.Desc.Name())
	}
	if scopes := rules.GetScopes(); len(scopes) > 0 {
		op.Security = []map[string][]string{{bearerScheme: scopes}}
	}
	op.Parameters = g.parameters(m, params, b.rule.GetBody())
	op.RequestBody = g.requestBody(m, params, b.rule.GetBody())
	op.Responses["200"] = g.success(m, b, rules)
	g.errorResponses(op, rules)
	if dep := rules.GetDeprecation(); dep != nil {
		op.Deprecated = true
		if t := dep.GetSunsetTime(); t != nil {
			op.Sunset = t.AsTime().UTC().Format(time.DateOnly)
		}
		op.Successor = dep.GetSuccessor()
	} else if opts, _ := m.Desc.Options().(*descriptorpb.MethodOptions); opts.GetDeprecated() {
		op.Deprecated = true
	}
	return path, op, nil
}

// operationDescription prefers the tool description and adds the scopes,
// which some renderers do not show from the security requirement.
func operationDescription(m *protogen.Method, rules *dedov1.MethodRules) string {
	text := strings.TrimSpace(rules.GetTool().GetDescription())
	if text == "" {
		text = cleanComment(m.Comments.Leading)
	}
	if scopes := rules.GetScopes(); len(scopes) > 0 {
		text += fmt.Sprintf("\n\nRequires one of the token scopes: `%s`.", strings.Join(scopes, "`, `"))
	}
	return strings.TrimSpace(text)
}

// paramNames names a variable's wildcards after the resource pattern it
// matches, such as {todo} for todos/{todo}.
func (g *generator) paramNames(input *protogen.Message, seg segment) []string {
	want := strings.Join(literals(seg.pattern), "/")
	for _, pattern := range g.resourcePatterns(input, seg.field) {
		parts := strings.Split(pattern, "/")
		if strings.Join(literals(parts), "/") != want || len(parts) != len(seg.pattern) {
			continue
		}
		var names []string
		for _, p := range parts {
			if strings.HasPrefix(p, "{") {
				names = append(names, strings.Trim(p, "{}"))
			}
		}
		return names
	}
	return nil
}

// resourcePatterns returns the patterns of the resource a field names: the
// type of its resource_reference, or the resource whose name field it is.
func (g *generator) resourcePatterns(input *protogen.Message, fieldPath string) []string {
	parts := strings.Split(fieldPath, ".")
	msg := input.Desc
	var fd protoreflect.FieldDescriptor
	for i, p := range parts {
		fd = msg.Fields().ByName(protoreflect.Name(p))
		if fd == nil {
			return nil
		}
		if i < len(parts)-1 {
			if fd.Message() == nil {
				return nil
			}
			msg = fd.Message()
		}
	}
	if ref, _ := proto.GetExtension(fd.Options(), annotations.E_ResourceReference).(*annotations.ResourceReference); ref.GetType() != "" {
		return g.resources[ref.GetType()]
	}
	if res, _ := proto.GetExtension(msg.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor); res != nil && fd.Name() == "name" {
		return res.GetPattern()
	}
	return nil
}

func (g *generator) parameters(m *protogen.Method, params []pathParam, body string) []Parameter {
	var out []Parameter
	bound := map[string]bool{}
	for _, p := range params {
		out = append(out, Parameter{
			Name:        p.name,
			In:          "path",
			Required:    true,
			Description: fmt.Sprintf("Part of `%s`, which has the form `%s`.", p.field, p.full),
			Schema:      &Schema{Type: "string"},
		})
		bound[strings.Split(p.field, ".")[0]] = true
	}
	if body == "*" {
		return out
	}
	for _, f := range m.Input.Fields {
		name := string(f.Desc.Name())
		if bound[name] || name == body || !isQueryable(f.Desc) {
			continue
		}
		out = append(out, Parameter{
			Name:        f.Desc.JSONName(),
			In:          "query",
			Description: cleanComment(f.Comments.Leading),
			Required:    hasBehavior(f.Desc, annotations.FieldBehavior_REQUIRED),
			Schema:      g.schemas.field(f.Desc),
		})
	}
	return out
}

func (g *generator) requestBody(m *protogen.Method, params []pathParam, body string) *RequestBody {
	switch body {
	case "":
		return nil
	case "*":
		skip := map[string]bool{}
		for _, p := range params {
			if !strings.Contains(p.field, ".") {
				skip[p.field] = true
			}
		}
		if len(skip) > 0 {
			return &RequestBody{Required: true, Content: jsonContent(g.schemas.object(m.Input, skip))}
		}
		return &RequestBody{Required: true, Content: jsonContent(g.schemas.ref(m.Input.Desc))}
	}
	fd := m.Input.Desc.Fields().ByName(protoreflect.Name(body))
	if fd == nil {
		return nil
	}
	return &RequestBody{
		Required: hasBehavior(fd, annotations.FieldBehavior_REQUIRED),
		Content:  jsonContent(g.schemas.field(fd)),
	}
}

// success documents the 200 response: the response_body field when set, so
// REST callers see the bare resource, and the headers the runtime adds.
func (g *generator) success(m *protogen.Method, b binding, rules *dedov1.MethodRules) *Response {
	var schema *Schema
	if fd := m.Output.Desc.Fields().ByName(protoreflect.Name(b.rule.GetResponseBody())); fd != nil {
		schema = g.schemas.field(fd)
	} else {
		schema = g.schemas.ref(m.Output.Desc)
	}
	headers := map[string]*Header{
		"X-Flow-ID": {Description: "Correlates the call across services. Send one to set it.", Schema: &Schema{Type: "string"}},
	}
	if c := rules.GetCache(); c != nil && b.method == http.MethodGet {
		headers["Cache-Control"] = &Header{Description: "How long the response may be cached.", Schema: &Schema{Type: "string"}}
	}
	if rules.GetDeprecation() != nil {
		headers["Deprecation"] = &Header{Description: "When the operation was deprecated (RFC 9745).", Schema: &Schema{Type: "string"}}
		headers["Sunset"] = &Header{Description: "When the operation stops working (RFC 8594).", Schema: &Schema{Type: "string"}}
	}
	return &Response{Description: "OK", Headers: headers, Content: jsonContent(schema)}
}

// errorResponses documents each declared error reason under its HTTP status,
// and the scope check's 403.
func (g *generator) errorResponses(op *Operation, rules *dedov1.MethodRules) {
	byStatus := map[int][]string{}
	for _, e := range rules.GetErrors() {
		status := httpStatus(e.GetCode())
		byStatus[status] = append(byStatus[status], e.GetReason())
		op.ErrorReasons = append(op.ErrorReasons, e.GetReason())
	}
	if len(rules.GetScopes()) > 0 {
		byStatus[http.StatusForbidden] = append(byStatus[http.StatusForbidden], "")
	}
	for status, reasons := range byStatus {
		op.Responses[fmt.Sprint(status)] = &Response{
			Description: errorDescription(status, reasons),
			Content:     jsonContent(&Schema{Ref: "#/components/schemas/" + statusSchema}),
		}
	}
}

func errorDescription(status int, reasons []string) string {
	var named []string
	scopeCheck := false
	for _, r := range reasons {
		if r == "" {
			scopeCheck = true
			continue
		}
		named = append(named, "`"+r+"`")
	}
	sort.Strings(named)
	text := http.StatusText(status)
	if len(named) > 0 {
		text += ". Reasons: " + strings.Join(named, ", ")
	}
	if scopeCheck {
		text += ". The token lacks every required scope"
	}
	return text + "."
}

// httpStatus maps a google.rpc.Code to its HTTP status, as Vanguard does.
func httpStatus(c code.Code) int {
	switch c {
	case code.Code_OK:
		return http.StatusOK
	case code.Code_CANCELLED:
		return 499
	case code.Code_INVALID_ARGUMENT, code.Code_FAILED_PRECONDITION, code.Code_OUT_OF_RANGE:
		return http.StatusBadRequest
	case code.Code_DEADLINE_EXCEEDED:
		return http.StatusGatewayTimeout
	case code.Code_NOT_FOUND:
		return http.StatusNotFound
	case code.Code_ALREADY_EXISTS, code.Code_ABORTED:
		return http.StatusConflict
	case code.Code_PERMISSION_DENIED:
		return http.StatusForbidden
	case code.Code_UNAUTHENTICATED:
		return http.StatusUnauthorized
	case code.Code_RESOURCE_EXHAUSTED:
		return http.StatusTooManyRequests
	case code.Code_UNIMPLEMENTED:
		return http.StatusNotImplemented
	case code.Code_UNAVAILABLE:
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
