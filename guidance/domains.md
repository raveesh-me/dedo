# Modelling the domain: DDD and dedo's layers

`dedo init` adds this file to your repository's `AGENTS.md` and `CLAUDE.md`, after [claude.md](https://github.com/raveesh-me/dedo/blob/main/guidance/claude.md), and `dedo upgrade` keeps it current.

An API built with dedo has layers: a repository, its proto packages, their services, the resources they manage, and the RPCs on those resources. Each layer is one of the building blocks of domain-driven design (DDD). Shape the API along that mapping, and the contract, the REST reference and the MCP tools all read in the language of the domain. Readers then navigate it the way the business thinks about it, not the way the code happens to be organised.

The terms come from Eric Evans' *Domain-Driven Design* (bounded context, module, aggregate, entity, value object, ubiquitous language, context map) and Vaughn Vernon's *Effective Aggregate Design* (small aggregates, reference by identity). The resource terms come from Google's AIPs, which dedo lints against.

## The layers

| DDD | dedo | Where it shows |
| --- | --- | --- |
| Domain | The organisation's business as a whole | The shared house rules and the root every path starts with (`/shop`) |
| Bounded context | One repository: one API module on the BSR | One REST root (`/shop/v1`), one OpenAPI document, one MCP server |
| Module | One proto package: `acme.orders.v1` | A navigation tag in the reference, holding the package's services; in the service, one Go package (`internal/orders/`) and one Postgres schema |
| Aggregate | One service: `OrderService` | A tag in the reference, nested under its package's |
| Aggregate root | The resource the service manages: a message with `google.api.resource` | Its resource name (`orders/{order}`) is its identity and its REST path |
| Entity inside an aggregate | A child resource, its pattern nested under the root's (`orders/{order}/lines/{line}`) | A nested REST path |
| Value object | A message without `google.api.resource`: `google.type.Money`, an address | A schema, never a path |
| Domain service | A service that manages no resource, holding only custom methods: `AddressService` | A tag in the reference; paths that start at the tenant (`locations/{location}:resolveAddress`) or at a collection noun (`addresses:resolveAddress`) |
| Command | An RPC that changes state: a standard method or a custom method (`:cancel`) | A `POST`, `PATCH` or `DELETE`; an MCP tool with `read_only_hint: false` |
| Query | `Get` and `List`, declared `NO_SIDE_EFFECTS` | A `GET`; an MCP tool with `read_only_hint: true` |
| Domain event | An event type a command declares in `emits` (`acme.orders.order.cancelled`), published outward to a bus | A line in the operation's description and `x-emits`; not a REST path |
| Ubiquitous language | Names, `title`s and leading comments | Every heading and description in the reference, and every MCP tool description |

## Bounded context: one repository, one root

A bounded context is the boundary inside which every term has one meaning. In dedo it is one repository.

- One repository holds one context: one API module on the BSR, one REST root (`/<root>/v1/...`), one OpenAPI document and one MCP server. Two contexts that share a repository share a module and a lint configuration, and their terms start to leak into each other.
- The context owns its storage. No other context reads its tables; they call its API.
- Within the context, every name means one thing. `Order` cannot mean a basket in one package and a binding sale in another. Keep the glossary where the product decisions live, and use exactly those words in the proto.

## Modules: one package each

A module groups the parts of the model that belong together, so that the model can be understood one part at a time. In dedo a module is a proto package, `<context>.<module>.v<major>`.

- **Name the package with a noun from the domain**: `orders`, `catalog`, `payments`. Never name it after a technical layer: `api`, `common`, `models`, `handlers` and `v1` alone say nothing about the domain.
- **Open the package with a comment that says what the module is responsible for**, directly above the `package` statement with no blank line between them. The reference shows the comment as the module's description:

  ```proto
  // Orders is what buyers commit to buy, and what they send back.
  package acme.orders.v1;
  ```

