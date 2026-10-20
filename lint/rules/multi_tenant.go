package rules

import (
	"context"
	"regexp"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"buf.build/go/bufplugin/descriptor"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/policy"
)

// The DEDO_MULTI_TENANT_* rules are opt-in: a repository whose services
// partition their data between tenants adds DEDO_MULTI_TENANT to lint.use.

var multiTenantServiceTenancy = optInRule(
	idMultiTenantServiceTenancy,
	"Checks that every service declares its tenant collection in dedo.v1.service.tenancy.",
	checkutil.NewServiceRuleHandler(checkServiceTenancy, checkutil.WithoutImports()),
)

var multiTenantResourcePattern = optInRule(
	idMultiTenantResourcePattern,
	"Checks that every resource pattern of a multi-tenant package starts with the tenant, such as locations/{location}/.",
	checkutil.NewFileRuleHandler(checkResourcePatterns, checkutil.WithoutImports()),
)

var multiTenantRequestTenant = optInRule(
	idMultiTenantRequestTenant,
	"Checks that every RPC of a multi-tenant service binds a request field to the tenant in its HTTP path, unless it is cross_tenant.",
	checkutil.NewMethodRuleHandler(checkRequestTenant, checkutil.WithoutImports()),
)

var multiTenantCachePrivate = optInRule(
	idMultiTenantCachePrivate,
	"Checks that no RPC of a multi-tenant service lets shared caches store its responses.",
	checkutil.NewMethodRuleHandler(checkTenantCache, checkutil.WithoutImports()),
)

// collectionID is an AIP-122 collection identifier, such as locations.
var collectionID = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

func checkServiceTenancy(_ context.Context, w check.ResponseWriter, _ check.Request, s protoreflect.ServiceDescriptor) error {
	switch collection := tenantCollection(s); {
	case collection == "":
		w.AddAnnotation(check.WithDescriptor(s),
			messagef(idMultiTenantServiceTenancy, "Service %q declares no tenancy: set dedo.v1.service.tenancy.collection, such as \"locations\".", s.Name()))
	case !collectionID.MatchString(collection):
		w.AddAnnotation(check.WithDescriptor(s),
			messagef(idMultiTenantServiceTenancy, "Service %q tenant collection %q is not a collection ID in lowerCamelCase, such as \"locations\".", s.Name(), collection))
	}
	return nil
}

// checkResourcePatterns checks the resources a file declares, on messages
// and as resource definitions, against the tenant collections the
// services of its package declare.
func checkResourcePatterns(_ context.Context, w check.ResponseWriter, req check.Request, fd descriptor.FileDescriptor) error {
	f := fd.ProtoreflectFileDescriptor()
	collections := packageCollections(req, f.Package())
	if len(collections) == 0 {
		return nil
	}
	check1 := func(d protoreflect.Descriptor, res *annotations.ResourceDescriptor) {
		for _, pattern := range res.GetPattern() {
			if !underTenant(pattern, collections) {
				w.AddAnnotation(check.WithDescriptor(d),
					messagef(idMultiTenantResourcePattern, "Resource %q pattern %q does not start with the tenant, such as %q.", res.GetType(), pattern, collections[0]+"/{id}/"))
			}
		}
	}
	definitions, _ := proto.GetExtension(f.Options(), annotations.E_ResourceDefinition).([]*annotations.ResourceDescriptor)
	for _, res := range definitions {
		check1(f, res)
	}
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := 0; i < msgs.Len(); i++ {
			m := msgs.Get(i)
			if res := extension[*annotations.ResourceDescriptor](m.Options(), annotations.E_Resource); res != nil {
				check1(m, res)
			}
			walk(m.Messages())
		}
	}
	walk(f.Messages())
	return nil
}

func checkRequestTenant(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	collection := tenantCollection(m.Parent().(protoreflect.ServiceDescriptor))
	crossTenant := methodRules(m).GetCrossTenant()
	switch {
	case collection == "" && crossTenant:
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idMultiTenantRequestTenant, "RPC %q is cross_tenant, but its service declares no tenancy.", m.Name()))
		return nil
	case collection == "" || crossTenant:
		return nil
	}
	if _, err := policy.MethodTenant(m, httpRule(m), collection); err != nil {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idMultiTenantRequestTenant, "RPC %q names no tenant: %s.", m.Name(), err))
	}
	return nil
}

func checkTenantCache(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	if tenantCollection(m.Parent().(protoreflect.ServiceDescriptor)) == "" {
		return nil
	}
	if methodRules(m).GetCache().GetVisibility() == dedov1.CachePolicy_VISIBILITY_PUBLIC {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idMultiTenantCachePrivate, "RPC %q serves tenant data with a public cache policy; use VISIBILITY_PRIVATE.", m.Name()))
	}
	return nil
}

func tenantCollection(s protoreflect.ServiceDescriptor) string {
	return serviceRules(s).GetTenancy().GetCollection()
}

// packageCollections lists the tenant collections declared by the services
// of a package, in the files of the request.
func packageCollections(req check.Request, pkg protoreflect.FullName) []string {
	var collections []string
	for _, fd := range req.FileDescriptors() {
		f := fd.ProtoreflectFileDescriptor()
		if f.Package() != pkg {
			continue
		}
		for i := 0; i < f.Services().Len(); i++ {
			if c := tenantCollection(f.Services().Get(i)); c != "" && !contains(collections, c) {
				collections = append(collections, c)
			}
		}
	}
	return collections
}

// underTenant reports whether a resource pattern starts with a tenant, as
// locations/{location}/todos/{todo} or the tenant's own locations/{location}.
func underTenant(pattern string, collections []string) bool {
	for _, c := range collections {
		if strings.HasPrefix(pattern, c+"/{") {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
