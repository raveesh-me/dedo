# Forking dedo

An organisation that wants to own every part of dedo its services run, and review each upstream change before it reaches them, can fork it. This guide lists what a fork owns, how its services install it, whether you make the fork public or private, where the options module goes in your own BSR, and how the fork keeps taking upstream releases.

`scripts/fork.sh` does the renaming. Run it once when you fork, and again after every merge from upstream.

## Do you need a fork?

Your conventions don't need one. House rules, the agent annexure, rule overrides and rule opt-outs all live in `dedo.yaml`, outside dedo ([RULES.md](RULES.md#house-rules)). Integrity doesn't need one either: what you install is already pinned.

| You want | Without a fork | With a fork |
| --- | --- | --- |
| The bytes you installed never change | `go.sum` and the public checksum database pin every Go module, `buf.lock` pins the options module's digest, and house rules are pinned by SHA-256 | The same, from your own repository |
| Builds that work if upstream disappears | A Go module proxy of your own (Artifactory, Athens) keeps every version you fetched | Yes |
| A person you trust reviews each change before any service gets it | No | Yes: a release reaches services only after you merge and tag it |
| Fixes on your own schedule | No | Yes, though they belong upstream too |
| Your own BSR module and plugin, private if you like | No | Yes |

## What a fork owns

A fork changes names, never behaviour. These are the names `scripts/fork.sh` changes:

| Asset | Upstream | Named in |
| --- | --- | --- |
| Go modules | `github.com/raveesh-me/dedo`, and `/cmd/dedo`, `/lint`, `/mcp` and `/openapi` under it | every `go.mod`, `go.work`, every import, and the generator paths `dedo init` pins (`cmd/dedo/init.go`) |
| Commands that `dedo` installs | `cmd/dedo` and `lint/cmd/buf-plugin-dedo-lint` | `cliPkg` (`cmd/dedo/update.go`), `lintPluginPkg` and `rulesVar` (`cmd/dedo/tools.go`) |
| Go package of the options | `github.com/raveesh-me/dedo/gen/go/dedo/v1` | `go_package` in `proto/dedo/v1/options.proto`, the managed-mode override in `buf.gen.yaml` files, and the generated code, which is regenerated |
| Options module on the BSR | `buf.build/r-a/dedo` | `buf.yaml`'s `name`, `templates/buf.yaml`, `optionsModule` (`cmd/dedo/init.go`) |
| Wasm lint plugin on the BSR | `buf.build/r-a/dedo-lint` | the release job, `remotePlugin` (`cmd/dedo/init.go`) |
| The MCP Go SDK build services replace the SDK with | `github.com/raveesh-me/go-sdk` | `sdkFork` (`cmd/dedo/sdkfork.go`), the examples' `go.mod`, the release check |
| Links in lint findings | `github.com/raveesh-me/dedo/blob/<tag>/guidance/RULES.md` | `docsBase` (`lint/rules/docs.go`), `DEDO_LDFLAGS` in the `Makefile` |
| Release | the `r-a` BSR organization, public visibility | `.github/workflows/ci.yaml`, the `BUF_KEY` secret, `scripts/check-release-versions.sh` |

What stays the same, so that no service changes a `.proto` file: the proto package `dedo.v1` and its file `dedo/v1/options.proto`, every option name and extension number, every rule ID, the `dedo.yaml` format, and the names of the binaries.

The fork renames the Go modules rather than pointing upstream's paths at itself with `replace`, because `go install pkg@version` ignores `go.work` and refuses a module whose `go.mod` has a `replace` directive. A renamed module installs like any other.

A service builds against upstream dedo or the fork, never both: each registers `dedo/v1/options.proto` with the protobuf runtime, and the second registration panics at startup. Every library a service links must make the same choice.

## Making the fork

