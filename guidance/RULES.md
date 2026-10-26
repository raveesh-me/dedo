# Lint rules

`buf-plugin-dedo-lint` is one Buf check plugin with three classes of rules, each named after its rule prefix:

| Class | Source | What it checks |
| --- | --- | --- |
| `AIP_*` | [Google's AIPs](https://google.aip.dev), through [api-linter](https://github.com/googleapis/api-linter) | Resource-oriented design: names, standard methods, HTTP bindings, field behavior, pagination |
| `ZALANDO_*` | [Zalando's RESTful API guidelines](https://opensource.zalando.com/restful-api-guidelines/) | API ownership, audience, OAuth scope names, caching, deprecation, money |
| `DEDO_*` | dedo itself | What dedo needs to serve one proto as RPC, REST, docs and MCP tools |

A fourth prefix, `HOUSE_*`, is reserved for your organisation's own rules, written as CEL checks in `rules.yml` (see [House rules](#house-rules)). dedo never ships a `HOUSE_*` rule, so yours never clash with an upgrade. House rules rank above dedo's, and may override them (see [Precedence](#precedence)).

`DEDO_MULTI_TENANT_*` rules are dedo's own too, but opt-in: they are a rule set of their own rather than part of `DEDO`, for services that partition their data between tenants (see [Multi-tenant rules](#multi-tenant-rules)).

## Choosing the rules

**`rules.yml` chooses which rules run; `buf.yaml` does not.** In `buf.yaml`, every rule of the plugin is in one category, `DEDO`, and that is the only one of them `buf.yaml` names:

```yaml
lint:
  use:
    - STANDARD   # Buf's own rules, which only buf.yaml can choose
    - COMMENTS
    - DEDO       # every rule of buf-plugin-dedo-lint
```

`rules.yml`, next to `buf.yaml`, chooses among them, so the whole order of the rules, from the sets that run to the last override, is in one file the plugin reads:

```yaml
use:                   # dedo's rule sets this repository runs
  - AIP_NOLANG         # every AIP rule except the language-specific ones
  - ZALANDO
  - DEDO
  - DEDO_MULTI_TENANT  # when its services are multi-tenant
disable: [DEDO_NO_JSON_NAME]   # rules this repository opts out of, by ID
```

- **`use`** names rule sets, or single rules by ID. Without `use`, or without a `rules.yml`, `AIP_NOLANG`, `ZALANDO` and `DEDO` run. `AIP_NOLANG` can be narrowed to `AIP_CORE` or widened to `AIP`, and single AIPs are sets too (`AIP_0131`). House rules always run: they are in `rules.yml` because you want them.
- **`disable`** turns off rules by ID: dedo's, or an imported house rule. A rule that does not run overrides nothing, so the rules it overrode run again.
- **Final rules** always run, whatever `use` and `disable` say (see [Final rules](#final-rules)).
- **Skipped rules** run only when `use` names them by ID (see [Skipped rules](#skipped-rules)).

`dedo init` writes `rules.yml`, and `dedo upgrade` moves a choice made in `buf.yaml` by an earlier release into it: the sets `lint.use` named become `use`, the rules `lint.except` named become `disable`. Keep every rule of the plugin out of `buf.yaml`'s `use` and `except`: dedo reads only `rules.yml`, so a rule turned off there is off without `dedo tools` knowing, final or not. One `rules.yml` serves every module of a Buf workspace.

To pass an api-linter configuration file, add `options: {config: path/to/api-linter.yaml}` to the plugin entry.

Every `ZALANDO_*` and `DEDO_*` finding links to its section below. A release of the plugin links to this file as it was at that release's tag, so the text always matches the rule that reported it. Development builds link to `main`. `AIP_*` findings link to the AIP's rule page on [linter.aip.dev](https://linter.aip.dev).

To suppress one finding, put `// buf:lint:ignore <RULE_ID>` above the element, followed by the reason. Reviewers treat each suppression as a decision to justify.

## Precedence

Rules come in layers, and each layer outranks the ones below it:

1. **Your repository's own house rules**, in its `rules.yml`
2. **Imported house rules**: a rules file outranks the files it imports
3. **DEDO**
4. **AIP**
5. **ZALANDO**

The AIPs outrank Zalando because the contract is a protobuf API, which they were written for; Zalando's guidelines were written for hand-designed JSON-over-HTTP APIs, and dedo adopts the ones that still apply. DEDO outranks both because its rules are what make one proto serve every transport. House rules outrank DEDO because they are your organisation's decisions about its own APIs, and your repository outranks the rules it imports because it knows its own service best.

The known conflicts between the guidelines themselves, and how they resolve:

| Topic | Losing side | Winning side |
| --- | --- | --- |
| Response messages | AIP: Get, Create and Update return the resource itself; Delete returns `Empty` | DEDO: every RPC returns its own `<Rpc>Response`, so fields can be added later. REST unwraps it with `response_body` ([`DEDO_HTTP_RESPONSE_BODY`](#dedo_http_response_body), which overrides the AIP rules that disagree). |
| `method_signature` | AIP: every standard method declares one | DEDO: Connect ignores it, so it is optional and its rules are skipped |
| API version | ZALANDO: no version in the URL; use media-type versioning | AIP and DEDO: the major version is in the package and the path ([`DEDO_HTTP_PATH_VERSION`](#dedo_http_path_version)) |
| JSON field names | ZALANDO: `snake_case` | AIP and DEDO: proto3 JSON's `lowerCamelCase`, never renamed ([`DEDO_NO_JSON_NAME`](#dedo_no_json_name)) |
| Path segments | ZALANDO: `kebab-case` | AIP: `lowerCamelCase` (AIP-122, AIP-127) |
| Error format | ZALANDO: Problem JSON (RFC 9457) | AIP: `google.rpc.Status` with `ErrorInfo` (AIP-193); DEDO checks the declared reasons ([`DEDO_ERROR_REASON_FORMAT`](#dedo_error_reason_format)) |
| Pagination | ZALANDO: cursors in a `next`/`prev` links object | AIP: `page_token` and `next_page_token` (AIP-158) |

When you write a new rule that contradicts one of a lower layer, name that rule in its overrides; a contradiction left undeclared is a bug, since both rules then report.

## Overrides

Nothing detects a conflict between two rules for you. A rule that replaces others says so, by naming them: its `overrides`. An overridden rule reports nothing. It stays a rule, so `buf config ls-lint-rules` shows it as `Overridden by <rule>`, as it shows a rule `rules.yml` leaves out as `Not in rules.yml's use` or `Disabled in rules.yml`.

A rule may override only rules of a lower layer. Among house rules, that means the rules of the files its own rules file imports, directly or through others; two files imported side by side cannot override each other, and neither can two rules of one file. A repository can import as many house sources as it likes, and its own rules may override any of them. When an overridden rule overrides others itself, its overrides lapse with it, and those rules run again: a house rule that overrides `DEDO_HTTP_RESPONSE_BODY` brings back the AIP rules it overrode.

The plugin consolidates the rules every time it starts: it reads `rules.yml`, gathers every rule and every override, checks `use`, `disable` and each override against this order, and runs what is left. Loading and checking the house rules takes well under a millisecond of a lint's 0.3 s, so `rules.yml` is never compiled into the plugin, and an edit takes effect on the next lint. **Run `dedo tools` (or `make tools`) every time `rules.yml` or an import changes.** It runs the same step and lists what runs and every override, so a mistake shows there, with the rule and file that made it, rather than as a failing lint in the editor; it also fetches a changed import's `ref`, which the lint never does:

```text
lint rules: 363 rules, 3 of them house rules; 338 run, 8 overridden, 17 not used, skipped or disabled
  use: AIP_NOLANG, ZALANDO, DEDO
  DEDO_HTTP_RESPONSE_BODY overrides AIP_0131_RESPONSE_MESSAGE_NAME
  ...
  HOUSE_TODO_RESOURCE_TYPE (rules.yml) overrides HOUSE_RESOURCE_TYPE_DOMAIN
```

The consolidation fails, naming every mistake at once, when an override names a rule that does not exist, one that does not rank below it, or a final rule, and when `use` or `disable` names a rule or set that does not exist, or `disable` names a final rule. An override of a rule that `disable` already removed changes nothing.

## Final rules

A final rule cannot be overridden, by any rule, or disabled, and it runs whatever `rules.yml`'s `use` says.

Three of dedo's rules are final because they check what dedo's runtime itself requires. Without them a contract would lint clean and then fail when the service starts:

| Rule | Why it is final |
| --- | --- |
| [`ZALANDO_SERVICE_RULES_REQUIRED`](#zalando_service_rules_required) | `policy.Build` refuses a service without the `dedo.v1.service` option |
| [`DEDO_METHOD_RULES_REQUIRED`](#dedo_method_rules_required) | `policy.Build` refuses a method without scopes |
| [`DEDO_MULTI_TENANT_REQUEST_TENANT`](#dedo_multi_tenant_request_tenant) | `policy.Build` refuses a multi-tenant method whose tenant field it cannot find |

A house rule is final when it says so:

```yaml
rules:
  - id: HOUSE_RESOURCE_TYPE_DOMAIN
    on: resource
    check: type.split('/')[0].endsWith('.acme.com')
    message: Resource types live under acme.com.
    final: true    # no rule may override it, and no rules.yml may disable it
```

Use it for the conventions your organisation does not negotiate: the shared file that defines the rule locks it for every service that imports it, however many files sit in between. A final rule may still override rules below it.

Add a rule beside a final one instead: a rule can demand more than a final rule does, never less. `dedo tools` lists every final rule.

## dedo's overrides

| Rule | Overrides | Why |
| --- | --- | --- |
| [`DEDO_HTTP_RESPONSE_BODY`](#dedo_http_response_body) | `AIP_0131_RESPONSE_MESSAGE_NAME` … `AIP_0135_RESPONSE_MESSAGE_NAME` | Every RPC returns its own `<Rpc>Response`, so fields can be added later; REST unwraps it with `response_body` |
| | `AIP_0121_RESOURCE_MUST_SUPPORT_GET`, `AIP_0134_REQUEST_REQUIRED_FIELDS` | The same wrapping hides the resource from api-linter's standard-method detection, so these report false positives |

Releases before v0.12.0 listed these rules in `buf.yaml`'s `lint.except` instead; `dedo upgrade` takes them out.

## Skipped rules

dedo turns these off on purpose. They are not conflicts, so no rule overrides them: they check things dedo has no use for. The plugin skips the AIP ones itself, unless `rules.yml`'s `use` names one by ID; releases before v0.15.0 listed them in `buf.yaml`'s `lint.except`, and `dedo upgrade` takes them out.

**AIP**

| Rules | Why |
| --- | --- |
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
# rules.yml in a service repository
imports:
  - git: https://github.com/acme/api-standards.git   # the organisation's shared rules
    ref: v3                                           # a tag or a commit
    path: rules.yml                                   # from that repository's root; the default
  - path: platform-rules/rules.yml                    # a folder (its rules.yml) or a file
disable: [HOUSE_MESSAGE_SIZE]                         # rules this service opts out of
rules:
  - id: HOUSE_REST_ROOT                               # this service's own
    on: method
    check: http_path == '' || http_path.startsWith('/commerce/')
    message: REST paths start with /commerce/, the commerce platform's root.
    doc: https://wiki.acme.com/api/rest-roots
  - id: HOUSE_CART_RESOURCE_TYPE
    on: resource
    check: type.startsWith('cart.acme.com/')
    message: Cart resource types live under cart.acme.com.
    overrides: [HOUSE_RESOURCE_TYPE_DOMAIN]           # rules this one replaces
    final: true                                       # optional: no rule may override or disable it
```

House rules run as soon as `rules.yml` defines them; `buf.yaml` already lists `DEDO`, which holds every rule of the plugin. Each rule is a Buf rule of its own, so `// buf:lint:ignore HOUSE_REST_ROOT` suppresses one finding, `disable: [HOUSE_REST_ROOT]` in a service's `rules.yml` turns an imported rule off, and a finding shows the rule's `message` and `doc` link.

**Paths.** Every `path` is relative to the root of the repository whose file names it, wherever in that repository the file sits: `platform-rules/naming.yml` names the same file from `rules.yml` and from `platform-rules/rules.yml`. In your service the root is the folder of `rules.yml`, next to `buf.yaml`, and a path may lead outside it, such as `../platform-rules` in a monorepo.

**Shared rules.** An organisation keeps its rules in one repository and tags it; each service imports it with `git` and `ref`. Imports nest: a shared file can import others, by paths from its own repository's root. Those paths must stay inside that repository, symlinks included: shared rules cannot make the lint read a service's files, or anything else on the machine. `dedo tools` (and `make tools`, `dedo init`, `dedo upgrade`) fetches git imports into `.tools/rules/`; the lint itself never touches the network, so it stays fast in the editor. A missing import fails the lint with "run dedo tools". Run `dedo tools` after every change to `rules.yml` or to an import's `ref`. `disable` lets a service opt out of an imported rule, by ID. Only the service's own `rules.yml` says `use`; an imported file adds rules, and cannot choose which of dedo's run.

**Overrides.** `overrides` lists the rules a rule replaces, which then report nothing. A house rule may override any DEDO, AIP or ZALANDO rule except the [final ones](#final-rules), and the rules of the files its rules file imports; your repository's `rules.yml` imports every other rules file, so its rules may override any imported house rule. Use `overrides` when your rule says something else; use `disable` when you simply do not want a rule. Neither works on a [final](#final-rules) rule. [Precedence](#precedence) has the order, and `dedo tools` lists every override in force.

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

House rules run in the native plugin, which reads `rules.yml`. The WebAssembly plugin cannot read files, so for repositories without Go, a platform team builds one with the house rules built in, `dedo tools -wasm house-lint.wasm` in a repository that has them, and pushes it to the BSR (`buf plugin push buf.build/<org>/house-lint --binary=house-lint.wasm`); those repositories list that plugin instead of `buf.build/r-a/dedo-lint`. It checks the rules exactly as the native plugin does, overrides and final rules included, and reads changes only when it is rebuilt and pushed. [`examples/house-rules`](../examples/house-rules) holds shared rules that both todo examples import, each adding one of its own: `examples/todo` follows them and `examples/bad-todo` breaks them. `examples/todo` also overrides the shared `HOUSE_RESOURCE_TYPE_DOMAIN` with a narrower rule of its own, `HOUSE_TODO_RESOURCE_TYPE`.

A house rule that contradicts a DEDO, AIP or ZALANDO rule without overriding it leaves a contract that no edit can make lint clean, since one of the two always reports. Keep a contract that lints clean in CI (your service, or dedo's starter) to catch that.

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

**Final:** no rule may override it (see [Final rules](#final-rules)).

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

**Final:** no rule may override it (see [Final rules](#final-rules)).

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

**Overrides** `AIP_0131_RESPONSE_MESSAGE_NAME` … `AIP_0135_RESPONSE_MESSAGE_NAME`, `AIP_0121_RESOURCE_MUST_SUPPORT_GET` and `AIP_0134_REQUEST_REQUIRED_FIELDS` (see [dedo's overrides](#dedos-overrides)).

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

## Multi-tenant rules

The `DEDO_MULTI_TENANT_*` rules are for services that partition their data between tenants: a SaaS's customers, a platform's sub-accounts or business locations. They are a rule set of their own, `DEDO_MULTI_TENANT`, which is not part of `DEDO`, so they run only where `rules.yml`'s `use` lists it. `dedo init` adds it when a service declares tenancy. `DEDO_MULTI_TENANT_REQUEST_TENANT` is final, so it runs either way.

```yaml
# rules.yml
use:
  - AIP_NOLANG
  - ZALANDO
  - DEDO
  - DEDO_MULTI_TENANT   # this repository's services are multi-tenant
```

**The model.** The tenant is the root of the resource hierarchy, as AIP-122 lays it out: every resource name starts with the tenant's (`locations/1/contacts/2`), so every request names its tenant in the field its HTTP path binds (`{name=locations/*/contacts/*}`, `{parent=locations/*}`). A service declares the tenant collection once:

```protobuf
service ContactService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "contacts-team"
    tenancy: {collection: "locations"}
  };
}
```

**The runtime.** `policy.Build` finds each RPC's tenant field from its HTTP binding, and refuses to start when one has none. The interceptor reads the tenants of every request message, on every transport (Connect, gRPC, REST and MCP, streams included), and asks the `interceptor.Config.Tenants` function you supply whether the caller may act in each: the tenant of the field the path binds, which must be set, and the tenant of every other field that references a resource (`google.api.resource_reference`) and holds a name under the tenant collection, anywhere in the request. So a move from `locations/1/contacts/2` to `destination: "locations/2"` needs access to both. A request naming a tenant the caller may not act in gets `PERMISSION_DENIED`, the same answer whether or not the resource exists (AIP-211), before validation or your handler sees it, and the message is cleared. A malformed name, such as one outside the tenant or with an empty, `.` or `..` segment, gets `INVALID_ARGUMENT`. Give every field that holds a resource name a `resource_reference`, as AIP-122 asks: a field without one is not checked. There is no audit mode: a tenant boundary is enforced from the start. A read across tenants with AIP-159's wildcard (`locations/-/contacts`) reaches your function as the tenant `-`; grant it only to callers who may see every tenant, and filter the results in the handler.

RPCs that are not about one tenant, such as listing the caller's own tenants, set `cross_tenant: true` in `dedo.v1.method`. The runtime does no tenant check for them, so their scopes alone guard them. [`examples/contacts`](../examples/contacts) is a multi-tenant contract that CI lints with every rule, these included.

| Rule | Checks |
| --- | --- |
| [`DEDO_MULTI_TENANT_SERVICE_TENANCY`](#dedo_multi_tenant_service_tenancy) | Services declare their tenant collection |
| [`DEDO_MULTI_TENANT_RESOURCE_PATTERN`](#dedo_multi_tenant_resource_pattern) | Resource patterns start with the tenant |
| [`DEDO_MULTI_TENANT_REQUEST_TENANT`](#dedo_multi_tenant_request_tenant) | Requests name their tenant in a field the HTTP path binds |
| [`DEDO_MULTI_TENANT_CACHE_PRIVATE`](#dedo_multi_tenant_cache_private) | Shared caches never store tenant data |

### DEDO_MULTI_TENANT_SERVICE_TENANCY

**Checks** that every service sets `dedo.v1.service.tenancy.collection` to a collection ID in lowerCamelCase, such as `locations` or `subAccounts`.

**Why.** In a repository that opts into these rules, a service without tenancy would serve every tenant's data to any caller with the scope. The declaration is what turns the runtime's tenant check on.

**Fix.** Declare the collection of the tenant resource: `tenancy: {collection: "locations"}`.

### DEDO_MULTI_TENANT_RESOURCE_PATTERN

**Checks** that every resource pattern declared in a package with a multi-tenant service, on messages and in `google.api.resource_definition`, starts with a tenant collection: `locations/{location}/contacts/{contact}`, or the tenant's own `locations/{location}`.

**Why.** The runtime finds the tenant in resource names. A resource outside the hierarchy, such as `contacts/{contact}`, has no tenant to check, and its IDs would have to be unique across every tenant.

```protobuf
// Fails: the contact belongs to no tenant.
option (google.api.resource) = {type: "contacts.acme.com/Contact" pattern: "contacts/{contact}"};

// Passes.
option (google.api.resource) = {type: "contacts.acme.com/Contact" pattern: "locations/{location}/contacts/{contact}"};
```

**Fix.** Put the resource under the tenant, and its HTTP paths with it.

### DEDO_MULTI_TENANT_REQUEST_TENANT

**Checks** that every RPC of a multi-tenant service, unless it is `cross_tenant`, binds a request field to the tenant in its HTTP path: a variable whose template starts with `<collection>/*`, such as `{parent=locations/*}` or `{contact.name=locations/*/contacts/*}`. Every additional binding must bind the same field, a path may bind only one, and the field must be a singular string. It also reports `cross_tenant` on a service without tenancy, where it means nothing, and on an RPC whose path names a tenant, which the runtime would then not check.

**Why.** The runtime reads the tenant from that field on every transport, REST or not, and the service will not start without it. This rule is the same check as the runtime's (`policy.MethodTenant`), so a contract that lints clean starts.

```protobuf
// Fails: the path binds no tenant.
rpc GetContact(GetContactRequest) returns (GetContactResponse) {
  option (google.api.http) = {get: "/commerce/contacts/v1/{name=contacts/*}"};
}

// Passes.
rpc GetContact(GetContactRequest) returns (GetContactResponse) {
  option (google.api.http) = {get: "/commerce/contacts/v1/{name=locations/*/contacts/*}"};
}
```

**Fix.** Bind the resource name, or the parent, with the tenant in its template. For an RPC that truly spans tenants, set `cross_tenant: true` and give it a scope of its own.

**Final:** no rule may override it (see [Final rules](#final-rules)).

### DEDO_MULTI_TENANT_CACHE_PRIVATE

**Checks** that no RPC of a multi-tenant service has a cache policy with `VISIBILITY_PUBLIC`, `cross_tenant` ones included.

**Why.** `Cache-Control: public` lets shared caches, such as a CDN, store the response and serve it to the next caller of the same URL without asking the server, so the tenant check never runs.

**Fix.** Use `VISIBILITY_PRIVATE`.
