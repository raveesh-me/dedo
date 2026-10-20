// Command buf-plugin-dedo is dedo's Buf check plugin. It runs Google's AIP
// linter (AIP_*), the Zalando guidelines dedo adopts (ZALANDO_*) and dedo's
// own rules (DEDO_*) in one pass. See guidance/RULES.md.
package main

import (
	"fmt"
	"os"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/rules"
)

func main() {
	spec, err := rules.NewSpec()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	check.Main(spec)
}
