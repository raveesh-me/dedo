package aip_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"buf.build/go/bufplugin/check/checktest"
)

func TestCollectionURISuffix(t *testing.T) {
	for _, tt := range []struct {
		name, method, binding string
		invalid               bool
	}{
		{"kebab collection", "SuggestTariffCodes", `post: "/shop/v1/tariff-codes:suggest"`, false},
		{"single word", "SortBooks", `post: "/v1/books:sort"`, false},
		{"camel collection", "SuggestTariffCodes", `post: "/v1/tariffCodes:suggest"`, false},
		{"noun differs", "ResolveAddress", `post: "/v1/addresses:resolveAddress"`, false},
		{"kebab noun differs", "ResolveTariffCode", `post: "/v1/tariff-codes:resolveTariffCode"`, false},
		{"wrong verb", "SuggestTariffCodes", `post: "/v1/tariff-codes:guess"`, true},
		{"wrong collection", "SuggestTariffCodes", `post: "/v1/other-codes:suggest"`, true},
		{"missing suffix", "SuggestTariffCodes", `post: "/v1/tariff-codes"`, true},
		{"two valid bindings", "SuggestTariffCodes", `post: "/v1/tariff-codes:suggest" additional_bindings: {get: "/v2/tariff-codes:suggest"}`, false},
		{"invalid additional binding", "SuggestTariffCodes", `post: "/v1/tariff-codes:suggest" additional_bindings: {get: "/v2/tariff-codes:guess"}`, true},
		{"invalid primary binding", "SuggestTariffCodes", `post: "/v1/tariff-codes:guess" additional_bindings: {get: "/v2/tariff-codes:suggest"}`, true},
		{"custom binding", "SuggestTariffCodes", `custom: {kind: "HEAD" path: "/v1/tariff-codes:suggest"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := fmt.Sprintf(`syntax = "proto3";
package example.shop.v1;
import "google/api/annotations.proto";
service Shop {
  rpc %s(Request) returns (Response) {
    option (google.api.http) = {%s};
  }
}
message Request {}
message Response {}
`, tt.method, tt.binding)
			if err := os.WriteFile(filepath.Join(dir, "shop.proto"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			var annotations []checktest.ExpectedAnnotation
			if tt.invalid {
				annotations = []checktest.ExpectedAnnotation{{
					RuleID: "AIP_0136_HTTP_URI_SUFFIX",
					FileLocation: &checktest.ExpectedFileLocation{
						FileName: "shop.proto", StartLine: 5, StartColumn: 4,
						EndLine: 5, EndColumn: len(fmt.Sprintf("    option (google.api.http) = {%s};", tt.binding)),
					},
				}}
			}
			checktest.CheckTest{
				Request: &checktest.RequestSpec{
					Files: &checktest.ProtoFileSpec{
						DirPaths:  []string{dir, "../rules/testdata/deps"},
						FilePaths: []string{"shop.proto"},
					},
					RuleIDs: []string{"AIP_0136_HTTP_URI_SUFFIX"},
				},
				Spec: spec(t), ExpectedAnnotations: annotations,
			}.Run(t)
		})
	}
}
