module github.com/raveesh-me/http-proto-lint/examples/todo

go 1.26.0

require (
	connectrpc.com/connect v1.21.1
	connectrpc.com/vanguard v0.4.0
	github.com/raveesh-me/http-proto-lint v0.0.0
	google.golang.org/genproto v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/api v0.0.0-20261012220534-4341edf3f941
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

replace github.com/raveesh-me/http-proto-lint => ../..
