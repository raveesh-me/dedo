package rules

import (
	"context"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	hplv1 "github.com/raveesh-me/http-proto-lint/gen/go/raveesh_me/http_proto_lint/v1"
)

var cacheRequiresNoSideEffects = lintRule(
	idCacheRequiresNoSideEffects,
	"Checks that a cache policy appears only on NO_SIDE_EFFECTS methods, with a visibility and a max age.",
	checkutil.NewMethodRuleHandler(checkCache, checkutil.WithoutImports()),
)

func checkCache(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	cache := methodRules(m).GetCache()
	if cache == nil {
		return nil
	}
	if idempotency(m) != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idCacheRequiresNoSideEffects, "RPC %q declares a cache policy but is not NO_SIDE_EFFECTS.", m.Name()))
	}
	if cache.GetVisibility() == hplv1.CachePolicy_VISIBILITY_UNSPECIFIED {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idCacheRequiresNoSideEffects, "RPC %q cache policy declares no visibility.", m.Name()))
	}
	if cache.GetMaxAge().AsDuration() <= 0 {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idCacheRequiresNoSideEffects, "RPC %q cache policy declares no positive max age.", m.Name()))
	}
	return nil
}

func idempotency(m protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	opts, _ := m.Options().(*descriptorpb.MethodOptions)
	return opts.GetIdempotencyLevel()
}
