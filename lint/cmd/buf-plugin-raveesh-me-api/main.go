// Command buf-plugin-raveesh-me-api is the house Buf check plugin. It checks
// the raveesh_me.http_proto_lint.v1 declarations and the guide rules the AIP linter lacks.
package main

import (
	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/http-proto-lint/lint/rules"
)

func main() {
	check.Main(rules.Spec)
}
