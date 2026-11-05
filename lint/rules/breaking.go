package rules

import (
	"context"
	"fmt"
	"slices"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// CategoryBreaking holds every breaking rule of buf-plugin-dedo-lint,
// dedo's and the house's, which buf breaking runs where buf.yaml's
// breaking.use lists it; dedo.yaml's breaking section chooses which of
// them run. They check what Buf's own breaking rules cannot see: the
// values of options, which Buf never compares.
const CategoryBreaking = "DEDO_BREAKING"

// Breaking lists dedo's own breaking rules, the DEDO set of dedo.yaml's
// breaking section.
var Breaking = []*check.RuleSpec{
	emitsNoDelete,
	scopesNoDelete,
	errorReasonSameCode,
	audienceNoNarrow,
	tenancySame,
	crossTenantNoDelete,
	httpBindingSame,
	fieldNoNewRequired,
	resourceSame,
	sunsetNoEarlier,
}

// DefaultBreakingUse are the sets that run when dedo.yaml's breaking
// section does not say use.
var DefaultBreakingUse = []string{CategoryDedo, "HOUSE"}

func breakingRule(id, purpose string, handler check.RuleHandler) *check.RuleSpec {
	return &check.RuleSpec{
		ID:          id,
		CategoryIDs: []string{CategoryDedo},
		Default:     true,
		Purpose:     purpose,
		Type:        check.RuleTypeBreaking,
		Handler:     handler,
	}
}

// breakingAt annotates an element and the one it was before.
func breakingAt(w check.ResponseWriter, d, against protoreflect.Descriptor, id, format string, args ...any) {
	w.AddAnnotation(check.WithDescriptor(d), check.WithAgainstDescriptor(against), messagef(id, format, args...))
}

var emitsNoDelete = breakingRule(
	idEmitsNoDelete,
	"Checks that an RPC still declares every event type it declared in emits, so consumers of a type keep receiving it.",
	checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MethodDescriptor) error {
		now := methodRules(m).GetEmits()
		for _, t := range methodRules(against).GetEmits() {
			if !slices.Contains(now, t) {
				breakingAt(w, m, against, idEmitsNoDelete, "RPC %q no longer declares event type %q, which consumers may subscribe to. Keep it, and add a new type alongside if the meaning changed.", m.Name(), t)
			}
		}
		return nil
	}, checkutil.WithoutImports()),
)

var scopesNoDelete = breakingRule(
	idScopesNoDelete,
	"Checks that an RPC still accepts every token scope it accepted, so no caller loses access.",
	checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MethodDescriptor) error {
		now := methodRules(m).GetScopes()
		for _, s := range methodRules(against).GetScopes() {
			if !slices.Contains(now, s) {
				breakingAt(w, m, against, idScopesNoDelete, "RPC %q no longer accepts scope %q, so callers whose token holds only that scope lose access.", m.Name(), s)
			}
		}
		return nil
	}, checkutil.WithoutImports()),
)

var errorReasonSameCode = breakingRule(
	idErrorReasonSameCode,
	"Checks that an error reason an RPC still declares keeps its status code.",
	checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MethodDescriptor) error {
		for _, before := range methodRules(against).GetErrors() {
			for _, now := range methodRules(m).GetErrors() {
				if now.GetReason() == before.GetReason() && now.GetCode() != before.GetCode() {
					breakingAt(w, m, against, idErrorReasonSameCode, "RPC %q returns reason %q with %s, not %s: clients that handle the reason by its code break. Declare a new reason instead.", m.Name(), now.GetReason(), now.GetCode(), before.GetCode())
				}
			}
		}
		return nil
	}, checkutil.WithoutImports()),
)

var audienceNoNarrow = breakingRule(
	idAudienceNoNarrow,
	"Checks that a service's audience does not narrow, so no caller it admitted is shut out.",
	checkutil.NewServicePairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, s, against protoreflect.ServiceDescriptor) error {
		now, before := serviceRules(s).GetAudience(), serviceRules(against).GetAudience()
		if before != dedov1.Audience_AUDIENCE_UNSPECIFIED && now < before {
			breakingAt(w, s, against, idAudienceNoNarrow, "Service %q narrows its audience from %s to %s, shutting out callers it admitted.", s.Name(), before, now)
		}
		return nil
	}, checkutil.WithoutImports()),
)

var tenancySame = breakingRule(
	idTenancySame,
	"Checks that a service's tenancy does not change: every request names its tenant in the same collection.",
	checkutil.NewServicePairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, s, against protoreflect.ServiceDescriptor) error {
		now, before := tenantCollection(s), tenantCollection(against)
		if now == before {
			return nil
		}
		breakingAt(w, s, against, idTenancySame, "Service %q changes its tenancy from %s to %s: every resource name and request path changes with it.", s.Name(), tenancyName(before), tenancyName(now))
		return nil
	}, checkutil.WithoutImports()),
)

func tenancyName(collection string) string {
	if collection == "" {
		return "none"
	}
	return fmt.Sprintf("%q", collection)
}

var crossTenantNoDelete = breakingRule(
	idCrossTenantNoDelete,
	"Checks that a cross-tenant RPC stays cross-tenant, so callers that span tenants keep working.",
	checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MethodDescriptor) error {
		if methodRules(against).GetCrossTenant() && !methodRules(m).GetCrossTenant() {
			breakingAt(w, m, against, idCrossTenantNoDelete, "RPC %q is no longer cross_tenant: requests that span tenants, or name none, now fail the tenant check.", m.Name())
		}
		return nil
	}, checkutil.WithoutImports()),
)

