package main

import (
	"errors"
	"os"
	"path"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// The MCP Go SDK decodes every message through a streaming JSON decoder that
// allocates at least 32 KiB per call, several times per tool call. Until a
// release fixes it (modelcontextprotocol/go-sdk#1453, #1454), services
// replace the SDK release dedo is built with by a build of it with the fix.
// The replace names that release, so a service that needs a later SDK gets
// the later upstream release instead. Set sdkForkRelease to "" once upstream
// has the fix: dedo upgrade then removes the replace.
const (
	sdkModule      = "github.com/modelcontextprotocol/go-sdk"
	sdkRelease     = "v1.8.0"
	sdkFork        = "github.com/raveesh-me/go-sdk"
	sdkForkRelease = "v1.8.0-dedo.1"
)

// sdkForkComment explains the replace in a service's go.mod.
const sdkForkComment = "// dedo: the MCP Go SDK with in-place JSON decoding, until a release has it" +
	" (https://github.com/modelcontextprotocol/go-sdk/pull/1454). dedo upgrade maintains this line."

// replaceSDK points the MCP Go SDK in the go.mod at rel at dedo's build of
// it, replacing the replace of an earlier dedo release, and reports whether
// it changed the file. It leaves a replace of the SDK that dedo did not
// write alone, and says so.
func (in *initer) replaceSDK(rel string) (bool, error) {
	b, err := os.ReadFile(in.path(rel))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	f, err := modfile.Parse(rel, b, nil)
	if err != nil {
		return false, err
	}
	want := module.Version{Path: sdkFork, Version: sdkForkRelease}
	found := false
	var stale []*modfile.Replace
	for _, r := range f.Replace {
		switch {
		case r.Old.Path != sdkModule:
		case r.New.Path != sdkFork:
			in.say("%s: keeps your replace of %s; dedo's build of it is not applied", rel, sdkModule)
			return false, nil
		case r.Old.Version == sdkRelease && r.New == want:
			found = true
		default:
			stale = append(stale, r)
		}
	}
	for _, r := range stale {
		// DropReplace clears r, so say what it was first.
		in.say("%s: removed the replace of %s by %s@%s", rel, sdkModule, r.New.Path, r.New.Version)
		if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
			return false, err
		}
	}
	changed := len(stale) > 0
	if !found && sdkForkRelease != "" {
		if err := f.AddReplace(sdkModule, sdkRelease, sdkFork, sdkForkRelease); err != nil {
			return false, err
		}
		r := f.Replace[len(f.Replace)-1]
		r.Syntax.Before = append(r.Syntax.Before, modfile.Comment{Token: sdkForkComment})
		in.say("%s: replaces %s %s with %s %s", rel, sdkModule, sdkRelease, sdkFork, sdkForkRelease)
		changed = true
	}
	if !changed {
		return false, nil
	}
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(in.path(rel), out, 0o644)
}

// replaceBFFSDKs keeps the replace in each BFF's go.mod as well: a BFF
// links the backend's generated MCP code, and Go reads replaces only from
// the go.mod of the module it builds. A BFF's go.sum then needs the SDK
// build, which go mod tidy there adds.
func (in *initer) replaceBFFSDKs() error {
	dirs, err := bffDirs(in.p.dir)
	if err != nil {
		return err
	}
	for _, d := range dirs {
		changed, err := in.replaceSDK(path.Join(d, "go.mod"))
		if err != nil {
			return err
		}
		if changed {
			in.say("%s/go.mod: run go mod tidy in %s/, once its code is generated, so its go.sum has the SDK build", d, d)
		}
	}
	return nil
}
