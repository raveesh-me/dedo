# Install dedo in a Go service

Each section adds one part of dedo to an existing Go module. `dedo init` does all of them, editing what is already there:

```sh
go install github.com/raveesh-me/dedo/cmd/dedo@v0.8.0
dedo init
```

It never removes your own deps, plugins or generators, and running it again changes nothing that is in place. It ends by printing a `main.go` for your service. `dedo init -offline` edits the files only and prints the `go`, `buf` and `make` commands instead of running them.

New to dedo? [tutorial.md](tutorial.md) builds a whole service from an empty directory.

You need Go 1.25 or later and the [Buf CLI](https://buf.build/docs/installation).

## 1. Annotations: the options module

Add dedo's options module, and the two modules it builds on, to `buf.yaml`:

```yaml
version: v2
deps:
  - buf.build/googleapis/googleapis
  - buf.build/bufbuild/protovalidate
  - buf.build/r-a/dedo
```

```sh
buf dep update
```

Then import it in your `.proto` files and declare each service's and RPC's contract:

```protobuf
import "dedo/v1/options.proto";
```

[guideline.md](guideline.md#3-write-the-contract) lists every option.

## 2. Linting: buf-plugin-dedo-lint

Download the plugin for your machine with the targets in `Makefile.api`:

```sh
dedo templates Makefile.api > Makefile.api
printf 'include Makefile.api\n' >> Makefile
printf '.tools/\n' >> .gitignore
make tools          # downloads .tools/buf-plugin-dedo-lint for DEDO_VERSION
```

List the plugin and its categories in `buf.yaml`, with the AIP rules dedo skips on purpose (the full list is in [templates/buf.yaml](../templates/buf.yaml); [RULES.md](RULES.md#skipped-rules) says why):

```yaml
lint:
  use:
    - STANDARD
    - COMMENTS
    - AIP_NOLANG
    - DEDO
    - ZALANDO
  except:
    - AIP_0131_RESPONSE_MESSAGE_NAME
    # ...the rest of the list in templates/buf.yaml
plugins:
  - plugin: .tools/buf-plugin-dedo-lint
```

```sh
make api-check      # buf format, buf lint, buf breaking against main
```

On Windows, use the Wasm plugin from the BSR instead: `- plugin: buf.build/r-a/dedo-lint`.

## 3. Code generators

Pin the generators as Go tools:

```sh
go get -tool google.golang.org/protobuf/cmd/protoc-gen-go
go get -tool connectrpc.com/connect/cmd/protoc-gen-connect-go
go get -tool github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@v0.8.0
go get -tool github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@v0.8.0
```

Add them to `buf.gen.yaml` ([templates/buf.gen.yaml](../templates/buf.gen.yaml) is a complete one):

```yaml
plugins:
  - local: ["go", "tool", "protoc-gen-go"]
    out: gen/go
    opt: paths=source_relative
  - local: ["go", "tool", "protoc-gen-connect-go"]
    out: gen/go
    opt: paths=source_relative
  # MCP tool input and output schemas.
  - local: ["go", "tool", "protoc-gen-dedo-mcp"]
    out: gen/go
    opt: paths=source_relative
  # The OpenAPI 3.1 document for the REST reference.
  - local: ["go", "tool", "protoc-gen-dedo-openapi"]
    out: docs
    strategy: all
    opt:
      - title=Your API
      - server=https://api.example.com
```

If `buf.gen.yaml` uses managed mode, keep dedo's Go package and the modules that ship their own Go code out of it:

```yaml
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/you/your-service/gen/go
    # After your own override: the last match wins.
    - file_option: go_package_prefix
      module: buf.build/r-a/dedo
      value: github.com/raveesh-me/dedo/gen/go
  disable:
    - module: buf.build/googleapis/googleapis
    - module: buf.build/bufbuild/protovalidate
```

```sh
make api-generate   # buf generate
```

## 4. The server

Add the runtime libraries. Pin Vanguard to `v0.4.0`: later versions need connect-go v2.

```sh
go get github.com/raveesh-me/dedo@v0.8.0 github.com/raveesh-me/dedo/mcp@v0.8.0
go get connectrpc.com/vanguard@v0.4.0
```

Add the REST reference page; it serves the generated `openapi.json` at `/openapi.json` and a reference page at `/docs`:

```sh
mkdir -p docs
dedo templates docs/docs.go.tmpl > docs/docs.go
dedo templates docs/index.html > docs/index.html   # then set its <title>
```

Then wire everything in `main.go`. `dedo init` prints this with your names filled in; here `CounterService` from `acme.time_counter.v1`:

```go
func newHandler(log *slog.Logger) (http.Handler, error) {
	// The API: scopes and policy come from the .proto, not from handlers.
	table, err := policy.Build(time_counterv1connect.CounterServiceName)
	if err != nil {
		return nil, err
	}
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        scopesFromContext, // your auth middleware puts the caller's scopes in the context
		EnforceScopes: true,
		Log:           log,
	})
	if err != nil {
		return nil, err
	}
	path, rpc := time_counterv1connect.NewCounterServiceHandler(newServer(), connect.WithInterceptors(guard))
	rest, err := vanguard.NewTranscoder([]*vanguard.Service{vanguard.NewService(path, rpc)})
	if err != nil {
		return nil, err
	}
	api := flowid.Middleware(authMiddleware(rest))

	// MCP: every RPC is a tool, as protoc-gen-dedo-mcp generated it, called
	// through the same API with the caller's token.
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "time-counter", Version: "v1"}, nil)
	target := mcpserver.Target{Client: mcpserver.InProcessClient(api)}
	if err := mcpserver.AddTools(mcpServer, target, time_counterv1.CounterServiceMCPTools()...); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /openapi.json", docs.Spec())
	mux.Handle("GET /docs", docs.Page())
	// The MCP transport requires an Origin check.
	mux.Handle("/mcp", flowid.Middleware(http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))))
	mux.Handle("/", api)
	return mux, nil
}
```

The complete file, with `main`, imports and a development `authMiddleware`, is [templates/main.go.tmpl](../templates/main.go.tmpl) (`dedo init` fills it in). Add a policy-table test ([templates/policy_table_test.go.tmpl](../templates/policy_table_test.go.tmpl)) so a missing declaration fails `go test` before deploy.

```sh
go mod tidy
go run .
```

## 5. Guidance for coding agents

Append [claude.md](claude.md) to your `AGENTS.md` or `CLAUDE.md` (`dedo templates claude.md >> AGENTS.md`). `dedo init` does this where the file lacks it, and creates `AGENTS.md` when neither exists.

## Updating

- **dedo itself:** `go install github.com/raveesh-me/dedo/cmd/dedo@<new version>`, then `dedo init` again. It moves `DEDO_VERSION`, re-pins the generators and runtime, and replaces plugin entries earlier releases used.
- **The options module:** `buf dep update`.
