// Command dedo sets up a Go repository to build its API with dedo.
//
//	go install github.com/raveesh-me/dedo/cmd/dedo@latest
//	dedo init
//
// dedo init adds the options module, the lint plugin, the code generators,
// the server libraries and the guidance for coding agents, each where it is
// missing, then prints a main.go to adapt. Running it again changes nothing
// that is already in place.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version is the dedo release this command installs. The release job checks
// that it names the release being tagged.
const version = "v0.10.0"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dedo:", err)
		os.Exit(1)
	}
}

const usage = `usage:
  dedo init [-dir DIR] [-offline]   set up the repository in DIR (default .)
  dedo tools [-dir DIR]             build the lint plugin pinned in go.mod into .tools/
  dedo templates [NAME]             list the bundled templates, or print one
  dedo version                      print the dedo release this command installs
`

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("no command\n" + usage)
	}
	switch args[0] {
	case "init":
		flags := flag.NewFlagSet("init", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the repository to set up")
		offline := flags.Bool("offline", false, "edit files only; print the go, buf and make commands instead of running them")
		release := flags.String("version", version, "the dedo release to pin")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return initRepo(*dir, *release, *offline, out)
	case "tools":
		flags := flag.NewFlagSet("tools", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the repository")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return tools(*dir, out)
	case "templates":
		return templates(args[1:], out)
	case "version":
		fmt.Fprintln(out, version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", args[0], usage)
}
