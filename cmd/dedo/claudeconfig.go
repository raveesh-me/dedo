package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// mcpServers is the member of Claude Desktop's config that lists the local
// servers it launches.
const mcpServers = "mcpServers"

// setMCPServer sets one server in Claude Desktop's config file, creating
// the file if needed and keeping everything else in it as it was.
func setMCPServer(cfg, name string, entry json.RawMessage) error {
	return editMCPServers(cfg, func(servers object) object { return servers.with(name, entry) })
}

// removeMCPServer removes one server from Claude Desktop's config file.
func removeMCPServer(cfg, name string, out io.Writer) error {
	removed := false
	err := editMCPServers(cfg, func(servers object) object {
		if _, ok := servers.get(name); ok {
			removed = true
			return servers.without(name)
		}
		return servers
	})
	if err != nil {
		return err
	}
	if !removed {
		fmt.Fprintf(out, "%s: no mcpServers.%s; nothing to remove\n", cfg, name)
		return nil
	}
	fmt.Fprintf(out, "%s: removed mcpServers.%s; restart Claude Desktop\n", cfg, name)
	return nil
}

// editMCPServers rewrites the mcpServers of cfg with edit. It writes only
// when something changed, keeps the previous file as cfg.bak, and replaces
// the file in one rename, so Claude Desktop never reads half of it.
func editMCPServers(cfg string, edit func(object) object) error {
	old, err := os.ReadFile(cfg)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	doc, err := parseObject(old)
	if err != nil {
		return fmt.Errorf("%s: %w", cfg, err)
	}
	servers := object{}
	if raw, ok := doc.get(mcpServers); ok {
		if servers, err = parseObject(raw); err != nil {
			return fmt.Errorf("%s: %s: %w", cfg, mcpServers, err)
		}
	}
	edited := edit(servers)
	if old == nil && len(edited) == 0 {
		return nil
	}
	raw, err := json.Marshal(edited)
	if err != nil {
		return err
	}
	b, err := doc.with(mcpServers, raw).indent()
	if err != nil {
		return err
	}
	if old != nil && bytes.Equal(compact(old), compact(b)) {
		return nil
	}
	return replaceFile(cfg, old, b)
}

// compact is b without insignificant space, or b if it is not JSON.
func compact(b []byte) []byte {
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return b
	}
	return out.Bytes()
}

// replaceFile writes b to path through a temporary file and a rename,
// keeping old, when there was a file, as path.bak. A symlinked path is
// written where the link points, and the link kept. A new file and the
// backup are readable by their owner only: the config can hold tokens.
func replaceFile(path string, old, b []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if old != nil {
		if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
			return fmt.Errorf("backing up %s: %w", path, err)
		}
		if err := os.Chmod(path+".bak", 0o600); err != nil {
			return fmt.Errorf("backing up %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
