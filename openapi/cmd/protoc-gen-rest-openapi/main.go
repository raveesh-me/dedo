// Command protoc-gen-rest-openapi generates an OpenAPI 3.1 document from the
// google.api.http bindings and raveesh_me.http_proto_lint.v1 options of
// protobuf services. Run it with strategy: all.
package main

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/raveesh-me/http-proto-lint/openapi/restdoc"
)

func main() {
	var cfg restdoc.Config
	protogen.Options{ParamFunc: cfg.Set}.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		return restdoc.Generate(plugin, cfg)
	})
}