- **Group by cohesion, not by size.** The services of one package change together, for the same reasons. A package that holds unrelated services is two modules.
- **Keep modules acyclic.** One module may refer to another's resources by name, and import its messages where it must. A call to another module's service counts as a dependency, and so does an internal port or repository that reads its data, the same as an import. A cycle through any of them means what an import cycle means: the two packages are one module, or a third holds what both need.

  Buf catches only import cycles. A `resource_reference` field is a string, so it adds no import, and cycles through calls pass the build. In the Go layout below, a port is an import, so Go rejects a cycle through ports, and `dedo modules` checks calls through generated clients. If `catalog` calls `inventory` to show stock, and `inventory` calls `catalog` to refuse a discontinued product, buf passes, but the two depend on each other. To break the cycle, keep one direction and let the client or a later call combine the two; merge the modules; or move what both need into a third. An export or report that reads every module belongs in a module of its own, which nothing else depends on.
- **Put shared value objects in a package of their own**, such as `acme.type`, with no services and no version. It gets no tag in the reference.
- **A context small enough for one module has one package.** Its reference lists its services without a package level.
- **The package is not in the REST path.** Paths keep one root per context, so regrouping modules does not move a REST path. It does rename the Connect and gRPC procedures, which carry the package, so `buf breaking` reports it as a breaking change. Settle the modules before the first release.

### Code and data follow the modules

The service keeps the modules of its protos in its Go code and in its database. Each module's dependencies are then explicit, so the cycles that the bullet on keeping modules acyclic warns about fail a build, a check or a query, not only a review. A context `acme` with the packages `acme.orders.v1`, `acme.orders.v2` and `acme.catalog.v1` looks like this:

```text
internal/
  orders/              # module orders: acme.orders.v1 and acme.orders.v2
    handlers_v1.go     # adapters for acme.orders.v1
    handlers_v2.go     # adapters for acme.orders.v2
    orders.go          # the domain: aggregates and their rules
    port.go            # Port: what other modules may read
    store.go           # the only code that reads the orders schema
    migrations/
    queries/
  catalog/             # module catalog: acme.catalog.v1
    ...
  shared/
    money/             # shared helpers
app/                   # the composition root, with main
main.go
```

- **One Go package per proto module.** The packages `<ctx>.<m>.vN` of a context `<ctx>` put module `m`'s code at `internal/<m>/`: its handlers, domain, store, migrations and queries. A service with one module uses the same layout, so every service reads the same way.
- **Shared helpers go at `internal/shared/<x>/`.** `shared` is reserved, so no proto module may be called `shared`: `DEDO_PACKAGE_SHARED_RESERVED` rejects `acme.shared.v1`. Value objects that the protos share go in `<ctx>.type`, as above.
- **Everything stays under `internal/`**, so no other repository can import it. Other contexts call the API.
- **The composition root is the only code that imports several modules.** `main` and the `app/` package build each module's dependencies, run the migrations in dependency order and register the handlers.
- **Every version of a module shares one package.** During an AIP-185 migration, `<ctx>.<m>.v1` and `<ctx>.<m>.v2` are two API surfaces over one domain and one store. Their handlers are adapters in `internal/<m>/`, such as `handlers_v1.go` and `handlers_v2.go`, and moving from v1 to v2 is not a cycle. The module boundary is the domain, not the API version: both versions serve the same aggregates, keep the same rules and own the same data. Split into two packages, they would have to share all three.
- **Ports belong to their provider.** A module that reads another depends on the provider's own `Port` interface, such as `filing.Port`, which returns the provider's generated messages. The dependency is then a Go import, and Go rejects an import cycle at compile time. Never put ports in a shared package: it hides every edge, and a cycle through it compiles. Modules never subscribe to each other's events: one reads another through its `Port`.
- **Generated clients and messages are edges too.** A call through another module's generated client (`<m>v1connect`) or an import of its messages is a dependency. Go cannot see a cycle through them, since every module imports only generated code, so `dedo modules` checks them.
- **Each module owns a Postgres schema named after it.** Only its migrations and queries touch it. Shared infrastructure, such as migration bookkeeping, lives in a `shared` schema.
- **No foreign keys or views across modules.** A reference to another module's data is a resource name, as in the protos.
- **Each module runs as its own database role**, with privileges only on its own schema. A query that reaches into another module's tables then fails at runtime.

