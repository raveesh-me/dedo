// Package restdoc generates an OpenAPI 3.1 document from the google.api.http
// bindings of protobuf services. Only those bindings are documented: the
// Connect and gRPC routes every RPC also has are transport details, not the
// public REST API.
package restdoc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// Config holds the plugin parameters.
type Config struct {
	Title    string   // info.title; defaults to the service names
	Version  string   // info.version; defaults to the package version
	Servers  []string // base URLs
	Filename string   // output file; defaults to openapi.json
}

// Set applies one plugin parameter, as protogen.Options.ParamFunc.
func (c *Config) Set(name, value string) error {
	switch name {
	case "title":
		c.Title = value
	case "version":
		c.Version = value
	case "server":
		c.Servers = append(c.Servers, value)
	case "filename":
		c.Filename = value
	default:
		return fmt.Errorf("unknown parameter %q", name)
	}
	return nil
}

type generator struct {
	schemas   *schemas
	resources map[string][]string // resource type to patterns
}

// Generate writes one OpenAPI document for every service in the files to
// generate. Run the plugin with strategy: all so it sees them together.
func Generate(plugin *protogen.Plugin, cfg Config) error {
	g := &generator{schemas: newSchemas(plugin.Files), resources: resourceIndex(plugin.Files)}
	doc := &Document{
		OpenAPI: "3.1.0",
		Paths:   map[string]*PathItem{},
		Components: Components{
			SecuritySchemes: map[string]SecurityScheme{
				bearerScheme: {Type: "http", Scheme: "bearer", Description: "An access token carrying the operation's scopes."},
			},
		},
	}
	for _, s := range cfg.Servers {
		doc.Servers = append(doc.Servers, Server{URL: s})
	}
	var names []string
	for _, f := range plugin.Files {
		if !f.Generate {
			continue
		}
		for _, svc := range f.Services {
			names = append(names, string(svc.Desc.Name()))
			doc.Tags = append(doc.Tags, tag(svc))
			if doc.Info.Version == "" {
				doc.Info.Version = string(f.Desc.Package().Name())
			}
			if err := g.addService(doc, svc); err != nil {
				return err
			}
		}
	}
	doc.Info.Title = firstNonEmpty(cfg.Title, strings.Join(names, ", "), "API")
	doc.Info.Version = firstNonEmpty(cfg.Version, doc.Info.Version, "v1")
	g.schemas.built[statusSchema] = statusSchemaDef()
	doc.Components.Schemas = g.schemas.built

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	file := plugin.NewGeneratedFile(firstNonEmpty(cfg.Filename, "openapi.json"), "")
	_, err = file.Write(append(out, '\n'))
	return err
}

func (g *generator) addService(doc *Document, svc *protogen.Service) error {
	for _, m := range svc.Methods {
		rule, _ := proto.GetExtension(m.Desc.Options(), annotations.E_Http).(*annotations.HttpRule)
		for _, b := range bindings(rule) {
			path, op, err := g.operation(svc, m, b)
			if err != nil {
				return err
			}
			item := doc.Paths[path]
			if item == nil {
				item = &PathItem{}
				doc.Paths[path] = item
			}
			if err := item.set(b.method, op); err != nil {
				return fmt.Errorf("%s: %w", m.Desc.FullName(), err)
			}
		}
	}
	return nil
}

func (p *PathItem) set(method string, op *Operation) error {
	slot := map[string]**Operation{
		http.MethodGet: &p.Get, http.MethodPut: &p.Put, http.MethodPost: &p.Post,
		http.MethodDelete: &p.Delete, http.MethodPatch: &p.Patch,
	}[method]
	if *slot != nil {
		return fmt.Errorf("%s is already bound to %s", method, (*slot).OperationID)
	}
	*slot = op
	return nil
}

func tag(svc *protogen.Service) Tag {
	rules, _ := proto.GetExtension(svc.Desc.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	t := Tag{
		Name:        serviceTag(svc),
		Description: cleanComment(svc.Comments.Leading),
		OwnerTeam:   rules.GetOwnerTeam(),
	}
	if a := rules.GetAudience(); a != dedov1.Audience_AUDIENCE_UNSPECIFIED {
		t.Audience = a.String()
	}
	return t
}

// resourceIndex maps every resource type in the files to its patterns.
func resourceIndex(files []*protogen.File) map[string][]string {
	index := map[string][]string{}
	for _, f := range files {
		defs, _ := proto.GetExtension(f.Desc.Options(), annotations.E_ResourceDefinition).([]*annotations.ResourceDescriptor)
		for _, d := range defs {
			index[d.GetType()] = d.GetPattern()
		}
		for _, m := range collectMessages(f.Messages) {
			if d, _ := proto.GetExtension(m.Desc.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor); d != nil {
				index[d.GetType()] = d.GetPattern()
			}
		}
	}
	return index
}

// statusSchemaDef is the google.rpc.Status JSON that REST errors carry.
func statusSchemaDef() *Schema {
	return &Schema{
		Type:        "object",
		Description: "An error. details holds a google.rpc.ErrorInfo whose reason is one of the operation's declared reasons.",
		Properties: map[string]*Schema{
			"code":    {Type: "integer", Format: "int32", Description: "The google.rpc.Code."},
			"message": {Type: "string", Description: "A developer-facing explanation."},
			"details": {Type: "array", Items: &Schema{
				Type:                 "object",
				Properties:           map[string]*Schema{"@type": {Type: "string"}},
				AdditionalProperties: &Schema{},
			}},
		},
		Required: []string{"code", "message"},
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
