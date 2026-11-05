module github.com/raveesh-me/dedo

go 1.26.0

require (
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261012220534-4341edf3f941
	google.golang.org/protobuf v1.36.12
)

require connectrpc.com/connect/v2 v2.0.1

require google.golang.org/genproto/googleapis/api v0.0.0-20261019180522-ce908d29299b

tool google.golang.org/protobuf/cmd/protoc-gen-go

// v0.32.1 was tagged on a commit whose examples did not build, so its
// release job published neither the options module to the BSR nor the
// plugin binaries. v0.32.2 is the same code, released in full.
retract v0.32.1
