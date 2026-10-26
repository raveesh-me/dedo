# Build, test and release dedo.
DIST              := dist
# The git ref that lint findings link to in guidance/RULES.md. Releases pass
# their tag, so each plugin version links to its own rule docs.
DOCS_REF          ?= main

.PHONY: all check starter generate format lint breaking test plugin plugins wasm-plugins release-binaries examples example-todo example-bad-todo example-contacts vendored-options clean

all: check

## check: everything CI runs
check: format generate test plugin lint examples starter

## generate: regenerate Go code for the options module and fail if it changed
generate:
	buf generate
	git diff --exit-code gen/

format:
	buf format --diff --exit-code

## lint: buf lint the options module with buf-plugin-dedo-lint
lint: plugin
	buf lint

## breaking: compare the options module against main
breaking:
	buf breaking proto --against '.git#branch=main,subdir=proto'

## test: run every Go module's tests
test:
	go vet ./...
	go test -race -cover ./...
	for mod in lint openapi mcp cmd/dedo; do (cd $$mod && go vet ./... && go test -race -cover ./...) || exit 1; done

# Lint plugins run natively: about 17 times faster per `buf lint` than the
# same plugins as WebAssembly, which matters in the editor. buf.yaml refers
# to them by path, so editors started outside a shell still find them.
DEDO_CMD     := ./cmd/buf-plugin-dedo-lint
DEDO_LDFLAGS := -X github.com/raveesh-me/dedo/lint/rules.DocsRef=$(DOCS_REF)
LINT_SRC      := $(shell find lint gen -name '*.go') lint/go.mod lint/go.sum
# Platforms the release ships native plugins for. Windows users can use the
# WebAssembly plugins on the BSR.
RELEASE_PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

## plugin: build buf-plugin-dedo-lint for this machine
plugin: $(DIST)/buf-plugin-dedo-lint

$(DIST)/buf-plugin-dedo-lint: $(LINT_SRC)
	mkdir -p $(DIST)
	cd lint && go build -trimpath -ldflags "$(DEDO_LDFLAGS)" -o ../$@ $(DEDO_CMD)

## plugins: every lint plugin for this machine, as the examples' buf.yaml use them
plugins: plugin

## wasm-plugins: buf-plugin-dedo-lint as WebAssembly, for the BSR
wasm-plugins: $(LINT_SRC)
	mkdir -p $(DIST)/wasm
	cd lint && GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags "$(DEDO_LDFLAGS)" \
		-o ../$(DIST)/wasm/buf-plugin-dedo-lint.wasm $(DEDO_CMD)

## release-binaries: the lint plugin, the generators and the dedo command natively for every release platform
release-binaries: $(LINT_SRC)
	mkdir -p $(DIST)/release
	for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		(cd lint && GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(DEDO_LDFLAGS)" \
			-o ../$(DIST)/release/buf-plugin-dedo-lint_$${os}_$${arch} $(DEDO_CMD)) || exit 1; \
		(cd mcp && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o ../$(DIST)/release/protoc-gen-dedo-mcp_$${os}_$${arch} ./cmd/protoc-gen-dedo-mcp) || exit 1; \
		(cd openapi && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o ../$(DIST)/release/protoc-gen-dedo-openapi_$${os}_$${arch} ./cmd/protoc-gen-dedo-openapi) || exit 1; \
		(cd cmd/dedo && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o ../../$(DIST)/release/dedo_$${os}_$${arch} .) || exit 1; \
	done

## starter: the starter service dedo init writes passes every rule, so no two
## rules contradict each other on it (with the todo example and the library)
STARTER := $(DIST)/_starter

starter: plugins
	rm -rf $(STARTER) && mkdir -p $(STARTER)/svc $(STARTER)/lint
	printf 'module example.com/starter\n' > $(STARTER)/svc/go.mod
	cd cmd/dedo && go run . init -offline -dir $(CURDIR)/$(STARTER)/svc > /dev/null
	cp -R examples/todo/buf.yaml examples/todo/buf.lock examples/todo/third_party $(STARTER)/svc/proto $(STARTER)/svc/rules.yml $(STARTER)/lint/
	sed -i.bak -e 's#\.\./\.\./dist/buf-plugin-dedo-lint#$(CURDIR)/$(DIST)/buf-plugin-dedo-lint#' $(STARTER)/lint/buf.yaml
	cd $(STARTER)/lint && buf lint proto

## examples: run the example services as consumer repositories
examples: vendored-options example-todo example-bad-todo example-contacts

# Each example owns a copy of the options module until it is on the BSR.
vendored-options:
	for ex in todo bad-todo contacts; do diff -r proto/dedo examples/$$ex/third_party/dedo/dedo || exit 1; done
	for ex in todo contacts; do diff templates/docs/docs.go.tmpl examples/$$ex/docs/docs.go || exit 1; done

## example-todo: the compliant service lints clean and passes its tests
example-todo: plugins
	cd examples/todo && buf format --diff --exit-code && buf lint proto
	cd examples/todo && buf generate && git diff --exit-code gen/ docs/
	$(MAKE) -C examples/todo web-gen && git diff --exit-code examples/todo/web/src/gen
	cd examples/todo && go vet ./... && go test -race ./...

## example-contacts: the multi-tenant service lints clean and passes its tests
example-contacts: plugins
	cd examples/contacts && buf format --diff --exit-code && buf lint proto
	cd examples/contacts && buf generate && git diff --exit-code gen/ docs/
	cd examples/contacts && go vet ./... && go test -race ./...

## example-bad-todo: the rule-breaking service reports exactly the expected findings
example-bad-todo: plugins
	cd examples/bad-todo && buf format --diff --exit-code
	cd examples/bad-todo && buf lint proto --error-format=msvs | sed -E 's/^[^(]*\(([0-9]+),[0-9]+\) : [a-z]+ ([A-Z0-9_]+) : .*/\1 \2/' | sort -k1,1n -k2,2 | diff -u expected-lint.txt -
	cd examples/bad-todo && buf generate && git diff --exit-code gen/
	cd examples/bad-todo && go vet ./... && go test -race ./...

clean:
	rm -rf $(DIST)