1. Copy the MCP Go SDK build, with its tags, to your organisation, private or public (it is the upstream SDK plus one fix, with nothing of yours in it):

   ```sh
   git clone --mirror https://github.com/raveesh-me/go-sdk.git
   cd go-sdk.git && git push --mirror https://github.com/acme/go-sdk.git
   ```

   Its `go.mod` keeps `module github.com/modelcontextprotocol/go-sdk`, which is how a `replace` works: don't rename it. The copy is temporary. When an SDK release has the fix ([go-sdk#1454](https://github.com/modelcontextprotocol/go-sdk/pull/1454)), upstream dedo drops the replace, and so does your next sync.

2. Create `acme/dedo` and push upstream into it. GitHub's fork button makes a public fork of a public repository, so for a private fork create an empty private repository instead:

   ```sh
   git clone https://github.com/raveesh-me/dedo.git && cd dedo
   git remote rename origin upstream
   git config remote.upstream.tagOpt --no-tags
   git remote add origin https://github.com/acme/dedo.git
   git push -u origin main
   ```

   The fork tags its releases with upstream's version numbers on its own commits, so upstream's tags must never land in it as tags. `tagOpt --no-tags` keeps `git fetch upstream` from bringing them; [Taking upstream releases](#taking-upstream-releases) fetches them under `refs/upstream-tags/` instead.

3. Rename, test, and open a pull request:

   ```sh
   git fetch upstream '+refs/tags/*:refs/upstream-tags/*'
   git switch -c fork-v0.22.0 refs/upstream-tags/v0.22.0
   GO_MODULE=github.com/acme/dedo SDK_MODULE=github.com/acme/go-sdk \
     BSR_OWNER=buf.build/acme BSR_VISIBILITY=private scripts/fork.sh
   make test
   git commit -am "chore: fork dedo v0.22.0 as acme/dedo"
   ```

   The script stops if any upstream name is left. It never edits generated code: a `.pb.go` file carries `go_package` inside a length-prefixed descriptor, so editing it makes the package panic at init. It regenerates instead.

4. Set up the repository: a `BUF_KEY` secret (a BSR token for your organisation that can create and push modules and plugins), branch and tag protection ([Supply-chain checklist](#supply-chain-checklist)), and, for a private fork, Go's access to it in CI ([Installing if your fork is private](#installing-if-your-fork-is-private)). Turn off Dependabot in the fork: upstream already takes its updates, and you get them in the next sync.

5. Release as upstream does, with upstream's version number: push the module tags `cmd/dedo/v0.22.0`, `lint/v0.22.0`, `mcp/v0.22.0` and `openapi/v0.22.0` first, at most three per push, then `v0.22.0` on its own. The `v` tag runs the release job, which pushes the options module and the Wasm plugin to your BSR and attaches the binaries to a GitHub release. GitHub starts no workflow when more than three tags arrive in one push, and in a public fork the Go proxy caches a lookup of a version whose module tags aren't pushed yet as "not found" for a while.

Keep the fork's version numbers equal to upstream's, so a version means the same code in both. A fix you can't wait for upstream to release goes out as a pre-release of the next patch, such as `v0.22.1-acme.1` for a fix on top of `v0.22.0`. Semantic versioning orders it after `v0.22.0` and before upstream's `v0.22.1`, which supersedes it. (A pre-release of `v0.22.0` itself would sort before `v0.22.0`, so Go would treat the fix as older than the release it fixes.) Release it like any other version: a release pull request that sets every pin `scripts/check-release-versions.sh` checks to `v0.22.1-acme.1`, then the same five tags. `go install …@latest` and `dedo update` skip pre-releases while a release exists, so install it by name with `dedo update -version v0.22.1-acme.1`, then run `dedo upgrade` in each service as usual. Send the fix upstream too. When you next sync, check that upstream's release has the fix: `-X theirs` takes upstream's side wherever its lines conflict with the fix.

## Installing if your fork is public

Whether the fork is public or private is your decision: this section and the next say what each one takes.

Nothing to configure. `proxy.golang.org` fetches and caches the fork's modules, `sum.golang.org` records each version's checksum in its public, append-only log, and anyone installs it as they install upstream, with the fork's path:

```sh
go install github.com/acme/dedo/cmd/dedo@v0.22.0
```

The checksum database makes a public fork tamper-evident: once a version has been fetched, nobody, including you, can change what that version contains. A private fork has to trust its tag protection for the same guarantee.

## Installing if your fork is private

Go fetches a private module straight from git, with your git credentials, once `GOPRIVATE` names it. `GOPRIVATE` also keeps its path away from the public proxy and checksum database, which can't fetch it and must not learn of it. Set both on every machine and in every CI job that installs the `dedo` command, runs `dedo tools` or `dedo upgrade`, or builds a service, since `go tool` and the runtime libraries come from the same modules:

```sh
go env -w GOPRIVATE=github.com/acme/*
gh auth setup-git                      # or, with SSH keys:
# git config --global url."git@github.com:acme/".insteadOf "https://github.com/acme/"

go install github.com/acme/dedo/cmd/dedo@v0.22.0
dedo tools                             # installs the lint plugin from the fork too
```

In GitHub Actions, the job's own `GITHUB_TOKEN` reads only its own repository. Give CI a GitHub App with read access to `acme/dedo` and `acme/go-sdk` alone, and mint a short-lived token per job:

```yaml
env:
  GOPRIVATE: github.com/acme/*
steps:
  - uses: actions/create-github-app-token@v2
    id: dedo
    with:
      app-id: ${{ vars.DEDO_READER_APP_ID }}
      private-key: ${{ secrets.DEDO_READER_KEY }}
      owner: acme
      repositories: dedo,go-sdk
  - run: git config --global url."https://x-access-token:${{ steps.dedo.outputs.token }}@github.com/acme/".insteadOf "https://github.com/acme/"
  - run: go install github.com/acme/dedo/cmd/dedo@v0.22.0 && dedo tools && buf lint
```

The fork's own release job needs the same: its release check reads `acme/go-sdk`'s tags, and its examples build against it. In a Docker build, pass the token as a build secret (`RUN --mount=type=secret`), never as a build argument or a file in a layer.

With many repositories, a Go module proxy of your own scales better than git tokens everywhere. Point `GOPROXY` at it, let it fetch `github.com/acme/*` with its own credentials, and set `GONOSUMDB=github.com/acme/*` (not `GOPRIVATE`, which would bypass the proxy too).

The public checksum database never sees a private module, so a service's committed `go.sum` is the only record of what each release contained. If anyone moves a tag in the fork, builds fail on a checksum mismatch. Protect the tags so that never needs to happen.

`go install`, `dedo update`, `dedo tools` and `go tool` all honour these settings; dedo asks for nothing of its own.

## The options module in your own BSR

The annotations, `dedo/v1/options.proto`, reach services as a BSR module that each `buf.yaml` pins to the release: `buf.build/acme/dedo:v0.22.0`. dedo reads that label to know the release, so `dedo tools` installs the lint plugin to match and `dedo upgrade` moves both together. Choose where the module lives:

- **A module in your organisation on buf.build** (what `scripts/fork.sh` sets up). The release job creates it on the first push, with the visibility you chose, and labels each release and `main`. A private module needs a BSR plan that allows them; every machine that runs `buf dep update`, `buf lint` or `buf generate` then needs a BSR token with read access (`buf registry login`, or `BUF_TOKEN` in CI).
- **Your own BSR instance**, such as `buf.acme.com`. Set `BSR_OWNER=buf.acme.com/platform`, and services depend on `buf.acme.com/platform/dedo:v0.22.0`. `buf registry login buf.acme.com` gives each machine access.
- **No BSR.** Copy `proto/dedo` from the fork's release into each repository and list it as a module of its `buf.yaml` workspace, as dedo's examples do (`examples/todo/third_party/dedo`). It works, but `buf.yaml` no longer names a release: `dedo tools` installs the plugin of the `dedo` command you run, and `dedo upgrade` doesn't refresh the copy. Copy it again on every upgrade, and have CI compare it with the fork's tag.

A service on the fork's BSR module has these in its Buf files. `dedo init` and `dedo upgrade` from the fork write them:

```yaml
# buf.yaml
deps:
  - buf.build/acme/dedo:v0.22.0
```

```yaml
# buf.gen.yaml, with managed mode
managed:
  override:
    - file_option: go_package_prefix
      module: buf.build/acme/dedo
      value: github.com/acme/dedo/gen/go
```

## Moving a service to the fork

In the service's repository, with the fork's `dedo` installed (it replaces upstream's `dedo` in `$GOBIN`: the binary has the same name):

```sh
git grep -lzE 'raveesh-me/(dedo|go-sdk)|buf\.build/r-a/dedo' -- . ':!*.pb.go' | xargs -0 perl -pi -e '
  s{github\.com/raveesh-me/dedo}{github.com/acme/dedo}g;
  s{github\.com/raveesh-me/go-sdk}{github.com/acme/go-sdk}g;
  s{buf\.build/r-a/dedo}{buf.build/acme/dedo}g'
dedo upgrade        # buf dep update, regenerates, go mod tidy, vendors the fork's plugin
make check test
```

That rewrites `go.mod` (the runtime, the generator tools and the SDK replace), the imports in your own code, `buf.yaml`, `buf.gen.yaml` and the guidance in `AGENTS.md`; `dedo upgrade` regenerates the rest. Move a service's libraries in the same change: a binary that links both upstream and the fork panics at startup.

## Taking upstream releases

The fork's only change is the renaming, so a sync is a merge that prefers upstream's lines, then the script again:

```sh
git fetch upstream '+refs/tags/*:refs/upstream-tags/*'
git switch -c sync-v0.23.0 origin/main
git merge -X theirs --no-edit refs/upstream-tags/v0.23.0
GO_MODULE=github.com/acme/dedo SDK_MODULE=github.com/acme/go-sdk \
  BSR_OWNER=buf.build/acme BSR_VISIBILITY=private scripts/fork.sh
make test
git commit -am "chore: sync upstream v0.23.0"
git diff refs/upstream-tags/v0.23.0 -- . ':!*.pb.go'   # names only
```

Where a line conflicts, upstream's is the one you want, renamed, and the script renames it. The last diff should show nothing but names and the BSR visibility. Open a pull request, have it reviewed, merge it, and release with upstream's number (step 5 of [Making the fork](#making-the-fork)).

If the release moves services to a new SDK build (`sdkForkRelease` in `cmd/dedo/sdkfork.go` names a new tag), mirror that tag to `acme/go-sdk` first; the release check fails until it exists.

Review the upstream changes before you merge them, and review them as code that runs with your access:

```sh
git diff refs/upstream-tags/v0.22.0 refs/upstream-tags/v0.23.0 --stat
```

| Path | Why it matters |
| --- | --- |
| `cmd/dedo` | runs `go`, `buf` and `make` on developers' machines and in CI |
| `lint` | runs in every service's CI and editor |
| `mcp`, `openapi`, `templates` | write code and config into every service |
| `guidance` | goes into every service's `AGENTS.md` and `CLAUDE.md`, which coding agents follow: read it as instructions to your agents |
| `.github/workflows` | runs in the fork with its secrets |
| `go.mod`, `go.sum` | new or bumped third-party dependencies |

A scheduled workflow can watch for upstream tags and open the sync pull request; the review stays with a person.

## Supply-chain checklist

- Protect `main`: pull requests only, with a required review from the people who own the fork (`CODEOWNERS`).
- Protect tags with a ruleset: only release managers create `v*` and the module tags, and nobody moves or deletes one.
- Release only from the release job. Scope `BUF_KEY` to your BSR organisation, and the CI reader App to `contents: read` on `acme/dedo` and `acme/go-sdk`.
- Pin the fork's GitHub Actions to commit SHAs, and pin them again after each sync, since a merge takes upstream's lines.
- Commit `go.sum` and `buf.lock` in every service; CI fails when either would change.
- Run `govulncheck ./...` on the fork in CI.
- Keep the fork's delta to names, so every sync is the script and a review.
- The fork owns dedo, not its dependencies: Go modules still come through the Go proxy, and `googleapis` and `protovalidate` from buf.build. Mirror them through your own Go proxy or BSR if your policy requires it.
