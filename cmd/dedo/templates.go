package main

import (
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/raveesh-me/dedo"
)

// templates lists the files bundled into this binary, or prints one, for
// setting a repository up by hand.
func templates(args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: dedo templates [NAME]")
	}
	if len(args) == 0 {
		return fs.WalkDir(dedo.Files, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			fmt.Fprintln(out, bundledName(path))
			return nil
		})
	}
	for _, path := range []string{"templates/" + args[0], "guidance/" + args[0]} {
		if b, err := fs.ReadFile(dedo.Files, path); err == nil {
			_, err = out.Write(b)
			return err
		}
	}
	return fmt.Errorf("no template %q; dedo templates lists them", args[0])
}

// bundledName is the name dedo templates takes: the path without its
// templates/ or guidance/ directory.
func bundledName(path string) string {
	if name, ok := strings.CutPrefix(path, "templates/"); ok {
		return name
	}
	return strings.TrimPrefix(path, "guidance/")
}
