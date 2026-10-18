# Build, test and release http-proto-lint.
AIP_PLUGIN_REPO   := https://github.com/hestiatechnology/buf-plugin-aip.git
AIP_PLUGIN_COMMIT := 5158ec0f16f3
DIST              := dist
# The git ref that lint findings link to in HOUSE_RULES.md. Releases pass
# their tag, so each plugin version links to its own rule docs.
DOCS_REF          ?= main

.PHONY: all check generate format lint breaking test plugin aip-plugin plugins wasm-plugins release-binaries examples example-todo example-bad-todo vendored-options clean

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

## test: run every Go module's tests
test:
	go vet ./...
	go test -race -cover ./...
	for mod in lint openapi mcp; do (cd $$mod && go vet ./... && go test -race -cover ./...) || exit 1; done

# Lint plugins run natively: about 17 times faster per `buf lint` than the
# same plugins as WebAssembly, which matters in the editor. buf.yaml refers
# to them by path, so editors started outside a shell still find them.
HOUSE_CMD     := ./cmd/buf-plugin-raveesh-me-api
HOUSE_LDFLAGS := -X github.com/raveesh-me/http-proto-lint/lint/rules.DocsRef=$(DOCS_REF)
LINT_SRC      := $(shell find lint gen -name '*.go') lint/go.mod lint/go.sum
AIP_SRC       := $(DIST)/aip-src/.pinned-$(AIP_PLUGIN_COMMIT)
# Platforms the release ships native plugins for. Windows users can use the
# WebAssembly plugins on the BSR.
RELEASE_PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

## plugin: build the house plugin for this machine
plugin: $(DIST)/buf-plugin-raveesh-me-api

$(DIST)/buf-plugin-raveesh-me-api: $(LINT_SRC)
	mkdir -p $(DIST)
	cd lint && go build -trimpath -ldflags "$(HOUSE_LDFLAGS)" -o ../$@ $(HOUSE_CMD)

## aip-plugin: build buf-plugin-aip at the pinned commit for this machine
aip-plugin: $(DIST)/buf-plugin-aip

$(DIST)/buf-plugin-aip: $(AIP_SRC)
	cd $(DIST)/aip-src && go build -trimpath -o ../buf-plugin-aip .

$(AIP_SRC):
	rm -rf $(DIST)/aip-src
	git clone --quiet $(AIP_PLUGIN_REPO) $(DIST)/aip-src
	git -C $(DIST)/aip-src checkout --quiet $(AIP_PLUGIN_COMMIT)
	touch $@

## plugins: both plugins for this machine, as buf.yaml in this repository uses them
plugins: plugin aip-plugin

## wasm-plugins: both plugins as WebAssembly, for the BSR
wasm-plugins: $(LINT_SRC) $(AIP_SRC)
	mkdir -p $(DIST)/wasm
	cd lint && GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags "$(HOUSE_LDFLAGS)" \
		-o ../$(DIST)/wasm/buf-plugin-raveesh-me-api.wasm $(HOUSE_CMD)
	cd $(DIST)/aip-src && GOOS=wasip1 GOARCH=wasm go build -trimpath -o ../wasm/buf-plugin-aip.wasm .

## release-binaries: both plugins natively for every release platform
release-binaries: $(LINT_SRC) $(AIP_SRC)
	mkdir -p $(DIST)/release
	for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		(cd lint && GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(HOUSE_LDFLAGS)" \
			-o ../$(DIST)/release/buf-plugin-raveesh-me-api_$${os}_$${arch} $(HOUSE_CMD)) || exit 1; \
		(cd $(DIST)/aip-src && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o ../release/buf-plugin-aip_$${os}_$${arch} .) || exit 1; \
	done

## examples: run both example services as consumer repositories
examples: vendored-options example-todo example-bad-todo

# Each example owns a copy of the options module until it is on the BSR.
vendored-options:
	for ex in todo bad-todo; do diff -r proto/raveesh_me examples/$$ex/third_party/http-proto-lint/raveesh_me || exit 1; done

## example-todo: the compliant service lints clean and passes its tests
example-todo: plugins
	cd examples/todo && buf format --diff --exit-code && buf lint proto
	cd examples/todo && buf generate && git diff --exit-code gen/ docs/
	cd examples/todo && go vet ./... && go test -race ./...

## example-bad-todo: the rule-breaking service reports exactly the expected findings
example-bad-todo: plugins
	cd examples/bad-todo && buf format --diff --exit-code
	cd examples/bad-todo && buf lint proto --error-format=json | jq -r '"\(.start_line) \(.type)"' | sort -k1,1n -k2,2 | diff -u expected-lint.txt -
	cd examples/bad-todo && buf generate && git diff --exit-code gen/
	cd examples/bad-todo && go vet ./... && go test -race ./...

clean:
	rm -rf $(DIST)
