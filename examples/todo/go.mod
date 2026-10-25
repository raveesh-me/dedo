module github.com/raveesh-me/dedo/examples/todo

go 1.26.1

require (
	connectrpc.com/vanguard v0.5.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pb33f/libopenapi v0.38.7
	github.com/raveesh-me/dedo v0.12.0
	github.com/raveesh-me/dedo/mcp v0.12.0
	google.golang.org/genproto v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/api v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.2 // indirect
	buf.build/gen/go/connectrpc/eliza/connectrpc/go v1.15.0-20230913231627-233fca715f49.1 // indirect
	buf.build/gen/go/connectrpc/eliza/protocolbuffers/go v1.33.0-20230913231627-233fca715f49.1 // indirect
	buf.build/go/protovalidate v1.4.0 // indirect
	cel.dev/cel-go v0.32.0 // indirect
	cel.dev/expr v0.25.3 // indirect
	connectrpc.com/connect/v2 v2.0.1 // indirect
	connectrpc.com/validate v0.9.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/pb33f/jsonpath v0.8.2 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/raveesh-me/dedo/openapi v0.12.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.6 // indirect
	golang.org/x/exp v0.0.0-20260820142414-ca536658362e // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

tool (
	connectrpc.com/connect/v2/cmd/protoc-gen-connect-go
	github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp
	github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi
	google.golang.org/protobuf/cmd/protoc-gen-go
)
