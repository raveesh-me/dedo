package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	// The options' extensions, registered so that buf's image resolves them.
	_ "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// slo writes the OpenSLO documents of the service level objectives the
// protos in dir declare, one file per service, into outDir. It removes the
// files it wrote before for services that no longer declare any.
func slo(dir, outDir string, opts sloOptions, out io.Writer) error {
	set, err := bufImage(dir)
	if err != nil {
		return err
	}
	return writeSLO(set, filepath.Join(dir, outDir), opts, out)
}

// bufImage builds the protos in dir, without their imports.
func bufImage(dir string) (*descriptorpb.FileDescriptorSet, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("buf", "build", "--exclude-imports", "--exclude-source-info", "-o", "-#format=binpb")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("buf build: %w\n%s", err, stderr.String())
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := (proto.UnmarshalOptions{Resolver: protoregistry.GlobalTypes}).Unmarshal(stdout.Bytes(), set); err != nil {
		return nil, fmt.Errorf("reading buf's image: %w", err)
	}
	return set, nil
}

// writeSLO writes the documents of every service of set that declares
// objectives.
func writeSLO(set *descriptorpb.FileDescriptorSet, outDir string, opts sloOptions, out io.Writer) error {
	written := map[string]bool{}
	var errs []error
	for _, fdp := range set.GetFile() {
		fd, err := (protodesc.FileOptions{AllowUnresolvable: true}).New(fdp, unresolvable{})
		if err != nil {
			return fmt.Errorf("%s: %w", fdp.GetName(), err)
		}
		services := fd.Services()
		for i := range services.Len() {
			svc := services.Get(i)
			b, err := openSLO(svc, opts)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if b == nil {
				continue
			}
			name := sloName(string(svc.FullName())) + ".yaml"
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(outDir, name), b, 0o644); err != nil {
				return err
			}
			written[name] = true
			fmt.Fprintf(out, "%s: OpenSLO documents of %s\n", filepath.Join(outDir, name), svc.FullName())
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return removeStaleSLO(outDir, written, out)
}

// removeStaleSLO removes the files dedo slo wrote before that it did not
// write this time.
func removeStaleSLO(outDir string, written map[string]bool, out io.Writer) error {
	entries, err := os.ReadDir(outDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stale []string
	for _, e := range entries {
		if e.IsDir() || written[e.Name()] || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(b), sloHeader) {
			stale = append(stale, e.Name())
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		if err := os.Remove(filepath.Join(outDir, name)); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: removed; its service declares no objectives\n", filepath.Join(outDir, name))
	}
	return nil
}

// unresolvable resolves nothing, so protodesc builds a file whose imports
// were left out of the image with placeholders for them.
type unresolvable struct{}

func (unresolvable) FindFileByPath(string) (protoreflect.FileDescriptor, error) {
	return nil, protoregistry.NotFound
}

func (unresolvable) FindDescriptorByName(protoreflect.FullName) (protoreflect.Descriptor, error) {
	return nil, protoregistry.NotFound
}
