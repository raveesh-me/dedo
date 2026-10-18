package restdoc

// The subset of OpenAPI 3.1 the generator emits. Maps marshal with sorted
// keys, so the output is deterministic.

// Document is an OpenAPI document.
type Document struct {
	OpenAPI    string               `json:"openapi"`
	Info       Info                 `json:"info"`
	Servers    []Server             `json:"servers,omitempty"`
	Tags       []Tag                `json:"tags,omitempty"`
	Paths      map[string]*PathItem `json:"paths"`
	Components Components           `json:"components"`
}

// Info describes the API.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Server is a base URL of the API.
type Server struct {
	URL string `json:"url"`
}

// Tag groups the operations of one service.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Audience    string `json:"x-audience,omitempty"`
	OwnerTeam   string `json:"x-owner-team,omitempty"`
}

// PathItem holds the operations of one path.
type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
	Patch  *Operation `json:"patch,omitempty"`
}

// Operation is one REST binding of an RPC.
type Operation struct {
	OperationID  string                `json:"operationId"`
	Summary      string                `json:"summary,omitempty"`
	Description  string                `json:"description,omitempty"`
	Tags         []string              `json:"tags,omitempty"`
	Parameters   []Parameter           `json:"parameters,omitempty"`
	RequestBody  *RequestBody          `json:"requestBody,omitempty"`
	Responses    map[string]*Response  `json:"responses"`
	Security     []map[string][]string `json:"security,omitempty"`
	Deprecated   bool                  `json:"deprecated,omitempty"`
	Sunset       string                `json:"x-sunset,omitempty"`
	Successor    string                `json:"x-successor,omitempty"`
	Destructive  bool                  `json:"x-destructive,omitempty"`
	Procedure    string                `json:"x-rpc-procedure"`
	ErrorReasons []string              `json:"x-error-reasons,omitempty"`
}

// Parameter is a path or query parameter.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema"`
}

// RequestBody is the JSON body of a request.
type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

// Response is one possible response.
type Response struct {
	Description string               `json:"description"`
	Headers     map[string]*Header   `json:"headers,omitempty"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// Header is a response header.
type Header struct {
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema"`
}

// MediaType wraps a schema for one content type.
type MediaType struct {
	Schema *Schema `json:"schema"`
}

// Components holds the shared schemas and security schemes.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityScheme is how callers authenticate.
type SecurityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme"`
	Description string `json:"description,omitempty"`
}

// Schema is a JSON Schema as OpenAPI 3.1 uses it.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 any                `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	ReadOnly             bool               `json:"readOnly,omitempty"`
	WriteOnly            bool               `json:"writeOnly,omitempty"`
	Deprecated           bool               `json:"deprecated,omitempty"`
	Sensitive            bool               `json:"x-sensitive,omitempty"`
}

func jsonContent(s *Schema) map[string]MediaType {
	return map[string]MediaType{"application/json": {Schema: s}}
}
