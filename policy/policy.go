// Package policy reads the declared operational contract of each RPC from its
// descriptors once, at startup.
package policy

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// Method is the operational contract of one RPC.
type Method struct {
	Procedure string // "/raveesh_me.library.v1.LibraryService/GetBook"
	Audience  dedov1.Audience
	Rules     *dedov1.MethodRules
}

// Table maps procedure names to their contracts.
type Table map[string]Method

// Build reads every named service from the global registry and fails on a
// missing declaration.
func Build(services ...string) (Table, error) {
	return BuildFrom(protoregistry.GlobalFiles, services...)
}

// BuildFrom reads every named service from files and fails on a missing
// declaration.
func BuildFrom(files *protoregistry.Files, services ...string) (Table, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("policy: no services named")
	}
	table := Table{}
	for _, name := range services {
		svc, err := findService(files, name)
		if err != nil {
			return nil, err
		}
		if err := addService(table, svc); err != nil {
			return nil, err
		}
	}
	return table, nil
}

func findService(files *protoregistry.Files, name string) (protoreflect.ServiceDescriptor, error) {
	d, err := files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, fmt.Errorf("policy: service %s: %w", name, err)
	}
	svc, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("policy: %s is not a service", name)
	}
	return svc, nil
}

func addService(table Table, svc protoreflect.ServiceDescriptor) error {
	svcRules, _ := proto.GetExtension(svc.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	if svcRules == nil {
		return fmt.Errorf("policy: service %s has no dedo.v1.service option", svc.FullName())
	}
	methods := svc.Methods()
	for i := 0; i < methods.Len(); i++ {
		m := methods.Get(i)
		rules, _ := proto.GetExtension(m.Options(), dedov1.E_Method).(*dedov1.MethodRules)
		// A method without scopes would be an open endpoint, so fail closed
		// even though lint already requires them.
		if rules == nil || len(rules.GetScopes()) == 0 {
			return fmt.Errorf("policy: method %s declares no scopes", m.FullName())
		}
		procedure := fmt.Sprintf("/%s/%s", svc.FullName(), m.Name())
		table[procedure] = Method{
			Procedure: procedure,
			Audience:  svcRules.GetAudience(),
			Rules:     rules,
		}
	}
	return nil
}
