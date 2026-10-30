package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// A block is a section of CLAUDE.md and AGENTS.md that dedo owns: its own
// guidance for coding agents, and each annexure dedo.yaml names. dedo init
// and dedo upgrade write every block afresh, in order, where the first one
// was, so the copies stay what this release and dedo.yaml say.
type block struct {
	key  string // guidance, generators, or annexure:<path or url>
	text string
	keep bool // the source could not be read offline: keep the copy there
}

const (
	beginPrefix = "<!-- dedo:begin "
	endPrefix   = "<!-- dedo:end "
	annexureKey = "annexure:"
)

func (b block) render() string {
	return fmt.Sprintf("%s%s (dedo init and dedo upgrade rewrite this block: change its source, not this copy) -->\n%s\n%s%s -->\n",
		beginPrefix, b.key, strings.TrimRight(b.text, "\n"), endPrefix, b.key)
}

// guidance writes dedo's blocks into CLAUDE.md and AGENTS.md, or creates
// AGENTS.md: dedo's guidance; where dedo init adds no generators or server,
// the guidance for adding them by hand; then the annexures of dedo.yaml,
// which dedo's guidance says win where they conflict with it.
func (in *initer) guidance() error {
	blocks, err := in.guidanceBlocks()
	if err != nil {
		return err
	}
	var files []string
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if _, err := os.Stat(in.path(name)); err == nil {
			files = append(files, name)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if len(files) == 0 {
		files = []string{"AGENTS.md"}
	}
	for _, name := range files {
		if err := in.writeBlocks(name, blocks); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// guidanceBlocks returns the blocks this release and dedo.yaml call for.
func (in *initer) guidanceBlocks() ([]block, error) {
	blocks := []block{{key: "guidance", text: string(asset("guidance/claude.md"))}}
	if in.p.schemaOnly {
		blocks = append(blocks, block{key: "generators", text: string(asset("guidance/generators.md"))})
	}
	path := in.path(config.FileName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return blocks, nil
	}
	annexures, err := config.Annexures(path)
	if err != nil {
		return nil, err
	}
	for _, a := range annexures {
		b := block{key: annexureKey + a.Name()}
		text, err := config.ReadAnnexure(in.p.dir, a)
		switch {
		case err != nil && in.offline && a.URL != "":
			in.say("%s: the annexure %s is not fetched offline; its copy stays as it is", config.FileName, a.Name())
			b.keep = true
		case err != nil:
			return nil, fmt.Errorf("%s: %w", config.FileName, err)
		}
		b.text = string(text)
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// writeBlocks writes blocks into the file name, replacing dedo's blocks
// there, and reports what changed.
func (in *initer) writeBlocks(name string, blocks []block) error {
	b, err := os.ReadFile(in.path(name))
	created := errors.Is(err, fs.ErrNotExist)
	if err != nil && !created {
		return err
	}
	rest, at, had, err := splitBlocks(string(b))
	if err != nil {
		return err
	}
	var out []string
	var changes []string
	for _, bl := range blocks {
		old, ok := had[bl.key]
		if bl.keep {
			if !ok {
				continue
			}
			bl.text = old
		}
		out = append(out, bl.render())
		switch {
		case !ok:
			changes = append(changes, "added "+label(bl.key))
		case old != strings.TrimRight(bl.text, "\n"):
			changes = append(changes, "updated "+label(bl.key))
		}
	}
	for key := range had {
		if !slices.ContainsFunc(blocks, func(bl block) bool { return bl.key == key }) {
			changes = append(changes, "removed "+label(key))
		}
	}
	text := join(rest[:at], strings.Join(out, "\n"), rest[at:])
	if text == string(b) {
		in.say("%s: dedo's guidance is up to date", name)
		return nil
	}
	if err := os.WriteFile(in.path(name), []byte(text), 0o644); err != nil {
		return err
	}
	slices.Sort(changes)
	switch {
	case created:
		in.say("%s: created with dedo's guidance", name)
	case len(changes) == 0:
		in.say("%s: marked dedo's guidance as dedo's, so dedo upgrade keeps it current", name)
	default:
		in.say("%s: %s", name, strings.Join(changes, ", "))
	}
	return nil
}

func label(key string) string {
	switch {
	case key == "guidance":
		return "dedo's guidance"
	case key == "generators":
		return "the guidance for adding dedo's generators"
	default:
		return "the annexure " + strings.TrimPrefix(key, annexureKey)
	}
}

// legacyMarkers head the guidance releases before v0.20.0 appended without
// block markers. It runs to the next top-level heading.
var legacyMarkers = []string{guidanceMarker, generatorsMarker}

// splitBlocks takes dedo's blocks out of a file. It returns the remaining
// lines, the line where the first block was (the end when there was none),
// and the text of each block by key; guidance an earlier release appended
// without markers counts as its block.
func splitBlocks(s string) (rest []string, at int, had map[string]string, err error) {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if s == "" {
		lines = nil
	}
	had = map[string]string{}
	at = -1
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		key, legacy := blockStart(line)
		if key == "" {
			rest = append(rest, line)
			continue
		}
		if at < 0 {
			at = len(rest)
		}
		var end int
		if legacy {
			end = legacyEnd(lines, i+1)
		} else {
			end = slices.Index(lines[i+1:], endPrefix+key+" -->")
			if end < 0 {
				return nil, 0, nil, fmt.Errorf("dedo's block %q has no %q line; restore it, or delete the block and run dedo upgrade", key, endPrefix+key+" -->")
			}
			end += i + 1
		}
		body := lines[i+1 : end]
		if legacy {
			body = lines[i:end]
		}
		had[key] = strings.TrimRight(strings.Join(body, "\n"), "\n")
		i = end
		if legacy {
			i = end - 1
		}
	}
	if at < 0 {
		at = len(rest)
	}
	return rest, at, had, nil
}

// blockStart returns the key of the block a line opens, and whether it is
// guidance appended without markers.
func blockStart(line string) (string, bool) {
	if k, ok := strings.CutPrefix(line, beginPrefix); ok {
		k, _, _ = strings.Cut(k, " ")
		return k, false
	}
	switch line {
	case guidanceMarker:
		return "guidance", true
	case generatorsMarker:
		return "generators", true
	}
	return "", false
}

// legacyEnd returns the line after guidance appended without markers: the
// next top-level heading outside a code block, or the end.
func legacyEnd(lines []string, from int) int {
	code := false
	for i := from; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			code = !code
		}
		if !code && (strings.HasPrefix(l, "# ") || strings.HasPrefix(l, beginPrefix)) {
			return i
		}
	}
	return len(lines)
}

// join puts dedo's blocks between the file's own lines, a blank line apart.
func join(before []string, blocks string, after []string) string {
	b := strings.TrimRight(strings.Join(before, "\n"), "\n ")
	a := strings.TrimLeft(strings.Join(after, "\n"), "\n")
	var out strings.Builder
	if b != "" {
		out.WriteString(b + "\n\n")
	}
	out.WriteString(blocks)
	if a != "" {
		out.WriteString("\n" + strings.TrimRight(a, "\n") + "\n")
	}
	return out.String()
}
