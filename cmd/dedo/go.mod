module github.com/raveesh-me/dedo/cmd/dedo

go 1.26.0

require (
	github.com/raveesh-me/dedo v0.37.0
	github.com/raveesh-me/dedo/lint v0.37.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	golang.org/x/mod v0.42.0
	golang.org/x/tools v0.52.0
)

require (
	golang.org/x/sync v0.24.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261019180522-ce908d29299b // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// v0.32.1 was tagged on a commit whose examples did not build, so its
// release job published neither the options module to the BSR nor the
// plugin binaries. v0.32.2 is the same code, released in full.
retract v0.32.1
