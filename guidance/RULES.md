# Lint rules

`buf-plugin-dedo-lint` is one Buf check plugin with three classes of rules, each named after its rule prefix:

| Class | Source | What it checks |
| --- | --- | --- |
| `AIP_*` | [Google's AIPs](https://google.aip.dev), through [api-linter](https://github.com/googleapis/api-linter) | Resource-oriented design: names, standard methods, HTTP bindings, field behavior, pagination |
| `ZALANDO_*` | [Zalando's RESTful API guidelines](https://opensource.zalando.com/restful-api-guidelines/) | API ownership, audience, OAuth scope names, caching, deprecation, money |
| `DEDO_*` | dedo itself | What dedo needs to serve one proto as RPC, REST, docs and MCP tools |

A fourth prefix, `HOUSE_*`, is reserved for your organisation's own rules, written as CEL checks in `dedo.yaml` (see [House rules](#house-rules)). dedo never ships a `HOUSE_*` rule, so yours never clash with an upgrade. House rules rank above dedo's, and may override them (see [Precedence](#precedence)).

`DEDO_MULTI_TENANT_*` rules are dedo's own too, but opt-in: they are a rule set of their own rather than part of `DEDO`, for services that partition their data between tenants (see [Multi-tenant rules](#multi-tenant-rules)).

## Choosing the rules

**`dedo.yaml` chooses which rules run; `buf.yaml` does not.** In `buf.yaml`, every lint rule of the plugin is in one category, `DEDO`, and every breaking rule in another, `DEDO_BREAKING`; those are the only ones of them `buf.yaml` names:

```yaml
lint:
  use:
    - STANDARD        # Buf's own rules, which only buf.yaml can choose
    - COMMENTS
    - DEDO            # every lint rule of buf-plugin-dedo-lint
breaking:
  use:
    - FILE            # Buf's own breaking rules
    - DEDO_BREAKING   # every breaking rule of buf-plugin-dedo-lint
```

`dedo.yaml`, next to `buf.yaml`, chooses among them: its `lint` section the lint rules, and its `breaking` section the [breaking rules](#breaking-rules). The whole order of the rules, from the sets that run to the last override, is in one file the plugin reads. A shared rules file that services import, a `rules.yml`, has the same `imports`, `lint` and `breaking`, without `use`. A repository set up before v0.20.0 kept these in a `rules.yml` of its own; the plugin reads it where there is no `dedo.yaml`, and `dedo upgrade` moves it into one, as it moves a `dedo.yaml` from before v0.32.1, whose one `rules` section held what `imports` and `lint` hold now.

```yaml
imports: [...]           # shared house rules, lint and breaking, which build on the sets below
lint:
  use:                   # the lint rule sets this repository runs
    - AIP_NOLANG         # every AIP rule except the language-specific ones
    - ZALANDO
    - DEDO
    - HOUSE              # the house lint rules: this file's and the imported ones
    - DEDO_MULTI_TENANT  # when its services are multi-tenant
  disable: [DEDO_NO_JSON_NAME]   # rules this repository opts out of, by ID
  rules: [...]           # this repository's own, which outrank everything
breaking:
  use: [DEDO, HOUSE]     # dedo's breaking rules and the house ones; the default
  disable: [DEDO_SUNSET_NO_EARLIER]
  rules: [...]           # this repository's own breaking rules
annexure: [...]          # the organisation's guidance for coding agents (install.md, section 5)
```

[templates/dedo.example.yaml](../templates/dedo.example.yaml) (`dedo templates dedo.example.yaml`) uses every key: all five sets, three imports, `disable`, its own rules overriding an imported rule and one of dedo's, and two annexures.

Each section takes the same keys, and the same rules apply to both, each to rules of its own kind: a lint rule never overrides or disables a breaking rule, nor the other way round, and every rule ID is unique across both.

- **`use`** names rule sets, or single rules by ID. Without `lint.use`, or without a `dedo.yaml`, `AIP_NOLANG`, `ZALANDO`, `DEDO` and `HOUSE` run; without `breaking.use`, `DEDO` and `HOUSE`, the only breaking sets, run; `DEDO_MULTI_TENANT` is opt-in, since it reports every service that declares no tenancy. `AIP_NOLANG` can be narrowed to `AIP_CORE` or widened to `AIP`, and single AIPs are sets too (`AIP_0131`).
- **`imports` and `rules`** are the house rules, which `HOUSE` runs: `imports`, at the top of the file, brings both kinds, and each section's `rules` adds its own. They build on the sets in `use` and override their rules where they say so (see [Precedence](#precedence)).
- **`disable`** turns off rules by ID: dedo's, or an imported house rule. A rule that does not run overrides nothing, so the rules it overrode run again.
- **Final rules** always run, whatever `use` and `disable` say (see [Final rules](#final-rules)).
- **Skipped rules** run only when `use` names them by ID (see [Skipped rules](#skipped-rules)).

`dedo init` writes `dedo.yaml`, and `dedo upgrade` moves a choice made in `buf.yaml` by an earlier release into it: the sets `buf.yaml`'s `lint.use` named become `lint.use`, the rules its `lint.except` named become `lint.disable`. Both add `DEDO_BREAKING` to `buf.yaml`'s `breaking.use`, keeping the rules it chose. Keep every rule of the plugin out of `buf.yaml`'s `use` and `except`: dedo reads only `dedo.yaml`, so a rule turned off there is off without `dedo tools` knowing, final or not. One `dedo.yaml` serves every module of a Buf workspace.

To pass an api-linter configuration file, add `options: {config: path/to/api-linter.yaml}` to the plugin entry.

Every `ZALANDO_*` and `DEDO_*` finding links to its section below. A release of the plugin links to this file as it was at that release's tag, so the text always matches the rule that reported it. Development builds link to `main`. `AIP_*` findings link to the AIP's rule page on [linter.aip.dev](https://linter.aip.dev).

To suppress one finding, put `// buf:lint:ignore <RULE_ID>` above the element, followed by the reason. Reviewers treat each suppression as a decision to justify.

## Precedence

Rules come in layers, and each layer outranks the ones below it:

1. **Your repository's own house rules**, in its `dedo.yaml`
2. **Imported house rules**: a rules file outranks the files it imports
3. **DEDO**
4. **AIP**
5. **ZALANDO**

Breaking rules rank the same way, in fewer layers: your repository's own house rules, then the imported ones, then DEDO's.

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

Nothing detects a conflict between two rules for you. A rule that replaces others says so, by naming them: its `overrides`. An overridden rule reports nothing. It stays a rule, so `buf config ls-lint-rules` shows it as `Overridden by <rule>`, as it shows a rule `dedo.yaml` leaves out as `Not in dedo.yaml's lint.use` or `Disabled in dedo.yaml's lint section` (`breaking`, for a breaking rule).

A rule may override only rules of a lower layer. Among house rules, that means the rules of the files its own rules file imports, directly or through others; two files imported side by side cannot override each other, and neither can two rules of one file. A repository can import as many house sources as it likes, and its own rules may override any of them. When an overridden rule overrides others itself, its overrides lapse with it, and those rules run again: a house rule that overrides `DEDO_HTTP_RESPONSE_BODY` brings back the AIP rules it overrode.

The plugin consolidates the rules every time it starts: it reads `dedo.yaml`, gathers every rule and every override, checks `use`, `disable` and each override against this order, and runs what is left. Loading and checking the house rules takes well under a millisecond of a lint's 0.3 s, so `dedo.yaml` is never compiled into the plugin, and an edit takes effect on the next lint. **Run `dedo tools` (or `make tools`) every time `dedo.yaml`'s rules or an import changes.** It runs the same step and lists what runs and every override, so a mistake shows there, with the rule and file that made it, rather than as a failing lint in the editor; it also fetches a changed import's `ref`, which the lint never does:

```text
rules in force:
lint: 366 rules, 3 of them house rules; 341 run, 8 overridden, 17 not used, skipped or disabled
  use: AIP_NOLANG, ZALANDO, DEDO, HOUSE
  DEDO_HTTP_RESPONSE_BODY overrides AIP_0131_RESPONSE_MESSAGE_NAME
  ...
  HOUSE_TODO_RESOURCE_TYPE (dedo.yaml) overrides HOUSE_RESOURCE_TYPE_DOMAIN
breaking: 11 rules, 1 of them house rules; 11 run, 0 overridden, 0 not used, skipped or disabled
  use: DEDO, HOUSE (the default; dedo.yaml does not say breaking.use)
```

The consolidation fails, naming every mistake at once, when an override names a rule that does not exist, one that does not rank below it, or a final rule, and when `use` or `disable` names a rule or set that does not exist, or `disable` names a final rule. An override of a rule that `disable` already removed changes nothing.

## Final rules

A final rule cannot be overridden, by any rule, or disabled, and it runs whatever its section's `use` says. dedo's breaking rules are not final; a house breaking rule can be.

Three of dedo's rules are final because they check what dedo's runtime itself requires. Without them a contract would lint clean and then fail when the service starts:

| Rule | Why it is final |
| --- | --- |
| [`ZALANDO_SERVICE_RULES_REQUIRED`](#zalando_service_rules_required) | `policy.Build` refuses a service without the `dedo.v1.service` option |
| [`DEDO_METHOD_RULES_REQUIRED`](#dedo_method_rules_required) | `policy.Build` refuses a method without scopes |
| [`DEDO_MULTI_TENANT_REQUEST_TENANT`](#dedo_multi_tenant_request_tenant) | `policy.Build` refuses a multi-tenant method whose tenant field it cannot find |

A house rule, lint or breaking, is final when it says so:

```yaml
lint:
  rules:
    - id: HOUSE_RESOURCE_TYPE_DOMAIN
      on: resource
      check: type.split('/')[0].endsWith('.acme.com')
      message: Resource types live under acme.com.
      final: true    # no rule may override it, and no file may disable it
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

dedo turns these off on purpose. They are not conflicts, so no rule overrides them: they check things dedo has no use for. The plugin skips the AIP ones itself, unless `dedo.yaml`'s `lint.use` names one by ID; releases before v0.15.0 listed them in `buf.yaml`'s `lint.except`, and `dedo upgrade` takes them out.

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

`HOUSE_*` rules are your organisation's own, for conventions dedo has no business knowing: your domain names, your REST roots, your field naming. You write them in `dedo.yaml` next to `buf.yaml`, as [CEL](https://cel.dev) checks: lint rules in its `lint` section, which `buf lint` runs, and breaking rules in its `breaking` section, which `buf breaking` runs. `buf-plugin-dedo-lint` runs them with dedo's own rules. No plugin to write, build or release.

```yaml
# dedo.yaml in a service repository
imports:                                                # shared rules, lint and breaking
  - git: https://github.com/acme/api-standards.git     # the organisation's shared rules
    ref: v3                                             # a tag or a commit
    path: rules.yml                                     # from that repository's root; the default
  - url: https://acme.github.io/api-standards/v3/rules.yml   # or a file published over HTTPS
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  - path: platform-rules/rules.yml                      # a folder (its rules.yml) or a file
lint:
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
breaking:
  rules:
    - id: HOUSE_COMMERCE_ROOT_KEPT                      # checked against the earlier version
      on: method
      check: "!against.http_path.startsWith('/commerce/') || http_path.startsWith('/commerce/')"
      message: A method served under /commerce/ stays there.
```

House rules run while their section's `use` lists `HOUSE`, as it does by default; `buf.yaml` already lists `DEDO` and `DEDO_BREAKING`, which hold every rule of the plugin. Each rule is a Buf rule of its own, so `// buf:lint:ignore HOUSE_REST_ROOT` suppresses one lint finding (`// buf:breaking:ignore` a breaking one), `disable: [HOUSE_REST_ROOT]` in a service's `lint` section turns an imported rule off, and a finding shows the rule's `message` and `doc` link.

**Breaking rules.** A rule of a `breaking` section runs in `buf breaking`, on each element that the version compared against also has: the same file, service, method, message, field (by number), enum or resource. Its check reads the current element's variables, below, and the earlier element's as `against`, a map of the same names: `against.scopes.all(s, s in scopes)` keeps every scope. An element that is new, or deleted, is not checked; Buf's `FILE` rules report a deletion. Imports, `disable`, `overrides` and `final` work as for lint rules, among breaking rules: a breaking house rule overrides dedo's [breaking rules](#breaking-rules) and the breaking rules of the files it imports.

**Paths.** Every `path` is relative to the root of the repository whose file names it, wherever in that repository the file sits: `platform-rules/naming.yml` names the same file from `dedo.yaml` and from `platform-rules/rules.yml`. In your service the root is the folder of `dedo.yaml`, next to `buf.yaml`, and a path may lead outside it, such as `../platform-rules` in a monorepo.

**Shared rules.** An organisation keeps its rules in one repository and tags it; each service imports it with `git` and `ref`. Imports nest: a shared file can import others, by paths from its own repository's root. Those paths must stay inside that repository, symlinks included: shared rules cannot make the lint read a service's files, or anything else on the machine. `dedo tools` (and `make tools`, `dedo init`, `dedo upgrade`) fetches git imports into `.tools/rules/`; the lint itself never touches the network, so it stays fast in the editor. A missing import fails the lint with "run dedo tools". Run `dedo tools` after every change to `dedo.yaml`'s rules, of either kind, or to an import's `ref`.

**Published rules.** A git import needs read access to the repository, which CI often lacks for a private one. Publish each release of the rules over HTTPS instead, such as to a static site at a path per version, and import it with `url` and the file's `sha256`; `dedo tools` downloads it, checks the hash, and fails on a mismatch. Without `sha256` it fails too, naming the file's hash to pin. The folder of the URL is the import's root: the file's own `path` imports are fetched from beside it, must stay inside that folder, and each carries a `sha256`, so every file read over the network is pinned. A relative `doc` on a rule of a url-imported file, such as `doc: RULES.html#house_rest_root`, is resolved against the file's URL, so a finding links to the documentation of the release that reported it; publish that page beside the rules. On GitHub Pages, which can serve a site publicly from a private repository, have the publishing workflow rebuild the site from every tag, since each deploy replaces the whole site: a published release then never changes or disappears. `disable` lets a service opt out of an imported rule, by ID, in the section of the rule's kind. Only the service's own `dedo.yaml` says `use`; an imported file adds rules, and cannot choose which of dedo's run.

**Overrides.** `overrides` lists the rules a rule replaces, which then report nothing. A house lint rule may override any DEDO, AIP or ZALANDO lint rule except the [final ones](#final-rules), a house breaking rule any of dedo's breaking rules, and the rules of the files its rules file imports; your repository's `dedo.yaml` imports every other rules file, so its rules may override any imported house rule. Use `overrides` when your rule says something else; use `disable` when you simply do not want a rule. Neither works on a [final](#final-rules) rule. [Precedence](#precedence) has the order, and `dedo tools` lists every override in force.

**Checks.** `check` is a CEL expression that is true when the element passes. `on` picks the element, and these are the names a check can use:

| `on` | Variables |
| --- | --- |
| `file` | `name` (the path), `proto_package`, `imports`, `options` |
| `service` | `name`, `full_name`, `file`, `proto_package`, `methods`, `resources` (the `google.api.resource` types its methods take or return, at any depth, each once), `roots` (those of `resources` with no pattern nested under another's: `orders/{order}/lines/{line}` belongs to `orders/{order}`), `title` (of `dedo.v1.service`, empty when unset), `options` |
| `method` | `name`, `full_name`, `file`, `proto_package`, `service`, `input`, `output`, `client_streaming`, `server_streaming`, `idempotency` (`NO_SIDE_EFFECTS`, `IDEMPOTENT`, `UNKNOWN`), `http_verb` and `http_path` (of the `google.api.http` rule), `http_bindings` (the rule and each of its `additional_bindings`, as `{verb, path}` maps), `scopes`, `errors` (reasons), `deprecated`, `options`, `service_options` (its service's `options`) |
| `message` | `name`, `full_name`, `file`, `proto_package`, `fields`, `options` |
| `field` | `name`, `full_name`, `file`, `proto_package`, `message`, `type` (a scalar kind such as `string`, or a message or enum full name), `repeated`, `optional`, `behaviors` (such as `REQUIRED`), `json_name`, `options` |
| `enum` | `name`, `full_name`, `file`, `proto_package`, `values`, `options` |
| `resource` | `name`, `full_name`, `file`, `proto_package`, `type`, `patterns`, `singular`, `plural`, `options` |

`options` holds every extension set on the element's options, keyed by the extension's full name, whatever file declares it: dedo's own, such as `dedo.v1.method`, and your organisation's, which buf passes with the module's imports. A message value is a map of its fields by their names in the `.proto`, an enum is its value's name, a repeated field is a list and a map field a map with string keys. An unset scalar reads as its zero value and an unset repeated or map field as empty; an unset message field, and an unset member of a oneof (a proto3 `optional` field among them), are left out, so test them with `has()`. An extension that is not set is not a key, so test it with `in`. A method also reads its service's options as `service_options`. With `acme.auth.v1` options of your own:

```yaml
- id: HOUSE_TENANT_DECLARED
  on: service
  check: "'acme.auth.v1.service' in options && options['acme.auth.v1.service'].tenant != 'TENANT_UNSPECIFIED'"
  message: Every service declares its tenant.
- id: HOUSE_CALLERS_NOT_PUBLIC
  on: method
  check: >-
    !('acme.auth.v1.method' in options) ||
    options['acme.auth.v1.method'].callers.size() == 0 ||
    service_options['dedo.v1.service'].audience != 'AUDIENCE_EXTERNAL_PUBLIC'
  message: A public method takes no service-to-service callers.
```

A breaking rule reads the earlier element's as `against.options`: `against.options['acme.auth.v1.file'].permissions == options['acme.auth.v1.file'].permissions` keeps a file's permissions as they were.

Lists are lists of strings (`'shop.cart.read' in scopes`, `fields.size() < 30`), except `http_bindings`, a list of maps (`http_bindings.all(b, !b.path.contains('_'))`, empty for a method without HTTP); booleans are booleans. CEL's string extensions are on (`split`, `lowerAscii`, `replace`, `trim`), and so is `cel.bind`, which names a value once instead of repeating the expression for it: `cel.bind(verb, http_path.substring(http_path.lastIndexOf(':') + 1), verb == '' || verb.matches('^[a-z][a-zA-Z0-9]*$'))`. A check that does not compile, or is not a bool, stops the lint and names the rule and its file; so does a lint rule that reads `against`, which only a breaking rule has.

A service's `resources` and `roots` let an organisation lint the aggregate shape that [domains.md](domains.md#aggregates-one-service-each) asks for. A domain service has neither, so both rules pass it:

```yaml
- id: HOUSE_SERVICE_ONE_ROOT
  on: service
  check: roots.size() <= 1
  message: A service manages one aggregate root, with its child resources.
- id: HOUSE_SERVICE_NAMED_AFTER_ROOT
  on: service
  check: roots.all(r, name == r.split('/')[1] + 'Service')
  message: A service is named after the root it manages, such as OrderService for shop.example.com/Order.
```

House rules run in the native plugin, which reads `dedo.yaml`. The WebAssembly plugin cannot read files, so a repository that cannot run the native plugin (`dedo install` and `dedo tools` give it one even without `go.mod`) needs a platform team to build one with the house rules built in, `dedo tools -wasm house-lint.wasm` in a repository that has them, and pushes it to the BSR (`buf plugin push buf.build/<org>/house-lint --binary=house-lint.wasm`); those repositories list that plugin instead of `buf.build/r-a/dedo-lint`. It checks the rules exactly as the native plugin does, overrides and final rules included, and reads changes only when it is rebuilt and pushed. [`examples/house-rules`](../examples/house-rules) holds shared rules that both todo examples import, each adding one of its own: `examples/todo` follows them and `examples/bad-todo` breaks them. `examples/todo` also overrides the shared `HOUSE_RESOURCE_TYPE_DOMAIN` with a narrower rule of its own, `HOUSE_TODO_RESOURCE_TYPE`.

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
| [`DEDO_PACKAGE_SHARED_RESERVED`](#dedo_package_shared_reserved) | No package names its module `shared` |
| [`DEDO_DOCS_NO_CODE_STRUCTURE`](#dedo_docs_no_code_structure) | Headings and descriptions in the reference name no code structure |
| [`DEDO_EVENT_TYPES`](#dedo_event_types) | Declared event types belong to the package, name one of its resources, appear once, and only on RPCs that change state |
| [`DEDO_EVENT_NOT_PUBLISHED_DECLARED`](#dedo_event_not_published_declared) | RPCs that emit events declare `EVENT_NOT_PUBLISHED` with `UNAVAILABLE` |
| [`DEDO_NO_HEALTH_RPC`](#dedo_no_health_rpc) | No health check in the contract: the server serves `GET /healthz` |

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

**Fix.** Rename the reason, or pick the code that describes the failure. `examples/todo/internal/todo/errors.go` shows how to return the reason in an `ErrorInfo`.

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
| `open_world_hint` | `false` if the tool only touches this service's own data; `true` if it reaches outside, such as sending email, charging a card, or calling a third party. A read-only call to a third party may stay `false` when it returns reference data the service could hold itself ([domains.md](domains.md#domain-services-operations-with-no-aggregate)) |

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

### DEDO_PACKAGE_SHARED_RESERVED

**Checks** that no proto package names its module `shared`. The module is the segment before the version, or the last segment of an unversioned package.

**Why.** Each module's code lives at `internal/<module>/`, and its tables in a Postgres schema of the same name. `internal/shared/` and the `shared` schema hold what the service's modules share: helpers and bookkeeping. A module called `shared` would collide with both (see [domains.md](domains.md#modules-one-package-each)). It is also a technical name, not a domain noun.

```protobuf
package acme.shared.v1;   // Fails.
package acme.shared;      // Fails.
package acme.type;        // Passes: shared value objects, not a module.
package acme.orders.v1;   // Passes.
```

**Fix.** Name the module with the domain noun for what it is responsible for. Put value objects that several modules use in `<context>.type`, with no services and no version.

### DEDO_DOCS_NO_CODE_STRUCTURE

**Checks** the text a service publishes:

- a service's heading in the reference, its `dedo.v1.service` title or else its name without the `Service` suffix, has no word *service*, *module*, *aggregate* or *microservice*, singular or plural, unless a resource the service manages is named with it;
- no leading comment of a package, service, RPC, message, field, enum or enum value, and no tool title or description or message description, names a proto service: an identifier ending in `Service`, such as `OrderService`, unless a message or enum of that name is in the file or one it imports.

**Why.** The reference is read by callers, who look for the domain's nouns and what they can do with them. *Service*, *module* and *aggregate* are how the code is built ([domains.md](domains.md#the-layers)), and a heading or description that says them shows the reader the code instead of the domain. They also tie the reference to the code's layout: two services that merge change the reader's navigation even when no REST path changes. The words alone are English, so prose may say them (a self-service desk, an aggregate amount); only a proto service's name, which no caller of the REST API ever sees, is flagged there.

```protobuf
// Fails: the heading is "Filing Calendar Service".
service FilingCalendarService {
  option (dedo.v1.service) = {title: "Filing Calendar Service"};
}

// Fails: the heading is "Catalog Module".
service CatalogModuleService {}

// Fails: the description names a proto service.
// OrderService reads orders.
service OrderService {}

// Passes: the heading is "Orders", and the description says what it does.
// Reads the orders buyers placed.
service OrderService {
  option (dedo.v1.service) = {title: "Orders"};
}

// Passes: service is the domain's noun, the resource the API manages.
// The services a location offers, such as a haircut.
service ServiceService {
  rpc GetService(GetServiceRequest) returns (Service);
}
```

**Fix.** Give the service a `title` with the noun readers look for. Renaming the proto service is not needed, and would change its Connect and gRPC paths. Rewrite a comment to say what the API does for its caller, not which service does it.

### DEDO_EVENT_TYPES

**Checks** the event types an RPC declares in `emits`:

- each is `<context>.<module>.<resource>.<past-tense verb>`: the RPC's package without its version, then the resource and the verb, in snake_case;
- the resource is one the package declares with `google.api.resource`, by its singular in snake_case: `order_line` for `singular: "orderLine"`, or for the type `example.com/OrderLine`;
- each appears once;
- the RPC is not `NO_SIDE_EFFECTS`.

**Why.** An event says that something happened to a resource, and nothing more: consumers fetch the resource through the API ([domains.md](domains.md#domain-events-outward-only)). The type is the whole of the event's contract, so it names the context and module that publish it, and the transition the business names (`cancelled`, not `updated`). It carries no payload, so it does not change with the package's major version. A query changes nothing, so it has nothing to announce.

```protobuf
package acme.orders.v1;

rpc CancelOrder(CancelOrderRequest) returns (Order) {
  option (dedo.v1.method) = {
    emits: "acme.orders.order.cancelled"      // Passes.
    // emits: "acme.orders.v1.order.cancelled"  Fails: no version.
    // emits: "billing.invoice.voided"          Fails: another context's type.
    // emits: "acme.orders.cancelled"           Fails: no resource.
    // emits: "acme.orders.basket.cancelled"    Fails unless the package declares a Basket resource.
  };
}
```

dedo does not check that the verb is in the past tense: a heuristic would reject `paid` and `sent`. A house rule can.

**Fix.** Start the type with the package without its version, then name the resource and what happened to it. Declare events only on RPCs that change state. `emits` stays optional: these checks apply only to an RPC that declares it.

### DEDO_EVENT_NOT_PUBLISHED_DECLARED

**Checks** that an RPC with `emits` declares the error `{reason: "EVENT_NOT_PUBLISHED", code: UNAVAILABLE}`. An RPC without `emits` is not checked.

**Why.** `events.Publish` returns that error when the bus does not take an event within the request's deadline, and the handler returns it so the caller retries the command ([domains.md](domains.md#domain-events-outward-only)). Undeclared, the error is missing from the REST reference and the MCP tool description, and the policy interceptor logs it as an undeclared reason.

```protobuf
option (dedo.v1.method) = {
  errors: {reason: "ORDER_NOT_FOUND", code: NOT_FOUND}
  errors: {reason: "EVENT_NOT_PUBLISHED", code: UNAVAILABLE}  // Passes; without it, fails.
  emits: "acme.orders.order.cancelled"
};
```

**Fix.** Add the error. A handler that logs a failed publish and succeeds instead still declares it, since the RPC's contract allows either.

### DEDO_NO_HEALTH_RPC

**Checks** that the contract declares no health check:

- no file imports `grpc/health/v1/health.proto`;
- no service is named `Health`, `HealthCheck`, `Healthz`, `Ping`, `Liveness` or `Readiness`, with or without the `Service` suffix, and on one that is, no RPC is named `Check`, `Watch` or `List`, gRPC's health methods;
- no RPC, on any service, is named one of those six names;
- no message is named one of them, with or without a `Request` or `Response` suffix (`HealthCheckResponse`, `PingRequest`), or starts with `HealthCheck` (`HealthCheckComponent`).

A domain's own words pass: `HealthRecord`, `HealthInsurancePlan`, `GetHealthRecord`, `PingPatient`.

**Why.** Health belongs to the process, not to the API. Every server dedo scaffolds serves `GET /healthz` beside the API, outside the interceptors, so a probe needs no token and a scope never gates it. In the contract, a health RPC becomes a REST path, an MCP tool and a reference entry that callers would think is part of the domain; it needs a scope, which a probe does not carry; and every service declares the same messages again, or imports them from a shared package that every service then depends on.

```protobuf
import "grpc/health/v1/health.proto";            // Fails.

service HealthService {                           // Fails.
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);  // Fails.
}

service OrderService {
  rpc Ping(PingRequest) returns (PingResponse);   // Fails, and so do both messages.
  rpc GetOrder(GetOrderRequest) returns (Order);  // Passes.
}
```

Buf's check plugins cannot report a warning: every finding fails `buf lint`. A service that must keep a health RPC for now disables the rule in `dedo.yaml`, with a comment saying when it goes.

**Fix.** Remove the health service, RPCs and messages, and let the server answer probes: `dedo init` writes `GET /healthz` into `main.go`. A server written before that adds the route itself, next to `/docs`:

```go
mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})
```

A readiness probe that checks the server's dependencies, such as its database, is the server's too: serve it beside `/healthz`, never in the contract.

## Breaking rules

`buf breaking` compares a contract with an earlier version of itself. Buf's own rules (`FILE`) check the descriptors but never the values of options, so they pass a change to a scope, an event type, a REST path or a resource pattern that breaks every caller who relied on it. dedo's breaking rules check those, in the same plugin: `buf.yaml`'s `breaking.use` lists `DEDO_BREAKING` beside `FILE` (`dedo init` and `dedo upgrade` add it), and `dedo.yaml`'s `breaking` section chooses which of them run, as its `lint` section does for lint rules: `use`, `disable`, house rules with `overrides` and `final`, and the rules its imports bring. Your organisation's own breaking rules are house rules there ([House rules](#house-rules)).

Each rule pairs an element with the one of the same name in the version compared against, so an element that was added, or deleted (which Buf's `FILE` rules report), is not checked.

| Rule | Checks |
| --- | --- |
| [`DEDO_EMITS_NO_DELETE`](#dedo_emits_no_delete) | An RPC still declares every event type it declared |
| [`DEDO_SCOPES_NO_DELETE`](#dedo_scopes_no_delete) | An RPC still accepts every scope it accepted |
| [`DEDO_ERROR_REASON_SAME_CODE`](#dedo_error_reason_same_code) | An error reason keeps its status code |
| [`DEDO_AUDIENCE_NO_NARROW`](#dedo_audience_no_narrow) | A service's audience does not narrow |
| [`DEDO_TENANCY_SAME`](#dedo_tenancy_same) | A service's tenancy does not change |
| [`DEDO_CROSS_TENANT_NO_DELETE`](#dedo_cross_tenant_no_delete) | A cross-tenant RPC stays cross-tenant |
| [`DEDO_HTTP_BINDING_SAME`](#dedo_http_binding_same) | Every REST binding stays, with the same body and response body |
| [`DEDO_FIELD_NO_NEW_REQUIRED`](#dedo_field_no_new_required) | No field becomes `REQUIRED` |
| [`DEDO_RESOURCE_SAME`](#dedo_resource_same) | A resource keeps its type and every pattern |
| [`DEDO_SUNSET_NO_EARLIER`](#dedo_sunset_no_earlier) | A sunset does not move earlier |

A change these rules report is still possible: make it in a new major version of the package (AIP-185), or add the label your CI reads to allow a breaking change.

### DEDO_EMITS_NO_DELETE

**Checks** that an RPC still declares, in `emits`, every event type it declared. Adding a type passes, and so does reordering.

**Why.** Consumers subscribe to an event by its type. A renamed or dropped type silently stops them hearing about the change.

```protobuf
// Before:
option (dedo.v1.method) = {emits: "acme.orders.order.cancelled"};
// After, fails: the type was renamed.
option (dedo.v1.method) = {emits: "acme.orders.order.voided"};
// After, passes: the old type stays, and the new one joins it.
option (dedo.v1.method) = {emits: ["acme.orders.order.cancelled", "acme.orders.order.voided"]};
```

**Fix.** Keep the type, and publish the new one beside it until no consumer needs the old one.

### DEDO_SCOPES_NO_DELETE

**Checks** that an RPC still accepts every token scope it accepted. Adding a scope passes: a caller needs only one.

**Why.** A caller whose token holds only the removed scope is denied from the next deploy.

**Fix.** Keep the scope. To move callers to another, add it, and remove the old one in a new major version.

### DEDO_ERROR_REASON_SAME_CODE

**Checks** that an error reason an RPC still declares keeps its status code. Adding or removing a reason passes.

**Why.** Clients handle errors by reason and code, and retry on some codes but not others. `ORDER_NOT_FOUND` turning from `NOT_FOUND` into `INVALID_ARGUMENT` changes what they do.

**Fix.** Declare a new reason for the new code.

### DEDO_AUDIENCE_NO_NARROW

**Checks** that a service's `audience` does not narrow, as from `AUDIENCE_EXTERNAL_PUBLIC` to `AUDIENCE_COMPANY_INTERNAL`. Widening passes.

**Why.** Callers outside the new audience were admitted before, and are shut out after.

**Fix.** Keep the audience, or move what must be narrower to a service of its own.

### DEDO_TENANCY_SAME

**Checks** that a service's `tenancy` does not change: it is not added, removed, or moved to another collection.

**Why.** The tenant is the root of every resource name and request path ([multi-tenant rules](#multi-tenant-rules)), so changing it changes all of them.

**Fix.** Change tenancy in a new major version of the package.

### DEDO_CROSS_TENANT_NO_DELETE

**Checks** that an RPC marked `cross_tenant` stays so.

**Why.** Without it the runtime checks the request's tenant, so a call that spans tenants, or names none, fails.

**Fix.** Keep it, or replace the RPC in a new major version.

### DEDO_HTTP_BINDING_SAME

**Checks** that every REST binding of an RPC, its `google.api.http` rule and each additional binding, stays at the same verb and path, with the same `body` and `response_body`. New bindings pass.

**Why.** REST clients call the path and send and read the shape the binding names. Buf compares no `google.api.http` option, so a moved path passes `FILE`.

```protobuf
// Before:
option (google.api.http) = {get: "/v1/{name=shelves/*/books/*}"};
// After, passes: the new path joins the old one.
option (google.api.http) = {
  get: "/v1/{name=shelves/*/books/*}"
  additional_bindings: {get: "/v1/{name=books/*}"}
};
```

**Fix.** Keep the old binding, as an `additional_bindings` entry if a new path joins it.

### DEDO_FIELD_NO_NEW_REQUIRED

**Checks** that no field becomes `REQUIRED` in `google.api.field_behavior`. A new field may be `REQUIRED`, and an existing one may stop being so.

**Why.** Callers that leave the field out, as they could before, start failing (AIP-180).

**Fix.** Keep the field optional, or add a new field.

### DEDO_RESOURCE_SAME

**Checks** that a resource keeps its `type` and every `pattern` it had. Adding a pattern passes.

**Why.** Clients store resource names and refer to resources by type. A dropped pattern leaves stored names that no longer resolve; a new type breaks every `resource_reference` to the old one.

**Fix.** Keep the type and the old pattern, and add the new pattern beside it.

### DEDO_SUNSET_NO_EARLIER

**Checks** that a deprecated RPC's `sunset_time` does not move earlier. Moving it later, or removing the deprecation, passes.

**Why.** Callers plan their migration by the sunset they were told.

**Fix.** Keep the date, or move it later.

## Multi-tenant rules

The `DEDO_MULTI_TENANT_*` rules are for services that partition their data between tenants: a SaaS's customers, a platform's sub-accounts or business locations. They are a rule set of their own, `DEDO_MULTI_TENANT`, which is not part of `DEDO`, so they run only where `dedo.yaml`'s `lint.use` lists it. `dedo init` adds it when a service declares tenancy. `DEDO_MULTI_TENANT_REQUEST_TENANT` is final, so it runs either way.

```yaml
# dedo.yaml
lint:
  use:
    - AIP_NOLANG
    - ZALANDO
    - DEDO
    - HOUSE
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
