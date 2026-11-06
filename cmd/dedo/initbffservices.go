package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// bffProto is what dedo init -bff reads from one of the BFF's protos: its
// directory below bff/proto, its package and its services.
type bffProto struct {
	Dir      string // shop/ui/v1
	Package  string // shop.ui.v1
	Services []string
}

// bffService is one of the BFF's services, as main.go registers it and its
// handler implements it.
type bffService struct {
	Name     string // UiService
	Type     string // uiService, the handler's type
	New      string // newUiService, the handler's constructor
	File     string // ui_service.go, the handler's file in bff/
	GoPkg    string // uiv1
	ProtoDir string // shop/ui/v1, where buf generate writes its Go code
}

// bffHandler is what the handler template of one service is filled with.
type bffHandler struct {
	bffService
	Module       string
	Backend      *service
	BackendField string
}

// bffProtos reads the protos below dir, bff/proto, or none when it does
// not exist.
func bffProtos(dir string) ([]bffProto, error) {
	rels, err := findProtos(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []bffProto
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		f := bffProto{Dir: path.Dir(filepath.ToSlash(rel))}
		if m := protoPkg.FindSubmatch(src); m != nil {
			f.Package = string(m[1])
		}
		for _, m := range protoSvc.FindAllStringSubmatch(commentLine.ReplaceAllString(string(src), ""), -1) {
			f.Services = append(f.Services, m[1])
		}
		out = append(out, f)
	}
	return out, nil
}

// bffServices returns the services of the BFF's protos, each named apart
// from the others in package main, and the directories of the protos.
func bffServices(protos []bffProto) ([]bffService, []string) {
	count := map[string]int{}
	for _, f := range protos {
		for _, s := range f.Services {
			count[s]++
		}
	}
	var services []bffService
	var dirs []string
	for _, f := range protos {
		if !slices.Contains(dirs, f.Dir) {
			dirs = append(dirs, f.Dir)
		}
		goPkg := goPackageName(f.Package)
		for _, name := range f.Services {
			// Two packages may declare a service of the same name; the
			// Go package's name tells their handlers apart.
			id := name
			if count[name] > 1 {
				id = strings.ToUpper(goPkg[:1]) + goPkg[1:] + name
			}
			services = append(services, bffService{
				Name:     name,
				Type:     strings.ToLower(id[:1]) + id[1:],
				New:      "new" + id,
				File:     snakeCase(id) + ".go",
				GoPkg:    goPkg,
				ProtoDir: f.Dir,
			})
		}
	}
	slices.Sort(dirs)
	return services, dirs
}

// Imports are the Connect packages main.go imports, one per proto
// directory that declares a service.
func (n bffNames) Imports() []string {
	var out []string
	for _, s := range n.Services {
		imp := n.Module + "/" + bffDir + "/gen/go/" + s.ProtoDir + "/" + s.GoPkg + "connect"
		if !slices.Contains(out, imp) {
			out = append(out, imp)
		}
	}
	return out
}

// bffRootInputs warns when the backend's buf.gen.yaml would generate the
// BFF's protos too: with no inputs it covers the whole workspace, bff/proto
// included, and runs every generator of the backend over it, OpenAPI and
// MCP among them, into the backend's gen/go.
func (in *initer) bffRootInputs(bffNames) error {
	doc, ok, err := loadYAML(in.path("buf.gen.yaml"))
	if err != nil {
		return fmt.Errorf("buf.gen.yaml: %w", err)
	}
	if !ok {
		return nil
	}
	protoRoot := in.p.protoRoot
	if protoRoot == "" {
		protoRoot = "proto"
	}
	fix := fmt.Sprintf("so buf generate runs the backend's generators, OpenAPI and MCP among them, over the BFF's protos: set\n    inputs:\n      - directory: %s", protoRoot)
	inputs := lookup(root(doc), "inputs")
	if inputs == nil || len(inputs.Content) == 0 {
		in.say("buf.gen.yaml: WARNING has no inputs, which means the whole workspace, %s/proto included, %s", bffDir, fix)
		return nil
	}
	for _, input := range inputs.Content {
		d := lookup(input, "directory")
		if d == nil {
			continue
		}
		dir := path.Clean(d.Value)
		if dir == "." || dir == bffDir || strings.HasPrefix(dir, bffDir+"/") || strings.HasPrefix(bffDir, dir+"/") {
			in.say("buf.gen.yaml: WARNING its input %s holds %s/proto, %s", d.Value, bffDir, fix)
			return nil
		}
	}
	in.say("buf.gen.yaml: its inputs leave %s/proto out", bffDir)
	return nil
}
