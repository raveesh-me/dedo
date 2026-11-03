package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// defaultDocsAddr is where dedo docs serves the reference: loopback only, so
// nothing reaches it from the network unless -tunnel shares it.
const defaultDocsAddr = "127.0.0.1:8099"

// docsFiles are the only files dedo docs serves, by URL path. The guides and
// code beside them in docs/ are never served.
var docsFiles = map[string]struct{ file, contentType string }{
	"/":             {"index.html", "text/html; charset=utf-8"},
	"/index.html":   {"index.html", "text/html; charset=utf-8"},
	"/openapi.json": {"openapi.json", "application/json"},
}

// lookPath finds cloudflared; tests replace it.
var lookPath = exec.LookPath

// tunnelURL matches the public URL cloudflared logs for a quick tunnel.
var tunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// docs serves the REST reference of the service in dir, docs/index.html and
// docs/openapi.json and nothing else, on addr until ctx ends. With tunnel it
// also shares it at a public Cloudflare quick-tunnel URL. It never tunnels the
// service itself, whose development tokens may be public.
func docs(ctx context.Context, dir, addr string, tunnel bool, out io.Writer) error {
	root := filepath.Join(dir, "docs")
	for _, f := range []string{"index.html", "openapi.json"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			return fmt.Errorf("docs/%s not found in %s: run make generate first (%w)", f, dir, err)
		}
	}
	cloudflared := ""
	if tunnel {
		path, err := lookPath("cloudflared")
		if err != nil {
			return errors.New("-tunnel needs cloudflared on PATH: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/ (macOS: brew install cloudflared)")
		}
		cloudflared = path
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("serve the docs on %s: %w (set another address with -addr)", addr, err)
	}
	local := "http://" + ln.Addr().String()
	srv := &http.Server{Handler: docsHandler(root), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	fmt.Fprintf(out, "dedo docs: the REST reference at %s/\n", local)

	if !tunnel {
		select {
		case <-ctx.Done():
			return nil
		case err := <-served:
			return fmt.Errorf("serve the docs: %w", err)
		}
	}
	return runTunnel(ctx, cloudflared, local, out)
}

// runTunnel runs a cloudflared quick tunnel to local until ctx ends, and
// prints the public URL once it is known and any errors cloudflared logs.
func runTunnel(ctx context.Context, cloudflared, local string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, cloudflared, "tunnel", "--no-autoupdate", "--url", local)
	logs, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("cloudflared: %w", err)
	}
	cmd.Stdout = cmd.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start cloudflared: %w", err)
	}
	shown := false
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case !shown && tunnelURL.MatchString(line):
			shown = true
			fmt.Fprintf(out, "dedo docs: shared at %s/ until Ctrl-C (a new address can take a minute to resolve)\n", tunnelURL.FindString(line))
		case ctx.Err() == nil && strings.Contains(line, " ERR "):
			fmt.Fprintln(out, line)
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		err = errors.New("exited")
	}
	return fmt.Errorf("cloudflared stopped before Ctrl-C: %w", err)
}

// docsHandler serves docsFiles from root, read on each request so a fresh
// make generate shows without a restart.
func docsHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := docsFiles[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := os.ReadFile(filepath.Join(root, f.file))
		if err != nil {
			http.Error(w, "docs/"+f.file+" is missing: run make generate", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", f.contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}
