package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestWriteSLO(t *testing.T) {
	out := t.TempDir()
	// A file dedo slo wrote for a service that is gone, and one of the
	// user's own.
	stale := filepath.Join(out, "acme-gone-v1-goneservice.yaml")
	own := filepath.Join(out, "notes.yaml")
	if err := os.WriteFile(stale, []byte(sloHeader+"kind: Service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(own, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := sloService(t, standardLevel(), nil)
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{protodesc.ToFileDescriptorProto(svc.ParentFile())}}
	var log bytes.Buffer
	if err := writeSLO(set, out, sloOptions{}, &log); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "acme-orders-v1-orderservice.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), sloHeader) || !strings.Contains(string(b), "kind: SLO") {
		t.Errorf("written:\n%s", b)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the stale file is still there")
	}
	if _, err := os.Stat(own); err != nil {
		t.Error("the user's own file was removed")
	}
	if !strings.Contains(log.String(), "removed") {
		t.Errorf("output = %s", log.String())
	}
}

func TestWriteSLOReportsBadDeclarations(t *testing.T) {
	level := standardLevel()
	level.Window = nil
	svc := sloService(t, level, nil)
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{protodesc.ToFileDescriptorProto(svc.ParentFile())}}
	err := writeSLO(set, t.TempDir(), sloOptions{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "window") {
		t.Errorf("err = %v, want one about the window", err)
	}
}

// dedo slo builds the protos with buf, resolving dedo's options.
func TestSLOCommandBuildsWithBuf(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf is not installed")
	}
	dir := t.TempDir()
	optionsProto, err := os.ReadFile("../../proto/dedo/v1/options.proto")
	if err != nil {
		t.Fatal(err)
	}
	codeProto, err := os.ReadFile("../../lint/rules/testdata/deps/google/rpc/code.proto")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"buf.yaml":                          "version: v2\nmodules:\n  - path: proto\n  - path: third_party\n",
		"third_party/dedo/v1/options.proto": string(optionsProto),
		"third_party/google/rpc/code.proto": string(codeProto),
		"proto/acme/notes/v1/notes.proto": `syntax = "proto3";

package acme.notes.v1;

import "dedo/v1/options.proto";

service NoteService {
  option (dedo.v1.service) = {
    owner_team: "notes"
    service_level: {
      objectives: {availability: 0.995}
      window: {seconds: 604800}
    }
  };
  rpc GetNote(GetNoteRequest) returns (GetNoteRequest) {}
}

message GetNoteRequest {}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"slo", "-dir", dir}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "slo", "acme-notes-v1-noteservice.yaml"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, want := range []string{"target: 0.995", "duration: 7d", "- acme.notes.v1.NoteService/GetNote"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("documents lack %q:\n%s", want, b)
		}
	}
}
