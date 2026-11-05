module github.com/raveesh-me/dedo/cmd/dedo

go 1.26.0

require (
	github.com/raveesh-me/dedo v0.32.3
	github.com/raveesh-me/dedo/lint v0.32.3
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/mod v0.41.0

// v0.32.1 was tagged on a commit whose examples did not build, so its
// release job published neither the options module to the BSR nor the
// plugin binaries. v0.32.2 is the same code, released in full.
retract v0.32.1
