package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// breaking runs buf breaking for each module buf.yaml lists, comparing it
// with its own directory on the branch base. A module that is new on this
// branch, such as a backend for frontend's being added, has no protos
// there and nothing to break, so it is skipped: comparing the whole
// workspace would fail on it instead. A module whose own breaking section
// ignores it still runs, and buf passes it. Without the branch base there
// is nothing to compare with, and the check is skipped. dir may sit below
// the git root: modules are compared at their paths from the root.
func breaking(dir, base string, out io.Writer) error {
	mods, err := bufModules(dir)
	if err != nil {
		return fmt.Errorf("buf.yaml: %w", err)
	}
	if len(mods) == 0 {
		// A buf.yaml without modules is one module at its root.
		mods = []bufModule{{Path: "."}}
	}
	if !hasBranch(dir, base) {
		fmt.Fprintf(out, "buf breaking: skipped; there is no branch %s to compare with (set API_BASE to compare with another branch)\n", base)
		return nil
	}
	gitDir, prefix, err := gitInput(dir)
	if err != nil {
		return err
	}
	var failed []string
	for _, m := range mods {
		protos, err := protosOnBranch(dir, base, m.Path)
		if err != nil {
			return err
		}
		if !protos {
			fmt.Fprintf(out, "buf breaking %s: skipped; %s has no .proto files in %s, so nothing to break\n", m.Path, base, m.Path)
			continue
		}
		against := gitDir + "#branch=" + base
		if sub := path.Join(prefix, filepath.ToSlash(m.Path)); sub != "." {
			against += ",subdir=" + sub
		}
		args := []string{"breaking", m.Path, "--against", against}
		fmt.Fprintln(out, "buf "+strings.Join(args, " "))
		cmd := exec.Command("buf", args...)
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				return fmt.Errorf("buf breaking %s: %w", m.Path, err)
			}
			failed = append(failed, m.Path)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("buf breaking found breaking changes in %s, against %s", strings.Join(failed, ", "), base)
	}
	return nil
}

// gitInput returns the git root's .git as buf reads it from dir, and dir's
// path from the git root, "." when dir is the root. Symlinks are resolved
// first, as git resolves them in the root it prints.
func gitInput(dir string) (gitDir, prefix string, err error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	top, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git rev-parse --show-toplevel: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	root, err := realPath(strings.TrimSpace(string(top)))
	if err != nil {
		return "", "", err
	}
	here, err := realPath(dir)
	if err != nil {
		return "", "", err
	}
	up, err := filepath.Rel(here, root)
	if err != nil {
		return "", "", err
	}
	down, err := filepath.Rel(root, here)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(filepath.Join(up, ".git")), filepath.ToSlash(down), nil
}

// hasBranch reports whether the repository at dir has the local branch.
func hasBranch(dir, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// protosOnBranch reports whether the branch has .proto files in mod, a
// directory relative to the repository.
func protosOnBranch(dir, branch, mod string) (bool, error) {
	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", branch, "--", mod)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	files, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git ls-tree %s %s: %v: %s", branch, mod, err, strings.TrimSpace(stderr.String()))
	}
	for _, f := range strings.Split(string(files), "\n") {
		if strings.HasSuffix(f, ".proto") {
			return true, nil
		}
	}
	return false, nil
}
