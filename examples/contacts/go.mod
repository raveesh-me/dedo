module github.com/raveesh-me/dedo/examples/contacts

go 1.26.1

require (
	connectrpc.com/connect/v2 v2.0.1
	connectrpc.com/validate v0.9.0
	connectrpc.com/vanguard v0.5.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/raveesh-me/dedo v0.14.0
	github.com/raveesh-me/dedo/mcp v0.14.0
	google.golang.org/genproto/googleapis/api v0.0.0-20261019180522-ce908d29299b
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.2 // indirect
	buf.build/go/protovalidate v1.4.0 // indirect
	cel.dev/cel-go v0.32.0 // indirect
	cel.dev/expr v0.25.3 // indirect
	connectrpc.com/connect v1.21.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/raveesh-me/dedo/openapi v0.14.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/exp v0.0.0-20260820142414-ca536658362e // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

tool (
	connectrpc.com/connect/v2/cmd/protoc-gen-connect-go
	github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp
	github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi
	google.golang.org/protobuf/cmd/protoc-gen-go
)

// dedo: the MCP Go SDK with in-place JSON decoding, until a release has it (https://github.com/modelcontextprotocol/go-sdk/pull/1454). dedo upgrade maintains this line.
replace github.com/modelcontextprotocol/go-sdk v1.8.0 => github.com/raveesh-me/go-sdk v1.8.0-dedo.1
