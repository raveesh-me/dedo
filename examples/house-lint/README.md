# house-lint

An example of an organisation's own lint plugin: the `HOUSE_*` rules that sit next to `buf-plugin-dedo` (see [RULES.md](../../guidance/RULES.md#house-rules)).

In a real organisation this is a repository of its own, shared by every service: copy this directory, change the module path, and release it the way dedo is released (native binaries on a GitHub release, and `buf plugin push` of the Wasm build for Windows). Services pin its version and list it in `buf.yaml` next to `buf-plugin-dedo`. Here it stands in for that repository: `make plugins` at the repository root builds it into `dist/`, and both todo examples use it.

| Rule | Checks |
| --- | --- |
| `HOUSE_RESOURCE_TYPE_DOMAIN` | Every `google.api.resource` type lives under the organisation's domain, set by the plugin option `resource_domain` |

```yaml
plugins:
  - plugin: .tools/buf-plugin-house
    options:
      resource_domain: raveesh.me
lint:
  use:
    - HOUSE
```

Name every rule `HOUSE_<NAME>` and put it in the `HOUSE` category, so it never collides with dedo's `AIP_*`, `ZALANDO_*` and `DEDO_*` rules. A house rule may add constraints but must not contradict theirs.
