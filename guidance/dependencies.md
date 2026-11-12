# Choosing dependencies

Every dependency is code you ship and a party you trust: its maintainers, everything it depends on in turn, and whoever can publish a release under its name. Qualify a dependency before it reaches your repository, pin it so it changes only when you decide, and record in the pull request that adds it why it passed.

This applies to everything a build or a running service fetches: Go modules, buf modules, tools run with `go run` or `go tool`, CI actions, container images, a frontend's or BFF's npm packages, and scripts loaded from a CDN.

**Your organisation's policy wins.** dedo cannot know which licenses your organisation permits or who must approve an exception. Those belong in the annexure your `dedo.yaml` names ([RULES.md](RULES.md#house-rules)), which coding agents read after dedo's guidance. Where it and this page disagree, follow the annexure.

## Before you add one

Answer each question in the pull request that adds the dependency.

1. **Do you need it?** Check the standard library and your existing dependencies first. If the job is a few dozen lines you can write and test, write them: "A little copying is better than a little dependency" ([Go Proverbs](https://go-proverbs.github.io/)).
2. **Does it carry logic, or only forward calls?** Don't depend on a thin wrapper ([below](#dont-depend-on-thin-wrappers)).
3. **Is its license permitted?** Check it and every module it brings with it ([below](#licenses)).
4. **Is it maintained?** It should publish tagged, semantically versioned releases, not just commits on a branch that Go turns into pseudo-versions. Look for a release in the last year, or a library that is clearly complete. Issues and security reports should get answers. Prefer more than one maintainer, or an organisation behind it.
5. **Is it safe?** It should have no open advisories: run `govulncheck` ([below](#check-for-vulnerabilities)), and look it up on [deps.dev](https://deps.dev), which also shows its [OpenSSF Scorecard](https://scorecard.dev). It should run nothing at install time, such as an npm `postinstall` script, and use no cgo unless cgo is the point.
6. **What does it bring with it?** Count the Go modules before and after adding it: `go list -m all | wc -l`. Each one it adds is another dependency, and has to pass questions 3 and 5 too. When two candidates do the same job, take the one with fewer dependencies.

## Don't depend on thin wrappers

A thin wrapper is a package whose job is to rename, re-export or set defaults on another library, or to turn a service's API into a few functions. Examples:

- a package that re-exports OpenTelemetry's API under its own names;
- a logger that wraps `log/slog`;
- a client module whose job is a single HTTP call to another service.

Depending on one costs you twice:

- **Supply chain.** You get one more publisher who can ship code into your build. It is usually reviewed and pinned less carefully than the library it wraps, and it pulls that library in anyway, often with more besides.
- **Maintenance.** Its releases trail upstream's, so you wait on its owner for a fix or feature upstream already has. Its names hide upstream's documentation. You now keep two version trains compatible instead of one.

Every repository has an owner, and that owner can maintain a few lines of glue.

| Instead of | Do this |
| --- | --- |
| A wrapper around a library | Depend on the library directly, such as `go.opentelemetry.io/otel` and `log/slog` |
| A client module for a service's API | Call the API with `net/http` or the service's own generated client, and keep its request and response types in your repository. Generate them from its schema when it publishes one. |
| A bootstrap package that sets a library up | Write the setup into your own `main.go`, as `dedo init` does with the server, so it reads as code you own |
| A shared package that sets defaults | Copy the defaults into your configuration, with a comment saying where they came from |

The test: **does it carry logic you would otherwise have to write, test and keep correct?** That means a protocol, an algorithm, state or a policy. If it does, it is a library; qualify it like any other. If it only forwards calls, don't add it.

A vendor adapter in your own repository is different. Keeping a vendor's SDK behind an interface in your code isolates the vendor, and you own the adapter. The thing to avoid is a wrapper someone else publishes.

## Pin exact versions

A build should fetch the same bytes today and in a year. A dependency changes only in a pull request someone reviewed.

| Dependency | Pin it with |
| --- | --- |
| Go modules | `go.mod` names exact versions and `go.sum` their hashes: commit both. `go.mod`'s version is a minimum, so a newly added dependency can raise another one (minimal version selection). Read the whole `go.mod` diff, and never run `go get -u ./...` to update everything at once. |
| Go tools | `go get -tool <module>@vX.Y.Z` for tools the build runs, and `go run <module>@vX.Y.Z` for an occasional one. Never write `go install …@latest` in a Makefile, CI or a guide. |
| The Go toolchain | The `go` and `toolchain` lines of `go.mod` |
| buf modules | `buf.lock` pins each dependency's commit: commit it, and run `buf dep update` only in a pull request that says why |
| House rules and annexures | The `sha256` beside each `url` in `dedo.yaml`. Change it only to move to another release. |
| GitHub Actions | A full commit SHA, with the version in a comment: `uses: actions/checkout@<40-character SHA> # v7.0.0`. A tag can be moved. |
| Container images | A digest: `image: golang@sha256:<digest> # 1.26.6`. A tag can be moved. |
| npm packages | Exact versions in `package.json`, with no `^` or `~` (`save-exact=true` in `.npmrc`). Commit the lockfile and install with `npm ci`. |
| Scripts from a CDN | An exact version in the URL and an `integrity` (SRI) hash, as dedo's REST docs page loads Scalar |

Update on purpose, in a pull request of its own. Read the release notes between the two versions, then run the tests and `govulncheck`. A bot such as Dependabot or Renovate, set to propose exact pins, can open these pull requests; a person reviews and merges them.

## Licenses

dedo does not lint licenses: which ones are permitted is your organisation's decision, and its annexure says so. Policies usually sort licenses into four groups:

- **allowed**: usually the permissive ones, such as MIT, BSD and ISC;
- **allowed with conditions**: such as only when you use the library and don't resell it;
- **needs legal review**: weak copyleft, such as LGPL and MPL;
- **blocked**: strong copyleft, such as GPL and AGPL.

Check a dependency's license before you add it, and check every module it brings with it. A permissive library that depends on a blocked one is blocked.

- **Go:** [go-licenses](https://github.com/google/go-licenses) reports the license of every module a package builds with, and `check` fails on any outside a list:

  ```sh
  go run github.com/google/go-licenses/v2@v2.0.1 report ./...
  go run github.com/google/go-licenses/v2@v2.0.1 check ./... --allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,ISC
  ```

  Take the list from your organisation's policy. A license it allows only with conditions, or after review, stays off the list: approve each such module by hand, and record the approval in the pull request. `--ignore <path prefix>` skips your own modules.
- **Any ecosystem:** [pkg.go.dev](https://pkg.go.dev) shows the license it detects for each Go module, and [deps.dev](https://deps.dev) shows them for Go, npm and others.
- **Read the license file itself** when a detector reports `unknown` or several licenses, when a module is dual-licensed, or when it has a `NOTICE` file: Apache 2.0 requires the notices in it to go with anything you distribute that includes the module.
- **Check again on a major version.** A project can change its license between releases.

## Check for vulnerabilities

```sh
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

`govulncheck` reports only the vulnerabilities your code can reach, so its findings need action. Run it before adding a dependency, on every update, and on a schedule in CI, because advisories arrive after you pinned.

## In the pull request

A pull request that adds a dependency says:

```markdown
**Dependency:** github.com/example/lib v1.4.2 (MIT)
**For:** what it does here, and why the standard library or an existing dependency does not
**Alternatives:** what else was considered, including writing it ourselves
**Brings:** N new modules (`go list -m all`, before and after), their licenses checked
**Health:** last release, maintainers, advisories (govulncheck, deps.dev)
```

A reviewer who can't confirm one of these lines doesn't approve.
