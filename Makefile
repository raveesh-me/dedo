# Build, test and release http-proto-lint.
AIP_PLUGIN_REPO   := https://github.com/hestiatechnology/buf-plugin-aip.git
AIP_PLUGIN_COMMIT := 5158ec0f16f3
DIST              := dist

.PHONY: all check generate format lint breaking test plugin aip-plugin plugins examples example-todo example-bad-todo vendored-options clean

all: check

## check: everything CI runs
check: format generate test plugin lint examples

## generate: regenerate Go code for the options module and fail if it changed
generate:
	buf generate
	git diff --exit-code gen/

format:
	buf format --diff --exit-code

## lint: buf lint the options module and the example with the house plugin
lint: plugin
	buf lint

## breaking: compare the options module against main
breaking:
	buf breaking proto --against '.git#branch=main,subdir=proto'

## test: run both Go modules' tests
test:
	go vet ./...
	go test -race -cover ./...
	cd lint && go vet ./... && go test -race -cover ./...

## plugin: build the house plugin as WebAssembly
plugin: $(DIST)/buf-plugin-raveesh-me-api.wasm

$(DIST)/buf-plugin-raveesh-me-api.wasm: $(shell find lint gen -name '*.go') lint/go.mod lint/go.sum
	mkdir -p $(DIST)
	cd lint && GOOS=wasip1 GOARCH=wasm go build -trimpath -o ../$@ ./cmd/buf-plugin-raveesh-me-api

## aip-plugin: build buf-plugin-aip at the pinned commit as WebAssembly
aip-plugin: $(DIST)/buf-plugin-aip.wasm

$(DIST)/buf-plugin-aip.wasm:
	rm -rf $(DIST)/aip-src
	git clone --quiet $(AIP_PLUGIN_REPO) $(DIST)/aip-src
	git -C $(DIST)/aip-src checkout --quiet $(AIP_PLUGIN_COMMIT)
	cd $(DIST)/aip-src && GOOS=wasip1 GOARCH=wasm go build -trimpath -o ../buf-plugin-aip.wasm .
	rm -rf $(DIST)/aip-src

## plugins: both release assets
plugins: plugin aip-plugin

## examples: run both example services as consumer repositories
examples: vendored-options example-todo example-bad-todo

# Each example owns a copy of the options module until it is on the BSR.
vendored-options:
	for ex in todo bad-todo; do diff -r proto/raveesh_me examples/$$ex/third_party/http-proto-lint/raveesh_me || exit 1; done

## example-todo: the compliant service lints clean and passes its tests
example-todo: plugins
	cd examples/todo && buf format --diff --exit-code && buf lint proto
	cd examples/todo && buf generate && git diff --exit-code gen/
	cd examples/todo && go vet ./... && go test -race ./...

## example-bad-todo: the rule-breaking service reports exactly the expected findings
example-bad-todo: plugins
	cd examples/bad-todo && buf format --diff --exit-code
	cd examples/bad-todo && buf lint proto --error-format=json | jq -r '"\(.start_line) \(.type)"' | sort -k1,1n -k2,2 | diff -u expected-lint.txt -
	cd examples/bad-todo && buf generate && git diff --exit-code gen/
	cd examples/bad-todo && go vet ./... && go test -race ./...

clean:
	rm -rf $(DIST)