// httpBinding is one REST binding of an RPC.
type httpBinding struct{ verb, path, body, responseBody string }

func httpBindings(rule *annotations.HttpRule) []httpBinding {
	if rule == nil {
		return nil
	}
	var out []httpBinding
	if verb, path := httpVerbPath(rule); path != "" {
		out = append(out, httpBinding{verb: verb, path: path, body: rule.GetBody(), responseBody: rule.GetResponseBody()})
	}
	for _, b := range rule.GetAdditionalBindings() {
		out = append(out, httpBindings(b)...)
	}
	return out
}

func httpVerbPath(rule *annotations.HttpRule) (string, string) {
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "GET", p.Get
	case *annotations.HttpRule_Post:
		return "POST", p.Post
	case *annotations.HttpRule_Put:
		return "PUT", p.Put
	case *annotations.HttpRule_Patch:
		return "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		return "DELETE", p.Delete
	case *annotations.HttpRule_Custom:
		return p.Custom.GetKind(), p.Custom.GetPath()
	}
	return "", ""
}

var httpBindingSame = breakingRule(
	idHTTPBindingSame,
	"Checks that every REST binding of an RPC stays, at the same verb and path, with the same body and response_body.",
	checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MethodDescriptor) error {
		now := httpBindings(httpRule(m))
		for _, b := range httpBindings(httpRule(against)) {
			i := slices.IndexFunc(now, func(n httpBinding) bool { return n.verb == b.verb && n.path == b.path })
			switch {
			case i < 0:
				breakingAt(w, m, against, idHTTPBindingSame, "RPC %q is no longer served at %s %s, which REST clients call. Keep the binding, as an additional_binding if a new path joins it.", m.Name(), b.verb, b.path)
			case now[i].body != b.body:
				breakingAt(w, m, against, idHTTPBindingSame, "RPC %q at %s %s takes body %q instead of %q, so REST clients send the wrong shape.", m.Name(), b.verb, b.path, now[i].body, b.body)
			case now[i].responseBody != b.responseBody:
				breakingAt(w, m, against, idHTTPBindingSame, "RPC %q at %s %s returns response_body %q instead of %q, so REST clients read the wrong shape.", m.Name(), b.verb, b.path, now[i].responseBody, b.responseBody)
			}
		}
		return nil
	}, checkutil.WithoutImports()),
)

var fieldNoNewRequired = breakingRule(
	idFieldNoNewRequired,
	"Checks that no field becomes REQUIRED, since callers that leave it out would start failing.",
	checkutil.NewFieldPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, f, against protoreflect.FieldDescriptor) error {
		if required(f) && !required(against) {
			breakingAt(w, f, against, idFieldNoNewRequired, "Field %q becomes REQUIRED, so callers that leave it out start failing. Add a new field instead, or keep it optional.", f.FullName())
		}
		return nil
	}, checkutil.WithoutImports()),
)

func required(f protoreflect.FieldDescriptor) bool {
	behaviors, _ := proto.GetExtension(f.Options(), annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	return slices.Contains(behaviors, annotations.FieldBehavior_REQUIRED)
}

var resourceSame = breakingRule(
	idResourceSame,
	"Checks that a resource keeps its type and every name pattern it had, so stored names and references still resolve.",
	checkutil.NewMessagePairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MessageDescriptor) error {
		before := extension[*annotations.ResourceDescriptor](against.Options(), annotations.E_Resource)
		if before == nil {
			return nil
		}
		now := extension[*annotations.ResourceDescriptor](m.Options(), annotations.E_Resource)
		if now.GetType() != before.GetType() {
			breakingAt(w, m, against, idResourceSame, "Message %q is resource %q, not %q: references to the old type no longer resolve.", m.Name(), now.GetType(), before.GetType())
			return nil
		}
		for _, p := range before.GetPattern() {
			if !slices.Contains(now.GetPattern(), p) {
				breakingAt(w, m, against, idResourceSame, "Resource %q no longer has the pattern %q, so names stored in it no longer resolve. Keep it, and add the new pattern beside it.", before.GetType(), p)
			}
		}
		return nil
	}, checkutil.WithoutImports()),
)

var sunsetNoEarlier = breakingRule(
	idSunsetNoEarlier,
	"Checks that a deprecated RPC's sunset does not move earlier than callers were told.",
	checkutil.NewMethodPairRuleHandler(func(_ context.Context, w check.ResponseWriter, _ check.Request, m, against protoreflect.MethodDescriptor) error {
		before := methodRules(against).GetDeprecation().GetSunsetTime()
		now := methodRules(m).GetDeprecation().GetSunsetTime()
		if before == nil || now == nil {
			return nil
		}
		if now.AsTime().Before(before.AsTime()) {
			breakingAt(w, m, against, idSunsetNoEarlier, "RPC %q now sunsets on %s, before the %s its callers were told.", m.Name(), now.AsTime().UTC().Format("2006-01-02"), before.AsTime().UTC().Format("2006-01-02"))
		}
		return nil
	}, checkutil.WithoutImports()),
)
