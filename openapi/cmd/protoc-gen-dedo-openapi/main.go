// Command protoc-gen-dedo-openapi generates an OpenAPI 3.2 document from the
// google.api.http bindings and dedo.v1 options of
// protobuf services. Run it with strategy: all.
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
