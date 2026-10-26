module github.com/raveesh-me/dedo/examples/bad-todo

go 1.26.0

require (
	connectrpc.com/connect/v2 v2.0.1
	connectrpc.com/vanguard v0.5.0
	github.com/raveesh-me/dedo v0.15.0
	google.golang.org/genproto/googleapis/api v0.0.0-20261019180522-ce908d29299b
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

tool (
	connectrpc.com/connect/v2/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
