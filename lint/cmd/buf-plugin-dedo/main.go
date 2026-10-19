// Command buf-plugin-dedo is the house Buf check plugin. It checks
// the dedo.v1 declarations and the guide rules the AIP linter lacks.
package main

import (
	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/rules"
)

func main() {
	check.Main(rules.Spec)
}
