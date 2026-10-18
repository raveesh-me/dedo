# House rules

The house rules are the lint rules the Buf built-ins and the AIP linter lack. They run under the `HOUSE` category of the `buf-plugin-raveesh-me-api` check plugin:

```yaml
lint:
  use:
    - HOUSE
```

Every finding links to its section below. A release of the plugin links to this file as it was at that release's tag, so the text always matches the rule that reported it. Development builds link to `main`.

To suppress one finding, put `// buf:lint:ignore <RULE_ID>` above the element, followed by the reason. Reviewers treat each suppression as a decision to justify.

| Rule | Checks |
| --- | --- |
| [`HOUSE_SERVICE_RULES_REQUIRED`](#house_service_rules_required) | Services declare an audience and an owner team |
| [`HOUSE_METHOD_RULES_REQUIRED`](#house_method_rules_required) | RPCs declare scopes and error reasons |
| [`HOUSE_SCOPE_NAME_FORMAT`](#house_scope_name_format) | Scopes are `<app>.<resource>.<read\|write>` |
| [`HOUSE_ERROR_REASON_FORMAT`](#house_error_reason_format) | Error reasons are UPPER_SNAKE_CASE with a meaningful code |
| [`HOUSE_CACHE_REQUIRES_NO_SIDE_EFFECTS`](#house_cache_requires_no_side_effects) | Only read-only RPCs are cached, with a visibility and a max age |
| [`HOUSE_DEPRECATION_CONSISTENT`](#house_deprecation_consistent) | Deprecations have a complete, ordered schedule and a real successor |
| [`HOUSE_READ_METHOD_NO_SIDE_EFFECTS`](#house_read_method_no_side_effects) | Get and List RPCs are marked `NO_SIDE_EFFECTS` |
| [`HOUSE_HTTP_RESPONSE_BODY`](#house_http_response_body) | Get, Create and Update return the bare resource over REST |
| [`HOUSE_HTTP_PATH_VERSION`](#house_http_path_version) | REST paths carry the version after one root per service |
| [`HOUSE_NO_JSON_NAME`](#house_no_json_name) | No custom JSON field names |
| [`HOUSE_MONEY_FIELD_TYPE`](#house_money_field_type) | Money fields use `google.type.Money` |
| [`HOUSE_TOOL_REQUIRED`](#house_tool_required) | RPCs declare their MCP tool and REST docs text |
| [`HOUSE_MESSAGE_DESCRIPTION_REQUIRED`](#house_message_description_required) | Messages declare a consumer-facing description |

---

## HOUSE_SERVICE_RULES_REQUIRED

**Checks** that every service sets `raveesh_me.http_proto_lint.v1.service` with an `audience` other than `AUDIENCE_UNSPECIFIED` and a non-empty `owner_team`.

**Why.** Every API has an owner and an intended audience. The REST docs publish both, and the runtime refuses to start a service without the option.

```protobuf
// Fails: no service option.
service ProductService {}

// Passes.
service ProductService {
  option (raveesh_me.http_proto_lint.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "pim-team"
  };
}
```

**Fix.** Add the option. Use the team's name in the team registry.

## HOUSE_METHOD_RULES_REQUIRED

**Checks** that every RPC sets `raveesh_me.http_proto_lint.v1.method` with at least one scope and at least one error reason.

**Why.** Scopes are the RPC's authorization: without one, the endpoint would be open. The runtime fails closed and will not start. Error reasons are the RPC's error contract: the docs list them, and the runtime logs any reason the RPC returns without declaring it.

```protobuf
// Fails: no scopes, no errors.
rpc GetProduct(GetProductRequest) returns (GetProductResponse);

// Passes.
rpc GetProduct(GetProductRequest) returns (GetProductResponse) {
  option (raveesh_me.http_proto_lint.v1.method) = {
    scopes: "pim.product.read"
    errors: {reason: "PRODUCT_NOT_FOUND", code: NOT_FOUND}
  };
}
```

**Fix.** Declare every scope that grants access (a caller needs one) and every error reason the handler returns.

## HOUSE_SCOPE_NAME_FORMAT

**Checks** that every scope matches `<app>.<resource>.<read|write>`, in lowercase with optional underscores, for example `pim.product.read`.

**Why.** Scopes appear in tokens issued by the identity platform. One format lets a reviewer and the identity platform tell what a scope grants.

```protobuf
scopes: "pim.read"            // Fails: no resource.
scopes: "pim.product.delete"  // Fails: the verb is read or write.
scopes: "pim.product.write"   // Passes.
```

**Fix.** Rename the scope. Deletes, updates and creates are all `write`.

## HOUSE_ERROR_REASON_FORMAT

**Checks** that every declared error reason is UPPER_SNAKE_CASE, and that its code is neither `OK` nor `UNKNOWN`.

**Why.** The reason is the machine-readable part of an error (`google.rpc.ErrorInfo.reason`). Clients branch on it, so it must be stable and readable. `OK` is not an error, and `UNKNOWN` tells the client nothing.

```protobuf
errors: {reason: "product_not_found", code: NOT_FOUND}  // Fails: case.
errors: {reason: "PRODUCT_NOT_FOUND", code: UNKNOWN}    // Fails: code.
errors: {reason: "PRODUCT_NOT_FOUND", code: NOT_FOUND}  // Passes.
```

**Fix.** Rename the reason, or pick the code that describes the failure. `examples/todo/app/errors.go` shows how to return the reason in an `ErrorInfo`.

## HOUSE_CACHE_REQUIRES_NO_SIDE_EFFECTS

**Checks** that a `cache` policy appears only on RPCs with `idempotency_level = NO_SIDE_EFFECTS`, and that it sets a `visibility` and a positive `max_age`.

**Why.** The runtime sets `Cache-Control` from the policy. Caching a response that changes state is a bug, and a policy without visibility or max age has no meaning.

```protobuf
// Fails: CreateProduct changes state.
rpc CreateProduct(CreateProductRequest) returns (CreateProductResponse) {
  option (raveesh_me.http_proto_lint.v1.method) = {
    cache: {visibility: VISIBILITY_PUBLIC, max_age: {seconds: 60}}
  };
}

// Passes.
rpc GetProduct(GetProductRequest) returns (GetProductResponse) {
  option idempotency_level = NO_SIDE_EFFECTS;
  option (raveesh_me.http_proto_lint.v1.method) = {
    cache: {visibility: VISIBILITY_PRIVATE, max_age: {seconds: 30}}
  };
}
```

**Fix.** Remove the cache policy from RPCs that change state. Otherwise complete it.

## HOUSE_DEPRECATION_CONSISTENT

**Checks** that `deprecated = true` and a `deprecation` schedule appear together; that the schedule has both times, with the sunset after the deprecation; and that the `successor` names an RPC that exists.

**Why.** The runtime sends `Deprecation` and `Sunset` headers from the schedule, and the docs point callers to the successor. A half-declared deprecation sends wrong dates or a dead link.

```protobuf
// Passes.
rpc RemoveProduct(RemoveProductRequest) returns (RemoveProductResponse) {
  option deprecated = true;
  option (raveesh_me.http_proto_lint.v1.method) = {
    deprecation: {
      deprecation_time: {seconds: 1767225600}
      sunset_time: {seconds: 1798761600}
      successor: "acme.pim.v1.ProductService.DeleteProduct"
    }
  };
}
```

**Fix.** Set both, with the sunset after the deprecation. Give the successor's fully qualified name: `<package>.<Service>.<Rpc>`.

## HOUSE_READ_METHOD_NO_SIDE_EFFECTS

**Checks** that unary RPCs named `Get*` or `List*` (followed by an upper-case letter) set `idempotency_level = NO_SIDE_EFFECTS`.

**Why.** The level tells Connect to allow HTTP GET, which makes the response cacheable. It also tells MCP clients the tool is read-only, and lets the runtime set cache headers.

```protobuf
rpc ListProducts(ListProductsRequest) returns (ListProductsResponse);  // Fails.

rpc ListProducts(ListProductsRequest) returns (ListProductsResponse) {  // Passes.
  option idempotency_level = NO_SIDE_EFFECTS;
}
```

**Fix.** Add the option. If the RPC does change state, rename it.

## HOUSE_HTTP_RESPONSE_BODY

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

## HOUSE_HTTP_PATH_VERSION

**Checks** that every HTTP path, additional bindings included, contains the package's version as a segment, such as `/v1/`. Everything before the version (the root) must be fixed text, and the same for every RPC of the service.

**Why.** The version in the path follows the proto package: a breaking change is a new package and a new path. The root, such as `/commerce/pim` or `/commerce/products`, is the owning platform's choice, and the rule does not judge it. It only keeps one service from spreading over several roots.

```protobuf
get: "/{name=products/*}"                     // Fails: no version.
get: "/commerce/{tenant}/v1/products"         // Fails: variable in the root.
get: "/commerce/pim/v1/{name=products/*}"     // Passes.
delete: "/commerce/products/v1/{name=products/*}"  // Fails in the same service: different root.
```

**Fix.** Put `/<version>/` after the root, and use the same root for every RPC of the service. Write each root segment as one lowercase or lowerCamelCase word, such as `/commerce/timeCounter`: AIP-127's path syntax check rejects hyphens.

## HOUSE_NO_JSON_NAME

**Checks** that no field changes its JSON name with `json_name`.

**Why.** JSON uses the protojson default, lowerCamelCase of the field name. A custom name renames the field for every JSON client, in REST, Connect JSON and MCP, away from the name in the schema. Every server and client would then need a codec option.

```protobuf
string title = 1 [json_name = "todoTitle"];  // Fails.
string title = 1;                            // Passes: JSON name "title".
```

**Fix.** Remove `json_name`. To change the name, rename the field.

## HOUSE_MONEY_FIELD_TYPE

**Checks** that fields named `price` or `amount`, or ending in `_price` or `_amount`, use `google.type.Money`.

**Why.** Floating-point money loses cents, and a bare number has no currency.

```protobuf
double price = 1;             // Fails.
google.type.Money price = 1;  // Passes.
```

**Fix.** Use `google.type.Money`. If the field is not money, rename it.

## HOUSE_TOOL_REQUIRED

**Checks** that every unary RPC sets `tool` in its method option, with a `title` and a `description` that does more than repeat the title or the RPC name. RPCs named `Delete*` or `Remove*` must set `destructive: true`. Streaming RPCs are exempt, because MCP cannot expose them.

**Why.** Every RPC is published as an MCP tool. The title and description are what an AI agent reads to pick a tool, and they are also the operation's summary and description in the REST docs. `destructive` tells MCP clients to confirm before calling.

```protobuf
// Passes.
option (raveesh_me.http_proto_lint.v1.method) = {
  tool: {
    title: "Delete a product"
    description: "Removes a product for good. Its variants are removed too."
    destructive: true
  }
};
```

**Fix.** Write the title as a short action ("Get a product"). Write the description for someone choosing between operations: what it does and when to use it.

## HOUSE_MESSAGE_DESCRIPTION_REQUIRED

**Checks** that every message, nested ones included, sets `raveesh_me.http_proto_lint.v1.message` with a non-empty `description`. Map entries are exempt.

**Why.** Request messages are the input schemas of MCP tools, and every message is a schema in the REST docs. The description is what agents and API readers see. Proto comments stay for the people who maintain the API.

```protobuf
// Passes.
message GetProductRequest {
  option (raveesh_me.http_proto_lint.v1.message) = {description: "Which product to get."};
}
```

**Fix.** Add the option. Describe what the message represents for a caller.