## Aggregates: one service each

An aggregate is a cluster of objects that changes as one, with one root that guards its rules. In dedo an aggregate is one service and the resource it manages.

- **One service per aggregate root**, named after it: `OrderService` manages `Order`. Give the service the plural noun readers look for as its `title` (`Orders`); the reference uses it as the service's heading. dedo doesn't lint this, but a house rule on a service's `roots` can ([RULES.md](RULES.md#house-rules)).
- **Every RPC of the service acts on that aggregate.** An RPC is one transaction, so the aggregate is the consistency boundary: what must always hold true is enforced inside one RPC. A rule that spans two aggregates holds eventually, through a later call to the other aggregate, not inside one transaction: the caller's, or that of an orchestrator an event notified.
- **Keep aggregates small.** Put only what must be consistent at once inside the boundary. Large aggregates make for large requests, conflicting writes and slow pages.
- **Refer to another aggregate by its resource name, never by embedding its message.** Declare the field with `google.api.resource_reference`. Embedding couples two lifecycles and leaks the other aggregate's rules into this one. A multi-tenant service also checks the tenant of every referenced name.
- **Entities inside the aggregate are child resources**, with patterns nested under the root's. They change through the root's service, so the root can keep its rules.

## Domain services: operations with no aggregate

Some operations belong to no aggregate and keep no state: resolving a free-text address, suggesting tariff codes for a product description, converting between currencies. DDD calls them domain services. In dedo a domain service is a service that manages no resource.

- **Name it after the noun readers look for.** `AddressService`, titled `Addresses`; `TariffCodeService`, titled `Tariff codes`. It declares no `google.api.resource` and has no standard methods, only custom methods named with the domain's verbs: `ResolveAddress`, `SuggestTariffCodes`.
- **In a multi-tenant service, hang its methods off the tenant.** AIP-136 attaches a stateless method to the resource that scopes it, with the whole RPC name after the `:`. Here that resource is the tenant. The request names the tenant in a field of its own, so the runtime checks the tenant as it does for any other call:

  ```proto
  rpc ResolveAddress(ResolveAddressRequest) returns (ResolveAddressResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
    option (google.api.http) = {get: "/shop/v1/{location=locations/*}:resolveAddress"};
  }

  message ResolveAddressRequest {
    // The location asking, such as locations/1.
    string location = 1 [
      (google.api.field_behavior) = REQUIRED,
      (google.api.resource_reference).type = "shop.example.com/Location"
    ];
    // The address as a person typed it.
    string text = 2 [(google.api.field_behavior) = REQUIRED];
  }
  ```

  A path without the tenant, such as `/shop/v1/addresses:resolveAddress`, fails `DEDO_MULTI_TENANT_REQUEST_TENANT`. Don't make the method `cross_tenant` to get past the rule: then only its scopes would guard it.
