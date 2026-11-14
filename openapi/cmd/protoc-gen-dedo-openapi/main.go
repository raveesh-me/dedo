// Command protoc-gen-dedo-openapi generates an OpenAPI 3.2 document from the
// google.api.http bindings and dedo.v1 options of
// protobuf services. Run it with strategy: all.
//
// The option min_audience (an Audience, AUDIENCE_ prefix optional) leaves out
// the services for a narrower audience; unset, it leaves out the
// AUDIENCE_COMPONENT_INTERNAL ones.
//
// The option access=<path>, repeatable, reads an access file (package
// accessdoc) that an auth generator wrote: each operation gets, from the
// scopes its RPC declares, their paragraphs in its description and their
// x- extensions.
//
// The option no_tag_groups leaves out x-tagGroups, which otherwise groups
// the service tags by proto package.
package main

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/raveesh-me/dedo/openapi/restdoc"
)

func main() {
	// Warnings go to stderr, which buf shows; they do not fail generation.
	cfg := restdoc.Config{Warn: func(msg string) {
		fmt.Fprintln(os.Stderr, "protoc-gen-dedo-openapi: warning: "+msg)
	}}
	protogen.Options{ParamFunc: cfg.Set}.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		return restdoc.Generate(plugin, cfg)
	})
}
