# Tutorial: a new API product from scratch

This page takes you from nothing to a running API, using a product called **time_counter** as the example. It counts time spent on tasks. At the end the same `.proto` serves RPC (Connect and gRPC), REST with reference docs, and MCP tools, linted in your editor and in CI.

Every step below was run as written. [install.md](install.md) lists the same setup as a checklist, for a repository that already exists. Read [guideline.md](guideline.md) for the reasons behind the steps, and [RULES.md](RULES.md) when a finding needs explaining.

## 0. Install once per machine

- Go 1.25 or later
- The [Buf CLI](https://buf.build/docs/installation)
- An editor with Buf's language server: VS Code with the Buf extension (`bufbuild.vscode-buf`), or Zed. Remove other Protobuf extensions.
- The `dedo` command:

  ```sh
  go install github.com/raveesh-me/dedo/cmd/dedo@v0.18.0
  ```

## 1. Decide the names

| Name | time_counter uses | Rule |
| --- | --- | --- |
| Repository and Go module | `github.com/acme/time-counter` | Your choice |
| Proto package | `acme.time_counter.v1` | `<org>.<product>.v<major>`, lowercase with underscores |
| REST root | `/commerce/timeCounter` | Your choice, but each segment is one lowercase or lowerCamelCase word: **no hyphens**. Every path is `<root>/v1/...`. |
| Scopes | `time_counter.counter.read`, `time_counter.counter.write` | `<app>.<resource>.<read\|write>` |
| Resource type | `time-counter.acme.com/Counter`, pattern `counters/{counter}` | `<service domain>/<Kind>` |

## 2. Create the repository

```sh
mkdir time-counter && cd time-counter && git init
go mod init github.com/acme/time-counter
```

Write the contract next, before running `dedo init`, so that `dedo init` sets the repository up around your service. (In an empty module, `dedo init` writes a starter service instead, which is the quickest way to try dedo.)

## 3. Write the contract

Create `proto/acme/time_counter/v1/time_counter.proto`:

```protobuf
syntax = "proto3";

package acme.time_counter.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/api/resource.proto";
import "google/protobuf/duration.proto";
import "google/protobuf/timestamp.proto";
import "dedo/v1/options.proto";

// CounterService counts time spent on tasks.
service CounterService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "time-counter-team"
  };

  // GetCounter returns one counter.
  rpc GetCounter(GetCounterRequest) returns (GetCounterResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
    option (google.api.http) = {
      get: "/commerce/timeCounter/v1/{name=counters/*}"
      response_body: "counter"
    };
    option (dedo.v1.method) = {
      scopes: "time_counter.counter.read"
      errors: {
        reason: "COUNTER_NOT_FOUND"
        code: NOT_FOUND
      }
      tool: {
        title: "Get a counter"
        description: "Returns one counter by its resource name, such as counters/1, with the time it has counted."
        annotations: {
          read_only_hint: true
          open_world_hint: false
        }
      }
    };
  }

  // CreateCounter starts a new counter at zero.
  rpc CreateCounter(CreateCounterRequest) returns (CreateCounterResponse) {
    option (google.api.http) = {
      post: "/commerce/timeCounter/v1/counters"
      body: "counter"
      response_body: "counter"
    };
    option (dedo.v1.method) = {
      scopes: "time_counter.counter.write"
      errors: {
        reason: "DISPLAY_NAME_REQUIRED"
        code: INVALID_ARGUMENT
      }
      tool: {
        title: "Create a counter"
        description: "Starts a new counter at zero for the task named in display_name."
        annotations: {
          read_only_hint: false
          destructive_hint: false
          idempotent_hint: false
          open_world_hint: false
        }
      }
    };
  }

  // AddTime adds time to a counter.
  rpc AddTime(AddTimeRequest) returns (AddTimeResponse) {
    option (google.api.http) = {
      post: "/commerce/timeCounter/v1/{name=counters/*}:addTime"
      body: "*"
      response_body: "counter"
    };
    option (dedo.v1.method) = {
      scopes: "time_counter.counter.write"
      errors: {
        reason: "COUNTER_NOT_FOUND"
        code: NOT_FOUND
      }
      tool: {
        title: "Add time to a counter"
        description: "Adds a duration, such as \"90s\", to a counter and returns its new total."
        annotations: {
          read_only_hint: false
          destructive_hint: false
          idempotent_hint: false
          open_world_hint: false
        }
      }
    };
  }
}

// Counter is the time counted for one task.
message Counter {
  option (google.api.resource) = {
    type: "time-counter.acme.com/Counter"
    pattern: "counters/{counter}"
    singular: "counter"
    plural: "counters"
  };
  option (dedo.v1.message) = {description: "The time counted for one task."};

  // The resource name, such as counters/1.
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // The task being timed.
  string display_name = 2 [(google.api.field_behavior) = REQUIRED];
  // The time counted so far.
  google.protobuf.Duration elapsed = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  // When the counter was created.
  google.protobuf.Timestamp create_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
}

// GetCounterRequest is the request of GetCounter.
message GetCounterRequest {
  option (dedo.v1.message) = {description: "Which counter to get."};

  // The resource name of the counter.
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "time-counter.acme.com/Counter"}
  ];
}

// GetCounterResponse is the response of GetCounter.
message GetCounterResponse {
  option (dedo.v1.message) = {description: "The requested counter."};

  // The counter.
  Counter counter = 1;
}

// CreateCounterRequest is the request of CreateCounter.
message CreateCounterRequest {
  option (dedo.v1.message) = {description: "The counter to create, and optionally its ID."};

  // The counter to create.
  Counter counter = 1 [(google.api.field_behavior) = REQUIRED];
  // The ID to give the counter. The server picks one when it is empty.
  string counter_id = 2 [(google.api.field_behavior) = OPTIONAL];
}

// CreateCounterResponse is the response of CreateCounter.
message CreateCounterResponse {
  option (dedo.v1.message) = {description: "The created counter."};

  // The counter.
  Counter counter = 1;
}

// AddTimeRequest is the request of AddTime.
message AddTimeRequest {
  option (dedo.v1.message) = {description: "Which counter to add time to, and how much."};

  // The resource name of the counter.
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "time-counter.acme.com/Counter"}
  ];
  // How much time to add.
  google.protobuf.Duration duration = 2 [(google.api.field_behavior) = REQUIRED];
}

// AddTimeResponse is the response of AddTime.
message AddTimeResponse {
  option (dedo.v1.message) = {description: "The counter with its new total."};

  // The counter.
  Counter counter = 1;
}
```

`tool` is how each RPC appears as an MCP tool. The model always sees `description`; `annotations` are the MCP specification's hints for the client. `AddTime` only adds time, so it is not destructive; a delete would be. [RULES.md](RULES.md#dedo_tool_annotations) explains how to set each hint.

Then set the repository up around it:

```sh
dedo init
```

`dedo init` finds `CounterService` and adds everything dedo needs, printing what it did:

- `buf.yaml` with the options module and the lint plugin, `rules.yml`, which chooses the plugin's rules that run, and `buf.gen.yaml` with the four generators
- the lint plugin, at the options module's release, copied into `.tools/` (`dedo tools` copies it again), and `Makefile.api` (included from `Makefile`), which runs the checks
- the generators pinned as Go tools in `go.mod`, the dedo runtime modules, and `buf.lock` (commit it)
- `docs/`, which serves the REST reference
- `server.go`, a stub where every RPC answers Unimplemented, and `policy_table_test.go`, which fails `go test` when a declaration is missing
- `AGENTS.md` with [the guidance for coding agents](claude.md)

Then it generates the code from the contract (step 4) and tidies `go.mod`, and reports what `buf lint` finds. It also writes `main.go`, which serves `CounterService` on every transport; step 5 explains it. Fill in `.github/CODEOWNERS` (from [templates/CODEOWNERS](../templates/CODEOWNERS)) so every change under `proto/` needs an API review, and copy [templates/vscode](../templates/vscode) to `.vscode/` if you use VS Code.

Now check the contract:

```sh
make check
```

It checks formatting, lints, and, once `main` has a contract, checks for breaking changes. It prints no findings when the contract is clean. Your editor shows the same findings as you type, each linking to its explanation.

## 4. Generate

`dedo init` already generated the code. After every later change to the contract, generate it again, which also tidies `go.mod`:

```sh
make generate
```

This writes:

- `gen/go/acme/time_counter/v1/` with the messages and `time_counter.mcp.pb.go` (`CounterServiceMCPTools`: every RPC as an MCP tool, with its input and output schemas, with field descriptions), and `time_counterv1connect/` with the RPC handlers and clients
- `docs/openapi.json`, the REST reference

Commit all of it. CI fails when it is stale.

## 5. Write the service

`server.go` holds the business logic, and nothing else. Replace the stub `dedo init` wrote with:

```go
package main

import (
	"context"
	"strconv"
	"sync"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	timecounterv1 "github.com/acme/time-counter/gen/go/acme/time_counter/v1"
)

// server holds business logic only. Scopes, headers, flow IDs and error
// audit come from the dedo runtime.
type server struct {
	mu       sync.Mutex
	counters map[string]*timecounterv1.Counter
	next     int
}

func newServer() *server { return &server{counters: map[string]*timecounterv1.Counter{}} }

func (s *server) GetCounter(_ context.Context, req *timecounterv1.GetCounterRequest) (*timecounterv1.GetCounterResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.counters[req.GetName()]
	if !ok {
		return nil, apiError(connect.CodeNotFound, "COUNTER_NOT_FOUND", "no counter named "+req.GetName())
	}
	return &timecounterv1.GetCounterResponse{Counter: proto.CloneOf(c)}, nil
}

func (s *server) CreateCounter(_ context.Context, req *timecounterv1.CreateCounterRequest) (*timecounterv1.CreateCounterResponse, error) {
	if req.GetCounter().GetDisplayName() == "" {
		return nil, apiError(connect.CodeInvalidArgument, "DISPLAY_NAME_REQUIRED", "display_name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := req.GetCounterId()
	if id == "" {
		s.next++
		id = strconv.Itoa(s.next)
	}
	c := &timecounterv1.Counter{
		Name:        "counters/" + id,
		DisplayName: req.GetCounter().GetDisplayName(),
		Elapsed:     durationpb.New(0),
		CreateTime:  timestamppb.Now(),
	}
	s.counters[c.GetName()] = c
	return &timecounterv1.CreateCounterResponse{Counter: proto.CloneOf(c)}, nil
}

func (s *server) AddTime(_ context.Context, req *timecounterv1.AddTimeRequest) (*timecounterv1.AddTimeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.counters[req.GetName()]
	if !ok {
		return nil, apiError(connect.CodeNotFound, "COUNTER_NOT_FOUND", "no counter named "+req.GetName())
	}
	next := proto.CloneOf(c)
	next.Elapsed = durationpb.New(c.GetElapsed().AsDuration() + req.GetDuration().AsDuration())
	s.counters[next.GetName()] = next
	return &timecounterv1.AddTimeResponse{Counter: proto.CloneOf(next)}, nil
}

// apiError returns an error carrying one of the RPC's declared reasons.
func apiError(code connect.Code, reason, message string) error {
	err := connect.NewError(code, message)
	if detail, derr := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: "time-counter.acme.com"}); derr == nil {
		err = err.WithDetail(detail)
	}
	return err
}
```

`main.go` is the one `dedo init` wrote in step 3. Running `dedo init` again keeps it, and prints dedo's template to compare with any changes you make.

`main.go` wires the API, the REST reference and MCP. Scopes and policy come from the `.proto`, so the handlers above contain no authorization code. `devTokens` holds the scopes declared in the `.proto`:

```go
// Command time-counter serves CounterService over Connect, gRPC, REST and MCP.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"
	"connectrpc.com/validate"
	"connectrpc.com/vanguard"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
	"github.com/raveesh-me/dedo/policy"

	"github.com/acme/time-counter/docs"
	time_counterv1 "github.com/acme/time-counter/gen/go/acme/time_counter/v1"
	"github.com/acme/time-counter/gen/go/acme/time_counter/v1/time_counterv1connect"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	handler, err := newHandler(log)
	if err != nil {
		log.Error("startup failed", "error", err)
		os.Exit(1)
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true) // gRPC clients without TLS
	log.Info("listening", "addr", addr)
	server := &http.Server{Addr: addr, Handler: handler, Protocols: protocols}
	if err := server.ListenAndServe(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func newHandler(log *slog.Logger) (http.Handler, error) {
	// The API: scopes and policy come from the .proto, not from handlers.
	table, err := policy.Build(time_counterv1connect.CounterServiceName)
	if err != nil {
		return nil, err
	}
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        scopesFromContext,
		EnforceScopes: true,
		Log:           log,
	})
	if err != nil {
		return nil, err
	}
	// The interceptors run for every transport below, in order: flow IDs,
	// auth, the declared contract (scopes, headers, error audit), then the
	// buf.validate rules.
	server := connect.NewServer(flowid.Interceptor(), auth, guard, validate.NewServerInterceptor())
	// newServer returns your implementation of time_counterv1connect.CounterServiceHandler.
	time_counterv1connect.RegisterCounterServiceHandler(server, newServer())

	mux := http.NewServeMux()
	// Connect, gRPC and gRPC-Web, then REST from the google.api.http bindings.
	connecthttp.Mount(mux, server)
	if err := vanguard.Mount(mux, server); err != nil {
		return nil, err
	}

	// MCP: every RPC is a tool, as protoc-gen-dedo-mcp generated it, called
	// in-process on the same server with the caller's token.
	tools := mcp.NewServer(&mcp.Implementation{Name: "time-counter", Version: "v1"}, nil)
	if err := mcpserver.AddTools(tools, connect.NewClient(connectinprocess.New(server)), time_counterv1.CounterServiceMCPTools()...); err != nil {
		return nil, err
	}

	// MCP_ACCESS=authenticated answers only callers whose token you accept,
	// tool discovery included; public, the default, lets anyone list the
	// tools. Tool calls are checked by the interceptors above either way.
	endpoint, err := mcpserver.Guard(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return tools },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}),
		mcpserver.Access(os.Getenv("MCP_ACCESS")), authenticated)
	if err != nil {
		return nil, err
	}

	mux.Handle("GET /openapi.json", docs.Spec())
	mux.Handle("GET /docs", docs.Page())
	// Cross-origin protection: the MCP transport requires an Origin check.
	mux.Handle("/mcp", flowid.Middleware(http.NewCrossOriginProtection().Handler(endpoint)))
	return mux, nil
}

// devTokens stands in for real token verification. Replace auth with your
// identity platform's token check before shipping.
var devTokens = map[string][]string{
	"reader": {"time_counter.counter.read"},
	"writer": {"time_counter.counter.read", "time_counter.counter.write"},
}

type scopesKey struct{}

// bearer returns the token of an Authorization header value.
func bearer(authorization string) string {
	token, _ := strings.CutPrefix(authorization, "Bearer ")
	return token
}

// authenticated reports whether a request carries a token you accept, for
// MCP_ACCESS=authenticated. Replace it with your token check, like auth.
func authenticated(r *http.Request) bool {
	_, ok := devTokens[bearer(r.Header.Get("Authorization"))]
	return ok
}

// auth puts the scopes of the caller's bearer token in the call's context.
// As a server interceptor it covers Connect, gRPC, REST and MCP alike.
func auth(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		var token string
		if info, ok := connect.CallInfoForServerContext(ctx); ok {
			token = bearer(info.RequestHeader().Get("Authorization"))
		}
		return next(context.WithValue(ctx, scopesKey{}, devTokens[token]), spec, stream)
	}
}

func scopesFromContext(ctx context.Context) []string {
	scopes, _ := ctx.Value(scopesKey{}).([]string)
	return scopes
}
```

Then run the tests, including the policy-table test:

```sh
make test
```

## 6. Run it and try every transport

```sh
make run                 # or ADDR=:9090 make run if port 8080 is taken
```

```sh
# REST
curl -H 'Authorization: Bearer writer' -H 'Content-Type: application/json' \
  -d '{"displayName":"Write install.md"}' localhost:8080/commerce/timeCounter/v1/counters
curl -H 'Authorization: Bearer writer' -H 'Content-Type: application/json' \
  -d '{"duration":"90s"}' localhost:8080/commerce/timeCounter/v1/counters/1:addTime
curl -i -H 'Authorization: Bearer reader' localhost:8080/commerce/timeCounter/v1/counters/1

# The reader token cannot write: 403
curl -o /dev/null -w '%{http_code}\n' -H 'Authorization: Bearer reader' -H 'Content-Type: application/json' \
  -d '{"displayName":"x"}' localhost:8080/commerce/timeCounter/v1/counters

# MCP: tools are named after the RPCs (get_counter, create_counter, add_time)
curl -H 'Authorization: Bearer reader' -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' localhost:8080/mcp
```

Open <http://localhost:8080/docs> for the REST reference. Point an MCP client at `http://localhost:8080/mcp` with an `Authorization: Bearer reader` header.

The same RPCs also answer over Connect and gRPC at `/acme.time_counter.v1.CounterService/<Rpc>`. Use the generated client in `time_counterv1connect`.

## 7. Before you ship

- Replace `auth` and `devTokens` in `main.go` with your identity platform's token verification, as a server interceptor. It must put the caller's scopes in the call's context.
- Commit to `main`, then run `make check`. It also runs `buf breaking` against `main`.
- Add the CI stage from [templates/Jenkinsfile.api-stage](../templates/Jenkinsfile.api-stage), or the same commands in your CI.
- Check that the teams in `.github/CODEOWNERS` exist. Every change under `proto/` then needs an API review.

## Updating later

- **A new dedo release:** `dedo update`, then `dedo upgrade`. The options module, the lint plugin, the generators and the runtime move together, and the code is regenerated. [install.md](install.md#upgrading) lists what changes.
- **A new RPC:** add it to the `.proto`, then run `make generate` and `make check`, and implement the method in `server.go`. The REST route, the docs and the MCP tool follow from the `.proto`.
- **Your organisation's own rules:** write them as CEL checks in `rules.yml` next to `buf.yaml`, or import your organisation's shared file from git, then run `dedo tools` (`make tools`), which fetches the imports and checks them; `buf lint` then reports them like any other rule. Run it again after every edit to `rules.yml` or its imports. A house rule outranks dedo's: to replace one, name it in the rule's `overrides`, and `dedo tools` lists every override in force. [RULES.md](RULES.md#house-rules) has the format.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `does not follow proper HTTP path template syntax` | A hyphen in the REST root, such as `/commerce/time-counter`. Use `/commerce/timeCounter`. |
| `Fields representing timestamps should use google.protobuf.Timestamp` on a field like `seconds` | AIP reads `seconds` as time. Use `google.protobuf.Duration` for spans of time and `Timestamp` for points in time. |
| `undefined: ...connect.New...ServiceHandler`, or `connect.Request` in errors | Code written for connect-go v1. `dedo upgrade` runs connect-go's migration tool; then wire `main.go` as dedo's template does. |
| `go: updates to go.mod needed` while generating | Run `make generate` again: it ends with `go mod tidy`. |
| `buf breaking: skipped; branch main has no .proto files yet` | Nothing to compare with yet: commit the contract to `main`. If your default branch has another name, run `make check API_BASE=<branch>`. |
| `bind: address already in use` | Something else holds port 8080. Run with `ADDR=:9090`. |
| No findings in the editor | The lint plugin is missing from `.tools/`, as after a fresh clone. Run `dedo tools` (or `make tools`), then reload the editor. |
| `reading github.com/raveesh-me/dedo/go.mod at revision v0.0.0: unknown revision` | `go get` picked an old commit instead of a release. Add `@v0.18.0` (or the latest release) to the module path. |
| `Module buf.build/bufbuild/protovalidate is declared ... but is unused` | A warning only. Remove the dependency from `buf.yaml` if you don't use protovalidate. |
