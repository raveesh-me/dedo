#!/usr/bin/env bash
# Renames this repository to a fork's own Go modules and BSR names, then
# regenerates the generated code. Run it once when forking, and again after
# every merge from upstream: it is idempotent. guidance/forking.md explains
# the fork it makes.
#
#   GO_MODULE=github.com/acme/dedo \
#   SDK_MODULE=github.com/acme/go-sdk \
#   BSR_OWNER=buf.build/acme \
#   BSR_VISIBILITY=private \
#   scripts/fork.sh
#
# GO_MODULE      the fork's root Go module; cmd/dedo, lint, mcp and openapi
#                sit under it, as they do here
# SDK_MODULE     the fork's copy of the MCP Go SDK build that services
#                replace the SDK with (cmd/dedo/sdkfork.go)
# BSR_OWNER      the BSR owner of the options module and the Wasm lint
#                plugin, such as buf.build/acme or buf.acme.com/platform
# BSR_VISIBILITY optional: the visibility the release job creates them with
#
# It needs git, perl, go and buf.
set -euo pipefail

: "${GO_MODULE:?set GO_MODULE, such as github.com/acme/dedo}"
: "${SDK_MODULE:?set SDK_MODULE, such as github.com/acme/go-sdk}"
: "${BSR_OWNER:?set BSR_OWNER, such as buf.build/acme}"

# Upstream's names, in full and in the short forms that
# scripts/check-release-versions.sh matches.
go_from=github.com/raveesh-me/dedo
sdk_from=github.com/raveesh-me/go-sdk
bsr_from=buf.build/r-a
upstream='raveesh-me/(dedo|go-sdk)|r-a/dedo'

cd "$(git rev-parse --show-toplevel)"

# This script and the forking guide name upstream on purpose, and so does
# the changelog: its links are upstream's pull requests and releases.
keep=(':!scripts/fork.sh' ':!guidance/forking.md' ':!changelog/changelog.md')

# 1. Every name outside generated code. A .pb.go embeds go_package in a
#    length-prefixed descriptor: editing it corrupts the descriptor, and the
#    package panics at init. Step 3 regenerates it instead.
export go_from sdk_from bsr_from GO_MODULE SDK_MODULE BSR_OWNER
# shellcheck disable=SC2016 # perl, not the shell, reads $ENV
git grep -lzE "$upstream" -- . ':!*.pb.go' "${keep[@]}" | xargs -0 -r perl -pi -e '
  BEGIN {
    ($gf, $sf, $bf) = @ENV{qw(go_from sdk_from bsr_from)};
    ($gt, $st, $bt) = @ENV{qw(GO_MODULE SDK_MODULE BSR_OWNER)};
    ($gfs, $gts) = map { s{^[^/]+/}{}r } $gf, $gt;   # raveesh-me/dedo
    ($bfs, $bts) = map { s{^.*/}{}r } $bf, $bt;      # r-a
  }
  s{\Q$gf\E}{$gt}g; s{\Q$sf\E}{$st}g; s{\Q$bf/\E}{$bt/}g;
  s{\Q$gfs\E}{$gts}g; s{\Q$bfs/dedo\E}{$bts/dedo}g;
'

# 2. The visibility the release job creates the BSR module and plugin with.
if [ -n "${BSR_VISIBILITY:-}" ]; then
  perl -pi -e "s{--create-visibility \\w+}{--create-visibility ${BSR_VISIBILITY}}g" .github/workflows/ci.yaml
fi

# 3. Regenerate, so every descriptor carries the new go_package.
buf generate
for example in examples/*/; do
  if [ -f "${example}buf.gen.yaml" ]; then (cd "$example" && buf generate); fi
done
git ls-files -z '*.go' | xargs -0 gofmt -w

# 4. A name left over would install or import upstream's code.
if git grep -nE "$upstream" -- . "${keep[@]}"; then
  echo "scripts/fork.sh: upstream names remain (above)" >&2
  exit 1
fi
echo "renamed to $GO_MODULE, $SDK_MODULE and $BSR_OWNER; run make test before committing"
