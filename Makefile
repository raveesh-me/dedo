# Build, test and release dedo.
DIST              := dist
# The git ref that lint findings link to in guidance/RULES.md. Releases pass
# their tag, so each plugin version links to its own rule docs.
DOCS_REF          ?= main

.PHONY: all check generate format lint breaking test plugin plugins wasm-plugins release-binaries templates-archive examples example-todo example-bad-todo vendored-options clean

all: check

## check: everything CI runs
check: format generate test plugin lint examples

## generate: regenerate Go code for the options module and fail if it changed
generate:
	buf generate
	git diff --exit-code gen/

format:
	buf format --diff --exit-code

## lint: buf lint the options module with buf-plugin-dedo
lint: plugin
	buf lint

## breaking: compare the options module against main
breaking:
	buf breaking proto --against '.git#branch=main,subdir=proto'

## test: run every Go module's tests
test:
	go vet ./...
	go test -race -cover ./...
	for mod in lint openapi mcp examples/house-lint; do (cd $$mod && go vet ./... && go test -race -cover ./...) || exit 1; done

# Lint plugins run natively: about 17 times faster per `buf lint` than the
# same plugins as WebAssembly, which matters in the editor. buf.yaml refers
# to them by path, so editors started outside a shell still find them.
DEDO_CMD     := ./cmd/buf-plugin-dedo
DEDO_LDFLAGS := -X github.com/raveesh-me/dedo/lint/rules.DocsRef=$(DOCS_REF)
LINT_SRC      := $(shell find lint gen -name '*.go') lint/go.mod lint/go.sum
# Platforms the release ships native plugins for. Windows users can use the
# WebAssembly plugins on the BSR.
RELEASE_PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

## plugin: build buf-plugin-dedo for this machine
plugin: $(DIST)/buf-plugin-dedo

$(DIST)/buf-plugin-dedo: $(LINT_SRC)
	mkdir -p $(DIST)
	cd lint && go build -trimpath -ldflags "$(DEDO_LDFLAGS)" -o ../$@ $(DEDO_CMD)

# An organisation's own HOUSE_* plugin; in this repository, the example one.
HOUSE_SRC := $(shell find examples/house-lint -name '*.go') examples/house-lint/go.mod examples/house-lint/go.sum

$(DIST)/buf-plugin-house: $(HOUSE_SRC)
	mkdir -p $(DIST)
	cd examples/house-lint && go build -trimpath -o ../../$@ ./cmd/buf-plugin-house

## plugins: every lint plugin for this machine, as the examples' buf.yaml use them
plugins: plugin $(DIST)/buf-plugin-house

## wasm-plugins: buf-plugin-dedo as WebAssembly, for the BSR
wasm-plugins: $(LINT_SRC)
	mkdir -p $(DIST)/wasm
	cd lint && GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags "$(DEDO_LDFLAGS)" \
		-o ../$(DIST)/wasm/buf-plugin-dedo.wasm $(DEDO_CMD)

## release-binaries: the lint plugin and the generators natively for every release platform
release-binaries: $(LINT_SRC) templates-archive
	mkdir -p $(DIST)/release
	for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		(cd lint && GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(DEDO_LDFLAGS)" \
			-o ../$(DIST)/release/buf-plugin-dedo_$${os}_$${arch} $(DEDO_CMD)) || exit 1; \
		(cd mcp && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o ../$(DIST)/release/protoc-gen-dedo-mcp_$${os}_$${arch} ./cmd/protoc-gen-dedo-mcp) || exit 1; \
		(cd openapi && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o ../$(DIST)/release/protoc-gen-dedo-openapi_$${os}_$${arch} ./cmd/protoc-gen-dedo-openapi) || exit 1; \
	done

## templates-archive: what a service copies, as one release asset:
## templates/, the REST docs page and the agent guidance
templates-archive:
	rm -rf $(DIST)/dedo-templates && mkdir -p $(DIST)/dedo-templates/docs $(DIST)/release
	cp -R templates/. $(DIST)/dedo-templates/
	cp examples/todo/docs/docs.go examples/todo/docs/index.html $(DIST)/dedo-templates/docs/
	cp guidance/claude.md $(DIST)/dedo-templates/
	tar -czf $(DIST)/release/dedo-templates.tar.gz -C $(DIST) dedo-templates

## examples: run both example services as consumer repositories
examples: vendored-options example-todo example-bad-todo

# Each example owns a copy of the options module until it is on the BSR.
vendored-options:
	for ex in todo bad-todo; do diff -r proto/dedo examples/$$ex/third_party/dedo/dedo || exit 1; done

## example-todo: the compliant service lints clean and passes its tests
example-todo: plugins
	cd examples/todo && buf format --diff --exit-code && buf lint proto
	cd examples/todo && buf generate && git diff --exit-code gen/ docs/
	$(MAKE) -C examples/todo web-gen && git diff --exit-code examples/todo/web/src/gen
	cd examples/todo && go vet ./... && go test -race ./...

## example-bad-todo: the rule-breaking service reports exactly the expected findings
example-bad-todo: plugins
	cd examples/bad-todo && buf format --diff --exit-code
	cd examples/bad-todo && buf lint proto --error-format=msvs | sed -E 's/^[^(]*\(([0-9]+),[0-9]+\) : [a-z]+ ([A-Z0-9_]+) : .*/\1 \2/' | sort -k1,1n -k2,2 | diff -u expected-lint.txt -
	cd examples/bad-todo && buf generate && git diff --exit-code gen/
	cd examples/bad-todo && go vet ./... && go test -race ./...

clean:
	rm -rf $(DIST)
