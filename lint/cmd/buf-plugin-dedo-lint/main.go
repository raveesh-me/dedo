// Command buf-plugin-dedo-lint is dedo's Buf check plugin. It runs Google's AIP
// linter (AIP_*), the Zalando guidelines dedo adopts (ZALANDO_*), dedo's own
// rules (DEDO_*) and the repository's house rules (HOUSE_*) in one pass,
// each overridden rule left out. See guidance/RULES.md.
//
// Run with -dedo-rules, it consolidates the rules of the repository it is
// started in, reports every override and exits; dedo tools runs it so.
package main

import (
	"fmt"
	"os"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/rules"
)

// rulesFlag asks for the consolidated rules instead of serving Buf, which
// passes only its own flags.
const rulesFlag = "-dedo-rules"

func main() {
	if len(os.Args) == 2 && os.Args[1] == rulesFlag {
		c, err := rules.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(c.Report())
		return
	}
	spec, err := rules.NewSpec()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	check.Main(spec)
}
