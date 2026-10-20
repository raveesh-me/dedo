# Start a new API product

This page takes you from nothing to a running API, using a product called **time_counter** as the example. It counts time spent on tasks. At the end the same `.proto` serves RPC (Connect and gRPC), REST with reference docs, and MCP tools, linted in your editor and in CI.

Every step below was run as written. Read [guideline.md](guideline.md) for the reasons behind the steps, and [RULES.md](RULES.md) when a finding needs explaining.

## 0. Install once per machine

- Go 1.25 or later
- The [Buf CLI](https://buf.build/docs/installation)
- An editor with Buf's language server: VS Code with the Buf extension (`bufbuild.vscode-buf`), or Zed. Remove other Protobuf extensions.

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
# The templates of the release you build against; step 5 uses them once
# more, then you delete them.
DEDO_VERSION=v0.6.0
curl -fsSL https://github.com/raveesh-me/dedo/releases/download/$DEDO_VERSION/dedo-templates.tar.gz | tar xz
DEDO=dedo-templates
cp $DEDO/buf.yaml $DEDO/buf.gen.yaml $DEDO/Makefile.api .
mkdir -p .github .vscode docs proto/acme/time_counter/v1
cp $DEDO/CODEOWNERS .github/
cp $DEDO/vscode/*.json .vscode/
cp $DEDO/docs/docs.go $DEDO/docs/index.html docs/
cat $DEDO/claude.md >> AGENTS.md   # guidance for coding agents
printf 'include Makefile.api\n' > Makefile
printf '.tools/\ndedo-templates/\n' > .gitignore
```

Replace the placeholders:

- `buf.gen.yaml`: `YOUR_ORG/YOUR_REPO` with `acme/time-counter`, `YOUR_API_TITLE` with `Time Counter API`, `https://YOUR_HOST` with your API's base URL.
- `.github/CODEOWNERS`: `@YOUR_ORG/YOUR_TEAM` and `@YOUR_ORG/api-reviewers` with your teams.
- `docs/index.html`: the page title.

Pin the code generators, so every machine and CI generate the same code:

```sh
go get -tool google.golang.org/protobuf/cmd/protoc-gen-go
go get -tool connectrpc.com/connect/cmd/protoc-gen-connect-go
go get -tool github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@v0.6.0
go get -tool github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@v0.6.0
```

Give the dedo modules an explicit version, the latest [release](https://github.com/raveesh-me/dedo/releases). Right after a release, Go's module proxy can still report an older version as the latest.

Download the lint plugins for your machine into `.tools/`. Do this once per clone; `make api-check` also does it.

```sh
make tools
```

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

Then resolve the dependencies and lint:

```sh
buf dep update      # writes buf.lock; commit it
buf format -w
buf lint
```

`buf lint` prints nothing when the contract is clean. Your editor shows the same findings as you type, each linking to its explanation.

## 4. Generate

```sh
buf generate
go mod tidy
```

This writes:

- `gen/go/acme/time_counter/v1/` with the messages and `time_counter.mcp_schema.pb.go` (MCP tool input and output schemas, with field descriptions), and `time_counterv1connect/` with the RPC handlers and clients
- `docs/openapi.json`, the REST reference

Commit all of it. CI fails when it is stale.

## 5. Write the service

`server.go` holds the business logic, and nothing else:

```go
package main

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"connectrpc.com/connect"
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

func (s *server) GetCounter(_ context.Context, req *connect.Request[timecounterv1.GetCounterRequest]) (*connect.Response[timecounterv1.GetCounterResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.counters[req.Msg.GetName()]
	if !ok {
		return nil, apiError(connect.CodeNotFound, "COUNTER_NOT_FOUND", "no counter named "+req.Msg.GetName())
	}
	return connect.NewResponse(&timecounterv1.GetCounterResponse{Counter: proto.CloneOf(c)}), nil
}

func (s *server) CreateCounter(_ context.Context, req *connect.Request[timecounterv1.CreateCounterRequest]) (*connect.Response[timecounterv1.CreateCounterResponse], error) {
	if req.Msg.GetCounter().GetDisplayName() == "" {
		return nil, apiError(connect.CodeInvalidArgument, "DISPLAY_NAME_REQUIRED", "display_name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := req.Msg.GetCounterId()
	if id == "" {
		s.next++
		id = strconv.Itoa(s.next)
	}
	c := &timecounterv1.Counter{
		Name:        "counters/" + id,
		DisplayName: req.Msg.GetCounter().GetDisplayName(),
		Elapsed:     durationpb.New(0),
		CreateTime:  timestamppb.Now(),
	}
	s.counters[c.GetName()] = c
	return connect.NewResponse(&timecounterv1.CreateCounterResponse{Counter: proto.CloneOf(c)}), nil
}

func (s *server) AddTime(_ context.Context, req *connect.Request[timecounterv1.AddTimeRequest]) (*connect.Response[timecounterv1.AddTimeResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.counters[req.Msg.GetName()]
	if !ok {
		return nil, apiError(connect.CodeNotFound, "COUNTER_NOT_FOUND", "no counter named "+req.Msg.GetName())
	}
	next := proto.CloneOf(c)
	next.Elapsed = durationpb.New(c.GetElapsed().AsDuration() + req.Msg.GetDuration().AsDuration())
	s.counters[next.GetName()] = next
	return connect.NewResponse(&timecounterv1.AddTimeResponse{Counter: proto.CloneOf(next)}), nil
}

// apiError returns an error carrying one of the RPC's declared reasons.
func apiError(code connect.Code, reason, message string) error {
	err := connect.NewError(code, errors.New(message))
	if detail, derr := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: "time-counter.acme.com"}); derr == nil {
		err.AddDetail(detail)
	}
	return err
}
```

`main.go` wires the API, the REST reference and MCP. Scopes and policy come from the `.proto`, so the handlers above contain no authorization code:

```go
// Command time-counter serves CounterService over Connect, gRPC, REST and MCP.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/vanguard"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
	"github.com/raveesh-me/dedo/policy"

	"github.com/acme/time-counter/docs"
	timecounterv1 "github.com/acme/time-counter/gen/go/acme/time_counter/v1"
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
	log.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
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
	path, rpc := time_counterv1connect.NewCounterServiceHandler(newServer(), connect.WithInterceptors(guard))
	rest, err := vanguard.NewTranscoder([]*vanguard.Service{vanguard.NewService(path, rpc)})
	if err != nil {
		return nil, err
	}
	api := flowid.Middleware(authMiddleware(rest))

	// MCP: every RPC is a tool, called through the same API with the caller's
	// token, using the schemas protoc-gen-dedo-mcp generated.
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "time-counter", Version: "v1"}, nil)
	if err := mcpserver.New(mcpServer, mcpserver.Config{
		Services:      []protoreflect.ServiceDescriptor{timecounterv1.File_acme_time_counter_v1_time_counter_proto.Services().ByName("CounterService")},
		InputSchemas:  timecounterv1.MCPInputSchemas,
		OutputSchemas: timecounterv1.MCPOutputSchemas,
		Client:        mcpserver.InProcessClient(api),
	}); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /openapi.json", docs.Spec())
	mux.Handle("GET /docs", docs.Page())
	// Cross-origin protection: the MCP transport requires an Origin check.
	mux.Handle("/mcp", flowid.Middleware(http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))))
	mux.Handle("/", api)
	return mux, nil
}

// devTokens stands in for real token verification. Replace authMiddleware
// with your identity platform's middleware before shipping.
var devTokens = map[string][]string{
	"reader": {"time_counter.counter.read"},
	"writer": {"time_counter.counter.read", "time_counter.counter.write"},
}

type scopesKey struct{}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopesKey{}, devTokens[token])))
	})
}

func scopesFromContext(ctx context.Context) []string {
	scopes, _ := ctx.Value(scopesKey{}).([]string)
	return scopes
}
```

Add the runtime libraries, and pin Vanguard to `v0.4.0`. Later versions need connect-go v2. Pin it after the code exists, or `go mod tidy` replaces it with the latest.

```sh
go get github.com/raveesh-me/dedo@v0.6.0 github.com/raveesh-me/dedo/mcp@v0.6.0
go get connectrpc.com/vanguard@v0.4.0
go mod tidy
```

Add the policy-table test, so a missing declaration fails before deploy:

```sh
sed -e 's#libraryv1connect "github.com/YOUR_ORG/YOUR_REPO/gen/go/raveesh_me/library/v1/libraryv1connect"#"github.com/acme/time-counter/gen/go/acme/time_counter/v1/time_counterv1connect"#' \
    -e 's#libraryv1connect.LibraryServiceName#time_counterv1connect.CounterServiceName#' \
    $DEDO/policy_table_test.go.tmpl > policy_table_test.go
gofmt -w policy_table_test.go
go test ./...
rm -rf dedo-templates
```

## 6. Run it and try every transport

```sh
go run .                 # or ADDR=:9090 go run . if port 8080 is taken
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

- Replace `authMiddleware` and `devTokens` in `main.go` with your identity platform's token verification. It must put the caller's scopes in the request context.
- Commit to `main`, then run `make api-check`. It also runs `buf breaking` against `main`.
- Add the CI stage from `Jenkinsfile.api-stage` in the release's `dedo-templates.tar.gz`, or the same commands in your CI.
- Check that the teams in `.github/CODEOWNERS` exist. Every change under `proto/` then needs an API review.

## Updating later

- **New lint rules:** bump `DEDO_VERSION` in `Makefile.api`, then run `make tools`.
- **New options module:** run `buf dep update`.
- **A new RPC:** add it to the `.proto`, then run `buf lint`, `buf generate` and `go mod tidy`, and implement the method in `server.go`. The REST route, the docs and the MCP tool follow from the `.proto`.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `does not follow proper HTTP path template syntax` | A hyphen in the REST root, such as `/commerce/time-counter`. Use `/commerce/timeCounter`. |
| `Fields representing timestamps should use google.protobuf.Timestamp` on a field like `seconds` | AIP reads `seconds` as time. Use `google.protobuf.Duration` for spans of time and `Timestamp` for points in time. |
| `undefined: vanguard.NewTranscoder` | Vanguard was upgraded to a version for connect-go v2. Run `go get connectrpc.com/vanguard@v0.4.0`. |
| `go: updates to go.mod needed` while generating | Run `go mod tidy`, then generate again. |
| `bind: address already in use` | Something else holds port 8080. Run with `ADDR=:9090`. |
| No findings in the editor | The lint plugins are missing from `.tools/`. Run `make tools`, then reload the editor. |
| `reading github.com/raveesh-me/dedo/go.mod at revision v0.0.0: unknown revision` | `go get` picked an old commit instead of a release. Add `@v0.6.0` (or the latest release) to the module path. |
| `Module buf.build/bufbuild/protovalidate is declared ... but is unused` | A warning only. Remove the dependency from `buf.yaml` if you don't use protovalidate. |
