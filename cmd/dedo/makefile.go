package main

import (
	"regexp"
	"slices"
	"strings"
)

// makeTargets are the targets of Makefile.api a service's own Makefile may
// already have.
var makeTargets = []string{"run", "generate", "check", "test", "tools"}

// makeRule matches a rule's target and prerequisites, and not a variable
// assignment such as NAME := value.
var makeRule = regexp.MustCompile(`^([A-Za-z0-9_.-]+)[ \t]*:([^=].*)?$`)

// makefileAPI returns Makefile.api for a service whose Makefile is mk, and
// the name each of makeTargets has in it. A target mk already defines is
// named api-<name> instead, so the service's own keeps working, and the
// alias of that name, which the renamed target replaces, is dropped.
func makefileAPI(tmpl, mk []byte) ([]byte, map[string]string) {
	defined := map[string]bool{}
	for _, line := range strings.Split(string(mk), "\n") {
		if m := makeRule.FindStringSubmatch(line); m != nil {
			defined[m[1]] = true
		}
	}
	names := map[string]string{}
	for _, t := range makeTargets {
		names[t] = t
		if defined[t] {
			names[t] = "api-" + t
		}
	}
	rename := func(words []string) []string {
		out := make([]string, len(words))
		for i, w := range words {
			out[i] = w
			if n, ok := names[w]; ok {
				out[i] = n
			}
		}
		return out
	}
	var out []string
	for _, line := range strings.Split(string(tmpl), "\n") {
		m := makeRule.FindStringSubmatch(line)
		switch {
		case strings.HasPrefix(line, ".PHONY:"):
			words := strings.Fields(strings.TrimPrefix(line, ".PHONY:"))
			// An alias the renamed target replaces leaves the list too.
			words = slices.DeleteFunc(words, func(w string) bool { return isReplacedAlias(w, names) })
			line = ".PHONY: " + strings.Join(rename(words), " ")
		case m != nil && isReplacedAlias(m[1], names):
			continue
		case m != nil:
			line = rename([]string{m[1]})[0] + ":"
			if deps := strings.Fields(m[2]); len(deps) > 0 {
				line += " " + strings.Join(rename(deps), " ")
			}
		case strings.HasPrefix(line, "## "):
			if name, rest, ok := strings.Cut(strings.TrimPrefix(line, "## "), ":"); ok {
				if n, known := names[name]; known {
					line = "## " + n + ":" + rest
				}
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n")), names
}

// isReplacedAlias reports whether target is the api- alias of a target that
// had to take that very name.
func isReplacedAlias(target string, names map[string]string) bool {
	plain, ok := strings.CutPrefix(target, "api-")
	return ok && names[plain] == target
}
