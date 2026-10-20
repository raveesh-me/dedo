module github.com/raveesh-me/dedo/examples/bad-todo

go 1.26.0

require (
	connectrpc.com/connect v1.21.1
	connectrpc.com/vanguard v0.4.0
	github.com/raveesh-me/dedo v0.6.0
	google.golang.org/genproto/googleapis/api v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.58.0 // indirect
	google.golang.org/grpc v1.83.2 // indirect
)

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
