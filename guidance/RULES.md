# Lint rules

`buf-plugin-dedo-lint` is one Buf check plugin with three classes of rules. Each class is a lint category named after its rule prefix:

| Class | Source | What it checks |
| --- | --- | --- |
| `AIP_*` | [Google's AIPs](https://google.aip.dev), through [api-linter](https://github.com/googleapis/api-linter) | Resource-oriented design: names, standard methods, HTTP bindings, field behavior, pagination |
| `ZALANDO_*` | [Zalando's RESTful API guidelines](https://opensource.zalando.com/restful-api-guidelines/) | API ownership, audience, OAuth scope names, caching, deprecation, money |
| `DEDO_*` | dedo itself | What dedo needs to serve one proto as RPC, REST, docs and MCP tools |

A fourth prefix, `HOUSE_*`, is reserved for your organisation's own rules, written as CEL checks in `rules.yml` (see [House rules](#house-rules)). dedo never ships a `HOUSE_*` rule, so yours never clash with an upgrade.

```yaml
lint:
  use:
    - AIP_NOLANG   # every AIP rule except the language-specific ones
    - DEDO
    - ZALANDO
```

`AIP_NOLANG` can be narrowed to `AIP_CORE` or widened to `AIP`. Single AIPs are categories too (`AIP_0131`). To pass an api-linter configuration file, add `options: {config: path/to/api-linter.yaml}` to the plugin entry.

Every `ZALANDO_*` and `DEDO_*` finding links to its section below. A release of the plugin links to this file as it was at that release's tag, so the text always matches the rule that reported it. Development builds link to `main`. `AIP_*` findings link to the AIP's rule page on [linter.aip.dev](https://linter.aip.dev).

To suppress one finding, put `// buf:lint:ignore <RULE_ID>` above the element, followed by the reason. Reviewers treat each suppression as a decision to justify.

## Precedence

When two sources disagree, the higher one wins:

1. **DEDO**
2. **AIP**
3. **ZALANDO**

dedo comes first because its rules are what make one proto serve every transport. The AIPs come next because the contract is a protobuf API, which they were written for. Zalando's guidelines were written for hand-designed JSON-over-HTTP APIs; dedo adopts the ones that still apply.

The known conflicts, and how they resolve:

| Topic | Losing side | Winning side |
| --- | --- | --- |
| Response messages | AIP: Get, Create and Update return the resource itself; Delete returns `Empty` | DEDO: every RPC returns its own `<Rpc>Response`, so fields can be added later. REST unwraps it with `response_body` ([`DEDO_HTTP_RESPONSE_BODY`](#dedo_http_response_body)). The AIP rules that disagree are skipped (below). |
| `method_signature` | AIP: every standard method declares one | DEDO: Connect ignores it, so it is optional and its rules are skipped |
| API version | ZALANDO: no version in the URL; use media-type versioning | AIP and DEDO: the major version is in the package and the path ([`DEDO_HTTP_PATH_VERSION`](#dedo_http_path_version)) |
| JSON field names | ZALANDO: `snake_case` | AIP and DEDO: proto3 JSON's `lowerCamelCase`, never renamed ([`DEDO_NO_JSON_NAME`](#dedo_no_json_name)) |
| Path segments | ZALANDO: `kebab-case` | AIP: `lowerCamelCase` (AIP-122, AIP-127) |
| Error format | ZALANDO: Problem JSON (RFC 9457) | AIP: `google.rpc.Status` with `ErrorInfo` (AIP-193); DEDO checks the declared reasons ([`DEDO_ERROR_REASON_FORMAT`](#dedo_error_reason_format)) |
| Pagination | ZALANDO: cursors in a `next`/`prev` links object | AIP: `page_token` and `next_page_token` (AIP-158) |

When you write a new rule, check this table first. A rule of a lower source that contradicts a higher one is a bug.

## Skipped rules

dedo turns these off on purpose. `templates/buf.yaml` carries the AIP exclusions, so a service inherits them.

**AIP**

| Rules | Why |
| --- | --- |
| `AIP_0131_RESPONSE_MESSAGE_NAME` … `AIP_0135_RESPONSE_MESSAGE_NAME` | Every RPC returns its own `<Rpc>Response` (see Precedence) |
| `AIP_0121_RESOURCE_MUST_SUPPORT_GET`, `AIP_0134_REQUEST_REQUIRED_FIELDS` | The same wrapping hides the resource from api-linter's standard-method detection, so these report false positives |
| `AIP_0131_METHOD_SIGNATURE` … `AIP_0135_METHOD_SIGNATURE` | `method_signature` drives Google's client generators; Connect ignores it |
| `AIP_0192_HAS_COMMENTS` | Buf's `COMMENTS` category reports the same thing |
| Language-specific rules (`AIP_CLIENT_LIBRARIES`) | `AIP_NOLANG` leaves them out; dedo's clients come from Buf's generators |

**Zalando**

dedo checks only the Zalando guidelines a proto can express. It does not follow these:

| Guideline | Why |
| --- | --- |
| No versioning in URLs | AIP wins (see Precedence) |
| `snake_case` JSON property names | AIP and DEDO win |
| `kebab-case` path segments | AIP wins |
| Problem JSON errors | AIP wins |
| Pagination links objects | AIP wins |
| Hypermedia (HAL and other link formats) | Clients are generated from the proto, not discovered at run time |
| `PATCH` with JSON Merge Patch | Partial updates use `FieldMask` (AIP-134) |
| Conditional requests (`ETag`, `If-Match`) as headers | AIP-154 puts `etag` in the resource instead, when an API needs it |

## House rules

`HOUSE_*` rules are your organisation's own, for conventions dedo has no business knowing: your domain names, your REST roots, your field naming. You write them in `rules.yml` next to `buf.yaml`, as [CEL](https://cel.dev) checks, and `buf-plugin-dedo-lint` runs them with dedo's own rules. No plugin to write, build or release.

```yaml
# rules.yml
imports:
  - git: https://github.com/acme/api-standards.git   # your organisation's shared rules
    ref: v3                                           # a tag or a commit
    path: rules.yml                                   # the default
  - path: ../platform-rules                           # a folder (its rules.yml) or a file
disable: [HOUSE_PLURAL_COLLECTIONS]                   # imported rules this repository opts out of
rules:
  - id: HOUSE_REST_ROOT
    on: method
    check: http_path == '' || http_path.startsWith('/commerce/')
    message: REST paths start with /commerce/, the commerce platform's root.
    doc: https://wiki.acme.com/api/rest-roots
```

Add `HOUSE` to `lint.use` in `buf.yaml` (`dedo init` does it when `rules.yml` exists). Each rule is a Buf rule of its own, so `except: [HOUSE_REST_ROOT]` and `// buf:lint:ignore HOUSE_REST_ROOT` work as for any other rule, and a finding shows the rule's `message` and `doc` link.

**Shared rules.** An organisation keeps its rules in one repository and tags it; each service imports it with `git` and `ref`. Imports nest: a shared file can import others. `dedo tools` (and `make tools`, `dedo init`, `dedo upgrade`) fetches git imports into `.tools/rules/`; the lint itself never touches the network, so it stays fast in the editor. A missing import fails the lint with "run dedo tools". `disable` lets a service opt out of an imported rule, by ID.

**Checks.** `check` is a CEL expression that is true when the element passes. `on` picks the element, and these are the names a check can use:

| `on` | Variables |
| --- | --- |
| `file` | `name` (the path), `proto_package`, `imports` |
| `service` | `name`, `full_name`, `file`, `proto_package`, `methods` |
| `method` | `name`, `full_name`, `file`, `proto_package`, `service`, `input`, `output`, `client_streaming`, `server_streaming`, `idempotency` (`NO_SIDE_EFFECTS`, `IDEMPOTENT`, `UNKNOWN`), `http_verb`, `http_path`, `scopes`, `errors` (reasons), `deprecated` |
| `message` | `name`, `full_name`, `file`, `proto_package`, `fields` |
| `field` | `name`, `full_name`, `file`, `proto_package`, `message`, `type` (a scalar kind such as `string`, or a message or enum full name), `repeated`, `optional`, `behaviors` (such as `REQUIRED`), `json_name` |
| `enum` | `name`, `full_name`, `file`, `proto_package`, `values` |
| `resource` | `name`, `full_name`, `file`, `proto_package`, `type`, `patterns`, `singular`, `plural` |

Lists are lists of strings (`'shop.cart.read' in scopes`, `fields.size() < 30`); booleans are booleans. CEL's string extensions are on (`split`, `lowerAscii`, `replace`, `trim`). A check that does not compile, or is not a bool, stops the lint and names the rule and its file.

House rules run in the native plugin, which reads `rules.yml`; the WebAssembly plugin on the BSR cannot read files, so a repository without Go does not run them. [`examples/house-rules`](../examples/house-rules) holds shared rules that both todo examples import, each adding one of its own: `examples/todo` follows them and `examples/bad-todo` breaks them.

House rules rank below everything above: they may add constraints, but not contradict a DEDO, AIP or ZALANDO rule. A contract that lints clean under all of them is the proof they agree; keep one (your service, or dedo's starter) in CI.

## dedo's rules

| Rule | Checks |
| --- | --- |
| [`ZALANDO_SERVICE_RULES_REQUIRED`](#zalando_service_rules_required) | Services declare an audience and an owner team |
| [`DEDO_METHOD_RULES_REQUIRED`](#dedo_method_rules_required) | RPCs declare scopes and error reasons |
| [`ZALANDO_SCOPE_NAME_FORMAT`](#zalando_scope_name_format) | Scopes are `<app>.<resource>.<read\|write>` |
| [`DEDO_ERROR_REASON_FORMAT`](#dedo_error_reason_format) | Error reasons are UPPER_SNAKE_CASE with a meaningful code |
| [`ZALANDO_CACHE_REQUIRES_NO_SIDE_EFFECTS`](#zalando_cache_requires_no_side_effects) | Only read-only RPCs are cached, with a visibility and a max age |
| [`ZALANDO_DEPRECATION_CONSISTENT`](#zalando_deprecation_consistent) | Deprecations have a complete, ordered schedule and a real successor |
| [`DEDO_READ_METHOD_NO_SIDE_EFFECTS`](#dedo_read_method_no_side_effects) | Get and List RPCs are marked `NO_SIDE_EFFECTS` |
| [`DEDO_HTTP_RESPONSE_BODY`](#dedo_http_response_body) | Get, Create and Update return the bare resource over REST |
| [`DEDO_HTTP_PATH_VERSION`](#dedo_http_path_version) | REST paths carry the version after one root per service |
| [`DEDO_NO_JSON_NAME`](#dedo_no_json_name) | No custom JSON field names |
| [`ZALANDO_MONEY_FIELD_TYPE`](#zalando_money_field_type) | Money fields use `google.type.Money` |
| [`DEDO_TOOL_REQUIRED`](#dedo_tool_required) | RPCs declare their MCP tool and REST docs text |
| [`DEDO_TOOL_ANNOTATIONS`](#dedo_tool_annotations) | RPCs declare the MCP ToolAnnotations hints, consistent with the proto |
| [`DEDO_MESSAGE_DESCRIPTION_REQUIRED`](#dedo_message_description_required) | Messages declare a consumer-facing description |

---

### ZALANDO_SERVICE_RULES_REQUIRED

**Checks** that every service sets `dedo.v1.service` with an `audience` other than `AUDIENCE_UNSPECIFIED` and a non-empty `owner_team`.

**Why.** Every API has an owner and an intended audience. The REST docs publish both, and the runtime refuses to start a service without the option.

```protobuf
// Fails: no service option.
service ProductService {}

// Passes.
service ProductService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "pim-team"
  };
}
```

**Fix.** Add the option. Use the team's name in the team registry.

### DEDO_METHOD_RULES_REQUIRED

**Checks** that every RPC sets `dedo.v1.method` with at least one scope and at least one error reason.

**Why.** Scopes are the RPC's authorization: without one, the endpoint would be open. The runtime fails closed and will not start. Error reasons are the RPC's error contract: the docs list them, and the runtime logs any reason the RPC returns without declaring it.

```protobuf
// Fails: no scopes, no errors.
rpc GetProduct(GetProductRequest) returns (GetProductResponse);

// Passes.
rpc GetProduct(GetProductRequest) returns (GetProductResponse) {
  option (dedo.v1.method) = {
    scopes: "pim.product.read"
    errors: {reason: "PRODUCT_NOT_FOUND", code: NOT_FOUND}
  };
}
```

**Fix.** Declare every scope that grants access (a caller needs one) and every error reason the handler returns.

### ZALANDO_SCOPE_NAME_FORMAT

**Checks** that every scope matches `<app>.<resource>.<read|write>`, in lowercase with optional underscores, for example `pim.product.read`.

**Why.** Scopes appear in tokens issued by the identity platform. One format lets a reviewer and the identity platform tell what a scope grants.

```protobuf
scopes: "pim.read"            // Fails: no resource.
scopes: "pim.product.delete"  // Fails: the verb is read or write.
scopes: "pim.product.write"   // Passes.
```

**Fix.** Rename the scope. Deletes, updates and creates are all `write`.

### DEDO_ERROR_REASON_FORMAT

**Checks** that every declared error reason is UPPER_SNAKE_CASE, and that its code is neither `OK` nor `UNKNOWN`.

**Why.** The reason is the machine-readable part of an error (`google.rpc.ErrorInfo.reason`). Clients branch on it, so it must be stable and readable. `OK` is not an error, and `UNKNOWN` tells the client nothing.

```protobuf
errors: {reason: "product_not_found", code: NOT_FOUND}  // Fails: case.
errors: {reason: "PRODUCT_NOT_FOUND", code: UNKNOWN}    // Fails: code.
errors: {reason: "PRODUCT_NOT_FOUND", code: NOT_FOUND}  // Passes.
```

**Fix.** Rename the reason, or pick the code that describes the failure. `examples/todo/app/errors.go` shows how to return the reason in an `ErrorInfo`.

### ZALANDO_CACHE_REQUIRES_NO_SIDE_EFFECTS

**Checks** that a `cache` policy appears only on RPCs with `idempotency_level = NO_SIDE_EFFECTS`, and that it sets a `visibility` and a positive `max_age`.

**Why.** The runtime sets `Cache-Control` from the policy. Caching a response that changes state is a bug, and a policy without visibility or max age has no meaning.

```protobuf
// Fails: CreateProduct changes state.
rpc CreateProduct(CreateProductRequest) returns (CreateProductResponse) {
  option (dedo.v1.method) = {
    cache: {visibility: VISIBILITY_PUBLIC, max_age: {seconds: 60}}
  };
}

// Passes.
rpc GetProduct(GetProductRequest) returns (GetProductResponse) {
  option idempotency_level = NO_SIDE_EFFECTS;
  option (dedo.v1.method) = {
    cache: {visibility: VISIBILITY_PRIVATE, max_age: {seconds: 30}}
  };
}
```

**Fix.** Remove the cache policy from RPCs that change state. Otherwise complete it.

### ZALANDO_DEPRECATION_CONSISTENT

**Checks** that `deprecated = true` and a `deprecation` schedule appear together; that the schedule has both times, with the sunset after the deprecation; and that the `successor` names an RPC that exists.

**Why.** The runtime sends `Deprecation` and `Sunset` headers from the schedule, and the docs point callers to the successor. A half-declared deprecation sends wrong dates or a dead link.

```protobuf
// Passes.
rpc RemoveProduct(RemoveProductRequest) returns (RemoveProductResponse) {
  option deprecated = true;
  option (dedo.v1.method) = {
    deprecation: {
      deprecation_time: {seconds: 1767225600}
      sunset_time: {seconds: 1798761600}
      successor: "acme.pim.v1.ProductService.DeleteProduct"
    }
  };
}
```

**Fix.** Set both, with the sunset after the deprecation. Give the successor's fully qualified name: `<package>.<Service>.<Rpc>`.

### DEDO_READ_METHOD_NO_SIDE_EFFECTS

**Checks** that unary RPCs named `Get*` or `List*` (followed by an upper-case letter) set `idempotency_level = NO_SIDE_EFFECTS`.

**Why.** The level tells Connect to allow HTTP GET, which makes the response cacheable. It also tells MCP clients the tool is read-only, and lets the runtime set cache headers.

```protobuf
rpc ListProducts(ListProductsRequest) returns (ListProductsResponse);  // Fails.

rpc ListProducts(ListProductsRequest) returns (ListProductsResponse) {  // Passes.
  option idempotency_level = NO_SIDE_EFFECTS;
}
```

**Fix.** Add the option. If the RPC does change state, rename it.

### DEDO_HTTP_RESPONSE_BODY

**Checks** that the HTTP rule of a `Get*`, `Create*` or `Update*` RPC sets `response_body`.

**Why.** Every RPC returns a unique `<Rpc>Response` message, which Buf requires. REST callers expect the resource itself, not a wrapper around it. `response_body` unwraps it.

```protobuf
// Passes: REST callers receive the Product.
option (google.api.http) = {
  get: "/commerce/pim/v1/{name=products/*}"
  response_body: "product"
};
```

**Fix.** Set `response_body` to the field holding the resource.

### DEDO_HTTP_PATH_VERSION

**Checks** that every HTTP path, additional bindings included, contains the package's version as a segment, such as `/v1/`. Everything before the version (the root) must be fixed text, and the same for every RPC of the service.

**Why.** The version in the path follows the proto package: a breaking change is a new package and a new path. The root, such as `/commerce/pim` or `/commerce/products`, is the owning platform's choice, and the rule does not judge it. It only keeps one service from spreading over several roots.

```protobuf
get: "/{name=products/*}"                     // Fails: no version.
get: "/commerce/{tenant}/v1/products"         // Fails: variable in the root.
get: "/commerce/pim/v1/{name=products/*}"     // Passes.
delete: "/commerce/products/v1/{name=products/*}"  // Fails in the same service: different root.
```

**Fix.** Put `/<version>/` after the root, and use the same root for every RPC of the service. Write each root segment as one lowercase or lowerCamelCase word, such as `/commerce/timeCounter`: AIP-127's path syntax check rejects hyphens.

### DEDO_NO_JSON_NAME

**Checks** that no field changes its JSON name with `json_name`.

**Why.** JSON uses the protojson default, lowerCamelCase of the field name. A custom name renames the field for every JSON client of REST and Connect, away from the name in the schema. Every server and client would then need a codec option.

```protobuf
string title = 1 [json_name = "todoTitle"];  // Fails.
string title = 1;                            // Passes: JSON name "title".
```

**Fix.** Remove `json_name`. To change the name, rename the field.

### ZALANDO_MONEY_FIELD_TYPE

**Checks** that fields named `price` or `amount`, or ending in `_price` or `_amount`, use `google.type.Money`.

**Why.** Floating-point money loses cents, and a bare number has no currency.

```protobuf
double price = 1;             // Fails.
google.type.Money price = 1;  // Passes.
```

**Fix.** Use `google.type.Money`. If the field is not money, rename it.

### DEDO_TOOL_REQUIRED

**Checks** that every unary RPC sets `tool` in its method option, with a `title` and a `description` that does more than repeat the title or the RPC name. Streaming RPCs are exempt, because MCP cannot expose them.

**Why.** Every RPC is published as an MCP tool. These are the MCP specification's `Tool.title` and `Tool.description`. The description is the one field every client passes to the model: hints and the title may never reach it. They are also the operation's summary and description in the REST docs.

```protobuf
// Passes.
option (dedo.v1.method) = {
  tool: {
    title: "Delete a product"
    description: "Removes a product and its variants for good. It cannot be undone."
  }
};
```

**Fix.** Write the title as a short action ("Get a product"). Write the description for someone choosing between operations: what it does, when to use it, and any consequence the caller must know, such as "cannot be undone", "sends an email" or "charges the card".

### DEDO_TOOL_ANNOTATIONS

**Checks** the MCP `ToolAnnotations` of every unary RPC, against the MCP specification (2026-07-28) and against what the proto already says. Only fields the specification defines are checked:

- `read_only_hint` and `open_world_hint` are always declared.
- `read_only_hint` is true exactly when `idempotency_level = NO_SIDE_EFFECTS`.
- `destructive_hint` and `idempotent_hint` are declared exactly when the tool is not read-only. The specification says they mean nothing otherwise.
- `idempotent_hint` is true exactly when `idempotency_level = IDEMPOTENT`.
- RPCs named `Delete*` or `Remove*` set `destructive_hint: true`.

**Why.** MCP clients use the hints to decide, for example, whether to ask the user before a call. The specification's defaults surprise people: a tool that declares nothing is assumed to be destructive (`destructiveHint` defaults to true) and to reach the outside world (`openWorldHint` defaults to true). So every hint is stated, and the hints that the proto already implies (read-only, idempotent) must agree with it.

Hints are not a safety mechanism. Clients must treat them as untrusted unless the server is trusted, and some hosts don't pass them to the model. A consequence the model must know belongs in the description ([`DEDO_TOOL_REQUIRED`](#dedo_tool_required)).

```protobuf
// A read. Only the two hints that apply.
rpc GetProduct(GetProductRequest) returns (GetProductResponse) {
  option idempotency_level = NO_SIDE_EFFECTS;
  option (dedo.v1.method) = {
    tool: {
      annotations: {read_only_hint: true, open_world_hint: false}
    }
  };
}

// A delete: destructive, and idempotent because repeating it changes nothing more.
rpc DeleteProduct(DeleteProductRequest) returns (DeleteProductResponse) {
  option idempotency_level = IDEMPOTENT;
  option (dedo.v1.method) = {
    tool: {
      annotations: {read_only_hint: false, destructive_hint: true, idempotent_hint: true, open_world_hint: false}
    }
  };
}
```

**Fix.**

| Hint | Set it to |
| --- | --- |
| `read_only_hint` | `true` for reads, which also set `idempotency_level = NO_SIDE_EFFECTS`; otherwise `false` |
| `destructive_hint` | `true` if the tool may overwrite or delete anything; `false` only if it purely adds, as a Create does |
| `idempotent_hint` | `true` if repeating the call with the same arguments changes nothing more, which also sets `idempotency_level = IDEMPOTENT` (Update and Delete usually are) |
| `open_world_hint` | `false` if the tool only touches this service's own data; `true` if it reaches outside, such as sending email, charging a card, or calling a third party |

### DEDO_MESSAGE_DESCRIPTION_REQUIRED

**Checks** that every message, nested ones included, sets `dedo.v1.message` with a non-empty `description`. Map entries are exempt.

**Why.** Request messages are the input schemas of MCP tools, and every message is a schema in the REST docs. The description is what agents and API readers see. Proto comments stay for the people who maintain the API.

```protobuf
// Passes.
message GetProductRequest {
  option (dedo.v1.message) = {description: "Which product to get."};
}
```

**Fix.** Add the option. Describe what the message represents for a caller.
