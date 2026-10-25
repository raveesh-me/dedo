package main

import (
	"errors"
	"os"

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

// replaceSDK points go.mod's MCP Go SDK at dedo's build of it, replacing the
// replace of an earlier dedo release. It leaves a replace of the SDK that
// dedo did not write alone, and says so.
func (in *initer) replaceSDK() error {
	b, err := os.ReadFile(in.path("go.mod"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		return err
	}
	want := module.Version{Path: sdkFork, Version: sdkForkRelease}
	found := false
	var stale []*modfile.Replace
	for _, r := range f.Replace {
		switch {
		case r.Old.Path != sdkModule:
		case r.New.Path != sdkFork:
			in.say("go.mod: keeps your replace of %s; dedo's build of it is not applied", sdkModule)
			return nil
		case r.Old.Version == sdkRelease && r.New == want:
			found = true
		default:
			stale = append(stale, r)
		}
	}
	for _, r := range stale {
		// DropReplace clears r, so say what it was first.
		in.say("go.mod: removed the replace of %s by %s@%s", sdkModule, r.New.Path, r.New.Version)
		if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
			return err
		}
	}
	changed := len(stale) > 0
	if !found && sdkForkRelease != "" {
		if err := f.AddReplace(sdkModule, sdkRelease, sdkFork, sdkForkRelease); err != nil {
			return err
		}
		r := f.Replace[len(f.Replace)-1]
		r.Syntax.Before = append(r.Syntax.Before, modfile.Comment{Token: sdkForkComment})
		in.say("go.mod: replaces %s %s with %s %s", sdkModule, sdkRelease, sdkFork, sdkForkRelease)
		changed = true
	}
	if !changed {
		return nil
	}
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return err
	}
	return os.WriteFile(in.path("go.mod"), out, 0o644)
}