- **With one tenant, put its methods after a collection noun.** Such a service has no resource to scope the method, and a path straight after the version (`/shop/v1:resolveAddress`) fails `DEDO_HTTP_PATH_VERSION`. Use the plural noun of what the method is about. The custom verb in the path follows `AIP_0136_HTTP_URI_SUFFIX`: just the verb when the RPC name ends with the collection noun (`SuggestTariffCodes` at `/shop/v1/tariffCodes:suggest`), and the whole RPC name when it doesn't (`ResolveAddress` at `/shop/v1/addresses:resolveAddress`, not `addresses:resolve`).
- **One that only answers is a query.** Declare it `idempotency_level = NO_SIDE_EFFECTS`, with `read_only_hint: true`. Prefer `GET`, with the input in query parameters: Connect can then call it over GET too, and the method can declare a cache policy (`VISIBILITY_PRIVATE` in a multi-tenant service). Use `POST` with `body: "*"` when the input may not fit in a URL, such as a product description. The method stays `NO_SIDE_EFFECTS`.
- **Say whether it reaches outside the context.** A domain service often wraps a vendor behind an anti-corruption layer. One that writes to the vendor, for example to send a message or file a return, is a command, and always sets `open_world_hint: true`. One that only reads from a vendor, such as a geocoder, usually sets it too, since its answer comes from data the context does not own. It may set `false` when the answer is reference data the context could hold itself, such as codes from a published tariff schedule.
- **It never changes an aggregate.** In DDD a domain service may also coordinate a change across aggregates, such as moving money between two accounts. In dedo an RPC is one aggregate's transaction, so that change is not a domain service. Put the command on the aggregate it changes and let an orchestrator, notified by its event, call the other, or make the process a resource of its own (`transfers/{transfer}`) whose service drives the steps.
- **It lives in the module whose language it speaks**, beside the aggregates that use it. When several modules use it, it gets a module of its own, and they depend on it through its generated client or its `Port`, like any other module.

## Commands and queries

