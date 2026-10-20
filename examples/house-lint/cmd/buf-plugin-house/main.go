// Command buf-plugin-house is an example of an organisation's own Buf check
// plugin, holding its HOUSE_* rules.
package main

import (
	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/examples/house-lint/rules"
)

func main() {
	check.Main(rules.Spec)
}
