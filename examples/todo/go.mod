module github.com/raveesh-me/dedo/examples/todo

go 1.26.1

require (
	connectrpc.com/connect v1.21.1
	connectrpc.com/vanguard v0.4.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pb33f/libopenapi v0.38.7
	github.com/raveesh-me/dedo v0.5.0
	github.com/raveesh-me/dedo/mcp v0.5.0
	google.golang.org/genproto v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/api v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.9-20250912141014-52f32327d4b0.1 // indirect
	buf.build/gen/go/redpandadata/common/protocolbuffers/go v1.34.2-20240917150400-3f349e63f44a.2 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/pb33f/jsonpath v0.8.2 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/raveesh-me/dedo/openapi v0.5.0 // indirect
	github.com/redpanda-data/common-go/api v0.0.0-20250801174835-9eea07f1ea06 // indirect
	github.com/redpanda-data/protoc-gen-go-mcp v0.0.0-20260729122341-6334690a62b6 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.6 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	google.golang.org/grpc v1.83.2 // indirect
)

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp
	github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi
	google.golang.org/protobuf/cmd/protoc-gen-go
)