- **Commands are named with the domain's verbs.** Use the standard methods (`Create`, `Update`, `Delete`) where they fit, and AIP-136 custom methods for transitions the business names: `CancelOrder` at `:cancel`, not `UpdateOrderStatus`. The RPC name becomes the MCP tool's name, so a model reads the same verb the business uses.
- **Queries have no side effects.** Declare `Get` and `List` with `idempotency_level = NO_SIDE_EFFECTS`, and their tools' `read_only_hint: true`; `DEDO_TOOL_ANNOTATIONS` checks that the two agree. The runtime sets their cache headers from the cache policy they declare.
- **Domain events leave the aggregate after the transaction.** A command may announce what it did, and whoever reacts does so in a transaction of its own ([below](#domain-events-outward-only)).

## Domain events: outward only

A core service waits to be called. It never subscribes to a bus, polls another service or watches a stream. When it takes part in orchestration, it may announce what happened, outward, to a bus. Whatever reacts, an orchestrator or another context, runs on its own and calls APIs, this one's included.

- **An event is a notification, not a document.** It says that something happened to a resource, and nothing more: a type (`acme.orders.order.cancelled`), the resource's name as its subject (`orders/123`), an ID and a time. It has no payload. Consumers fetch the resource through the API, with their own token and scopes, so an event never carries data past a scope check. A duplicate or an event out of order costs a consumer one more fetch, since the fetch returns the current state. The ID stays the same when the bus redelivers an event, but a command the caller retries publishes a new one: a consumer that must not act twice decides from the resource's state, not from the event.
- **The command declares what it may announce**, in `emits`. The type is the package without its version, then the resource and the verb: `<context>.<module>.<resource>.<past-tense verb>`. Name the transition the business names (`cancelled`, `shipped`), not `updated`. `DEDO_EVENT_TYPES` checks the form, and the REST reference lists each operation's events. Most commands emit nothing: add an event when a consumer needs one. `buf breaking` does not compare option values, so renaming or dropping a type passes it and silently stops its consumers: treat a released type like a field, and add new ones rather than rename one.

  ```proto
  rpc CancelOrder(CancelOrderRequest) returns (Order) {
    option (dedo.v1.method) = {
      scopes: "orders.order.write"
      errors: {reason: "ORDER_NOT_FOUND", code: NOT_FOUND}
      errors: {reason: "EVENT_NOT_PUBLISHED", code: UNAVAILABLE}
      emits: "acme.orders.order.cancelled"
    };
  }
  ```

- **The handler publishes after the change commits**, through `events.Publisher` ([install.md](install.md#publishing-events)). The runtime refuses a type the RPC does not declare. The bus is a driver the service injects, as `database/sql` takes a database driver: dedo has no opinion on Pub/Sub, Kafka or anything else. Once the driver acknowledges an event, the bus delivers it at least once.
- **Until then, `Publish` retries within the request's deadline**, with exponential backoff. If the bus does not acknowledge in time, the handler returns the error, `UNAVAILABLE` with reason `EVENT_NOT_PUBLISHED`, and the caller retries the command. That closes the gap between the commit and the acknowledgement without the service holding any state, and needs the command to be safe to retry: idempotent, or a create that takes a `request_id` (AIP-155). A handler that may lose an event can log the error and succeed instead.
- **Nothing else carries events.** No outbox, no server stream of events, no webhooks or registered clients: each makes the core service hold state about its consumers. A consumer that needs a live feed, such as a browser, gets it from a gateway that reads the bus.

## Between contexts: the context map

- **Published language.** The API module on the BSR, with the event types its commands declare, is the language other contexts speak to this one. Consumers depend on a released label, and `buf breaking` keeps released versions compatible.
- **Open host service.** REST, Connect, gRPC and MCP are one host service, all generated from the same contract.
- **Anti-corruption layer.** When the context calls a vendor or another context with a model of its own, an adapter inside the service translates between the two models. The vendor's model never appears in the proto: the contract speaks only this context's language.
- **Shared kernel.** dedo's options module and the `google.type` messages are shared by every context. Keep what contexts share this small: anything else shared between them belongs to one context, and the others refer to it.

## How the reference shows it

`protoc-gen-dedo-openapi` turns the layers into OpenAPI 3.2 tags. When the documented services span several packages, each package becomes a navigation tag (`kind: nav`) named after the package. Its `summary` is the package's last segment before the version, in sentence case (`acme.orders.v1` reads "Orders", `shop.tax_profile.v1` "Tax profile"), and its description is the package comment. Each service's tag names its package as its `parent`:

```json
"tags": [
  {"name": "acme.orders.v1", "summary": "Orders", "kind": "nav",
   "description": "Orders is what buyers commit to buy, and what they send back."},
  {"name": "Orders", "parent": "acme.orders.v1", "description": "OrderService reads orders."},
  {"name": "Returns", "parent": "acme.orders.v1", "description": "ReturnService reads returns."}
]
```

Operations name their tag by name alone, so every tag name must be unique across the document: two services titled `Orders` in different packages fail generation, and one of them needs another title. A package's comment comes from the first of its files that has one. A service whose tag repeats its package's heading, so that the reference reads "Calculation › Calculation", draws a warning when it is not named after the one root it manages: it is probably named after its module. The warning names the root to call it after (`TaxQuoteService` titled "Tax quotes"). `OrderService` titled `Orders` in `acme.orders.v1` draws none: that is the layout this guide describes.

A reference that supports 3.2 shows context, module, aggregate and operation as four levels. The docs page that `dedo init` writes uses Scalar, which does. A viewer that ignores `parent` still lists every service, without the package level.

## Checklist

- One repository, one context, one REST root.
- One package per module, named with a domain noun and opened with a comment that says what it is responsible for. No module is called `shared`.
- One Go package per module at `internal/<m>/`, holding every version's handlers, its domain, store, migrations and queries; shared helpers at `internal/shared/<x>/`; only `main` and `app/` import several modules.
- A module reads another only through the provider's `Port` or its generated client, and the modules stay acyclic.
- One Postgres schema and one database role per module; no foreign keys or views across modules.
- One service per aggregate, titled with the noun readers look for; every RPC acts on that aggregate.
- An operation with no aggregate is a domain service: no resource, only custom methods, hung off the tenant in a multi-tenant service and off a collection noun otherwise. It changes no aggregate, and one that only answers is a `NO_SIDE_EFFECTS` query, over `GET` where the input fits in a URL.
- References to other aggregates are resource names with `resource_reference`, never embedded messages.
- Commands are named with the domain's verbs; queries are `NO_SIDE_EFFECTS`.
- A command that announces what it did declares each event type in `emits` and publishes it after the commit, outward only; events carry no payload, and the service never subscribes.
- Vendor and other contexts' models stay behind an adapter, out of the proto.
