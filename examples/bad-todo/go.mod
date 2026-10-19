module github.com/raveesh-me/dedo/examples/bad-todo

go 1.26.0

require (
	connectrpc.com/connect v1.21.1
	connectrpc.com/vanguard v0.4.0
	github.com/raveesh-me/dedo v0.5.0
	github.com/raveesh-me/http-proto-lint v0.2.0
	github.com/raveesh-me/http-proto-lint/examples/bad-todo v0.0.0-20261019230733-c7db126b3080
	google.golang.org/genproto/googleapis/api v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

require golang.org/x/text v0.41.0 // indirect

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
