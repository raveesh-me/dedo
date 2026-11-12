package config

import (
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"

	"gopkg.in/yaml.v3"
)

// modulePath is dedo's module, whose version names the running dedo.
const modulePath = "github.com/raveesh-me/dedo"

// unknownField is the yaml error for a key no field of the type takes.
var unknownField = regexp.MustCompile(`^line (\d+): field (\S+) not found in type config\.(\w+)$`)

// places names, for each type a key can be unknown in, where it was.
var places = map[string]string{
	"file":      "the top of the file",
	"project":   "the top of the file",
	"section":   "a lint or breaking section",
	"Rule":      "a rule",
	"goSection": "the go section",
	"GoRule":    "a go rule",
	"Import":    "an import",
	"Annexure":  "an annexure",
}

// explain rewrites the yaml errors for unknown keys in path, which are a
// typo or a file written for a newer dedo, to say where they are and what
// to do. dedo reads every file strictly: a key it skipped could be a rule
// that silently doesn't run. Other errors keep yaml's wording.
func explain(path string, err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return fmt.Errorf("%s: %w", path, err)
	}
	dedo := "dedo"
	if v := dedoVersion(); v != "" {
		dedo += " " + v
	}
	lines := make([]string, 0, len(te.Errors))
	for _, e := range te.Errors {
		m := unknownField.FindStringSubmatch(e)
		place, ok := "", false
		if m != nil {
			place, ok = places[m[3]]
		}
		switch {
		case !ok:
			lines = append(lines, fmt.Sprintf("%s: %s", path, e))
		case m[2] == "annexure" && m[3] == "file":
			lines = append(lines, fmt.Sprintf("%s:%s: annexure belongs in the repository's own dedo.yaml; a rules file adds rules", path, m[1]))
		default:
			lines = append(lines, fmt.Sprintf("%s:%s: %s doesn't know `%s` in %s. If it's a typo, fix it; if the file was written for a newer dedo, run `dedo update && dedo upgrade`", path, m[1], dedo, m[2], place))
		}
	}
	return errors.New(strings.Join(lines, "\n"))
}

// dedoVersion is the release of dedo this binary was built from, as the dedo
// command or as a dependency of the lint plugin, or "" for a development
// build.
func dedoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := ""
	if info.Main.Path == modulePath {
		v = info.Main.Version
	}
	for _, d := range info.Deps {
		if d.Path == modulePath {
			v = d.Version
		}
	}
	if !strings.HasPrefix(v, "v") {
		return "" // (devel), or not built from dedo at all
	}
	return v
}
