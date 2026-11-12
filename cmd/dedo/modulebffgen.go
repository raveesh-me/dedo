package main

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// The backend's buf.gen.yaml runs its generators, OpenAPI and MCP among
// them, over the protos its inputs hold, and over the whole workspace, a
// BFF's buf module included, when it has none. Their min_audience leaves
// AUDIENCE_COMPONENT_INTERNAL services out by default, so a BFF whose
// services are that audience stays out of the backend's reference and
// tools whatever the inputs. An external BFF's services pass it, so only
// the inputs keep them out, and dedo modules checks that they do.

// genInput is one input of a buf.gen.yaml that buf reads from the
// repository: a directory or a proto file, relative to the workspace, and
// the paths it is filtered to and the paths it leaves out. An input buf
// fetches, such as a module or a Git repository, has neither.
type genInput struct {
	Directory, ProtoFile string
	Paths, ExcludePaths  []string
}

// genInputs returns the inputs of the buf.gen.yaml doc, and false when it
// has none, which buf generate reads as the whole workspace, as it does an
// empty list and a v1 template, which has no inputs.
func genInputs(doc *yaml.Node) ([]genInput, bool) {
	seq := lookup(root(doc), "inputs")
	if seq == nil || seq.Kind != yaml.SequenceNode || len(seq.Content) == 0 {
		return nil, false
	}
	out := make([]genInput, 0, len(seq.Content))
	for _, n := range seq.Content {
		out = append(out, genInput{
			Directory:    scalarValue(lookup(n, "directory")),
			ProtoFile:    scalarValue(lookup(n, "proto_file")),
			Paths:        scalars(lookup(n, "paths")),
			ExcludePaths: scalars(lookup(n, "exclude_paths")),
		})
	}
	return out, true
}

// scalarValue is the value of a scalar node, or "" for nil or another kind.
func scalarValue(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// name is how the input names itself in buf.gen.yaml.
func (in genInput) name() string {
	if in.Directory != "" {
		return in.Directory
	}
	return in.ProtoFile
}

// holds reports whether the input holds file, a proto relative to the
// workspace: a directory holds what is below it, the workspace's root
// everything, and a proto file itself.
func (in genInput) holds(file string) bool {
	switch {
	case in.Directory != "":
		d := path.Clean(in.Directory)
		return d == "." || inPath(file, d)
	case in.ProtoFile != "":
		return path.Clean(in.ProtoFile) == file
	}
	return false
}

// generates reports whether buf generate runs over file, a proto of the
// buf module at mod, from this input: the input holds it, one of its paths,
// if it has any, names it, and none of its exclude_paths does.
func (in genInput) generates(file, mod string) bool {
	names := func(filter string) bool { return filterNames(filter, file, mod) }
	if !in.holds(file) {
		return false
	}
	if len(in.Paths) > 0 && !slices.ContainsFunc(in.Paths, names) {
		return false
	}
	return !slices.ContainsFunc(in.ExcludePaths, names)
}

// filterNames reports whether filter, an entry of an input's paths or
// exclude_paths, names file, a proto of the buf module at mod. Like
// buf, it reads an entry relative to the workspace and only when it is
// inside the module: it names the file itself or a directory above it
// there. buf ignores an entry that holds the module, such as bff for
// bff/proto, and refuses one that is the module.
func filterNames(filter, file, mod string) bool {
	f := path.Clean(filter)
	return strings.HasPrefix(f, mod+"/") && inPath(file, f)
}

// rootGenerates returns why buf generate, with the buf.gen.yaml doc, runs
// its generators over one of files, protos of the buf module at mod
// relative to the workspace, or "" when it runs over none of them.
func rootGenerates(doc *yaml.Node, mod string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	inputs, ok := genInputs(doc)
	if !ok {
		return fmt.Sprintf("has no inputs, which means the whole workspace, %s included", mod)
	}
	for _, in := range inputs {
		for _, f := range files {
			if in.generates(f, mod) {
				return fmt.Sprintf("its input %s holds %s", in.name(), f)
			}
		}
	}
	return ""
}

// isExternalBFFAudience reports whether a BFF's service that declares
// audience is external: one of bffAudiences other than bffAudience, which
// the generators' default min_audience does not leave out.
func isExternalBFFAudience(audience string) bool {
	return audience != bffAudience && isBFFAudience(audience)
}

// bffGenFinding is the finding on the repository's buf.gen.yaml, in dir,
// when buf generate runs the backend's generators over the BFF's buf module
// at mod and one of its services, which are services, is external, or ""
// when it does not or there is no buf.gen.yaml. backend is a buf module of
// the backend's, such as proto, which the finding names as the input.
func bffGenFinding(dir, mod string, services []moduleService, backend string) (string, error) {
	var files, names []string
	for _, s := range services {
		if !isExternalBFFAudience(s.Audience) {
			continue
		}
		names = append(names, s.Name)
		if !slices.Contains(files, s.File) {
			files = append(files, s.File)
		}
	}
	if len(files) == 0 {
		return "", nil
	}
	doc, ok, err := loadYAML(filepath.Join(dir, "buf.gen.yaml"))
	if err != nil {
		return "", fmt.Errorf("buf.gen.yaml: %w", err)
	}
	if !ok {
		return "", nil
	}
	why := rootGenerates(doc, mod, files)
	if why == "" {
		return "", nil
	}
	return fmt.Sprintf("buf.gen.yaml: %s, so buf generate runs the backend's generators, OpenAPI and MCP among them, over the BFF's external services %s, which min_audience does not leave out: set inputs to the backend's own modules only, such as inputs: [{directory: %s}]", why, strings.Join(names, ", "), backend), nil
}

// backendModule is the first buf module of mods outside the nested Go
// modules, the backend's own, or proto when there is none.
func backendModule(mods []bufModule, nested []string) string {
	for _, m := range mods {
		if _, inside := insideModule(m.Path, nested); !inside {
			return m.Path
		}
	}
	return "proto"
}
