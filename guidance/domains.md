# Modelling the domain: DDD and dedo's layers

An API built with dedo has layers: a repository, its proto packages, their services, the resources they manage, and the RPCs on those resources. Each layer is one of the building blocks of domain-driven design (DDD). Shape the API along that mapping, and the contract, the REST reference and the MCP tools all read in the language of the domain. Readers then navigate it the way the business thinks about it, not the way the code happens to be organised.

The terms come from Eric Evans' *Domain-Driven Design* (bounded context, module, aggregate, entity, value object, ubiquitous language, context map) and Vaughn Vernon's *Effective Aggregate Design* (small aggregates, reference by identity). The resource terms come from Google's AIPs, which dedo lints against.

## The layers

| DDD | dedo | Where it shows |
| --- | --- | --- |
| Domain | The organisation's business as a whole | The shared house rules and the root every path starts with (`/shop`) |
| Bounded context | One repository: one API module on the BSR | One REST root (`/shop/v1`), one OpenAPI document, one MCP server |
| Module | One proto package: `acme.orders.v1` | A navigation tag in the reference, holding the package's services |
| Aggregate | One service: `OrderService` | A tag in the reference, nested under its package's |
| Aggregate root | The resource the service manages: a message with `google.api.resource` | Its resource name (`orders/{order}`) is its identity and its REST path |
| Entity inside an aggregate | A child resource, its pattern nested under the root's (`orders/{order}/lines/{line}`) | A nested REST path |
| Value object | A message without `google.api.resource`: `google.type.Money`, an address | A schema, never a path |
| Command | An RPC that changes state: a standard method or a custom method (`:cancel`) | A `POST`, `PATCH` or `DELETE`; an MCP tool without `readOnlyHint` |
| Query | `Get` and `List`, declared `NO_SIDE_EFFECTS` | A `GET`; an MCP tool with `readOnlyHint` |
| Domain event | A server stream (`WatchOrders`), or messages the service publishes | A streaming RPC; not a REST path |
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
- **Keep modules acyclic.** One module may refer to another's resources by name, and import its messages where it must. Protobuf rejects import cycles, so a dependency in both directions shows up at compile time: it means the two packages are one module, or a third holds what both need.
- **Put shared value objects in a package of their own**, such as `acme.type`, with no services and no version. It gets no tag in the reference.
- **A context small enough for one module has one package.** Its reference lists its services without a package level.
- **The package is not in the REST path.** Paths keep one root per context, so regrouping modules does not move a REST path. It does rename the Connect and gRPC procedures, which carry the package, so `buf breaking` reports it as a breaking change. Settle the modules before the first release.

## Aggregates: one service each

An aggregate is a cluster of objects that changes as one, with one root that guards its rules. In dedo an aggregate is one service and the resource it manages.

- **One service per aggregate root**, named after it: `OrderService` manages `Order`. Give the service the plural noun readers look for as its `title` (`Orders`); the reference uses it as the service's heading.
- **Every RPC of the service acts on that aggregate.** An RPC is one transaction, so the aggregate is the consistency boundary: what must always hold true is enforced inside one RPC. A rule that spans two aggregates holds eventually, through events or a later call, not inside one transaction.
- **Keep aggregates small.** Put only what must be consistent at once inside the boundary. Large aggregates make for large requests, conflicting writes and slow pages.
- **Refer to another aggregate by its resource name, never by embedding its message.** Declare the field with `google.api.resource_reference`. Embedding couples two lifecycles and leaks the other aggregate's rules into this one. A multi-tenant service also checks the tenant of every referenced name.
- **Entities inside the aggregate are child resources**, with patterns nested under the root's. They change through the root's service, so the root can keep its rules.

## Commands and queries

- **Commands are named with the domain's verbs.** Use the standard methods (`Create`, `Update`, `Delete`) where they fit, and AIP-136 custom methods for transitions the business names: `CancelOrder` at `:cancel`, not `UpdateOrderStatus`. The RPC name becomes the MCP tool's name, so a model reads the same verb the business uses.
- **Queries have no side effects.** Declare `Get` and `List` with `idempotency_level = NO_SIDE_EFFECTS`; dedo caches them as declared and marks their MCP tools read-only.
- **Domain events leave the aggregate after the transaction.** A `Watch` stream or published messages tell other aggregates and contexts what happened; they act on it in transactions of their own.

## Between contexts: the context map

- **Published language.** The API module on the BSR is the language other contexts speak to this one. Consumers depend on a released label, and `buf breaking` keeps released versions compatible.
- **Open host service.** REST, Connect, gRPC and MCP are one host service, all generated from the same contract.
- **Anti-corruption layer.** When the context calls a vendor or another context with a model of its own, an adapter inside the service translates between the two models. The vendor's model never appears in the proto: the contract speaks only this context's language.
- **Shared kernel.** dedo's options module and the `google.type` messages are shared by every context. Keep what contexts share this small: anything else shared between them belongs to one context, and the others refer to it.

## How the reference shows it

`protoc-gen-dedo-openapi` turns the layers into OpenAPI 3.2 tags. When the documented services span several packages, each package becomes a navigation tag (`kind: nav`) named after the package, so it cannot clash with a service's tag. Its `summary` is the package's last segment before the version, in sentence case (`acme.orders.v1` reads "Orders", `shop.tax_profile.v1` "Tax profile"), and its description is the package comment. Each service's tag names its package as its `parent`:

```json
"tags": [
  {"name": "acme.orders.v1", "summary": "Orders", "kind": "nav",
   "description": "Orders is what buyers commit to buy, and what they send back."},
  {"name": "Orders", "parent": "acme.orders.v1", "description": "OrderService reads orders."},
  {"name": "Returns", "parent": "acme.orders.v1", "description": "ReturnService reads returns."}
]
```

A reference that supports 3.2 shows context, module, aggregate and operation as four levels. The docs page that `dedo init` writes uses Scalar, which does. A viewer that ignores `parent` still lists every service, without the package level.

## Checklist

- One repository, one context, one REST root.
- One package per module, named with a domain noun and opened with a comment that says what it is responsible for.
- One service per aggregate, titled with the noun readers look for; every RPC acts on that aggregate.
- References to other aggregates are resource names with `resource_reference`, never embedded messages.
- Commands are named with the domain's verbs; queries are `NO_SIDE_EFFECTS`.
- Vendor and other contexts' models stay behind an adapter, out of the proto.
