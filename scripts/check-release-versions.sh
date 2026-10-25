#!/bin/sh
# Fails unless every version that users copy (the Makefile.api default and
# the pins in the docs) names the release being tagged. Run by the release
# job before anything is published.
#   scripts/check-release-versions.sh v0.4.1
set -eu
tag="$1"
status=0
check() {
  file="$1"; shift
  found=$(grep -oE "$@" "$file" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sort -u)
  for v in $found; do
    if [ "$v" != "$tag" ]; then
      echo "$file names $v, but the release is $tag" >&2
      status=1
    fi
  done
  if [ -z "$found" ]; then
    echo "$file names no version" >&2
    status=1
  fi
}
check guidance/install.md 'raveesh-me/dedo[a-z/-]*@v[0-9.]+'
check templates/buf.yaml 'r-a/dedo:v[0-9.]+'
check guidance/install.md 'r-a/dedo:v[0-9.]+'
check guidance/tutorial.md 'raveesh-me/dedo[a-z/-]*@v[0-9.]+'
check cmd/dedo/main.go 'const version = "v[0-9.]+"'
check guidance/guideline.md 'raveesh-me/dedo[a-z/-]*@v[0-9.]+'
check readme.md 'Latest release: \[v[0-9.]+\]'
# Inside the repository: the workspace replacements and every module's
# requirement on the others name the release too.
check go.work 'dedo[a-z/]* v[0-9.]+ =>'
for mod in lint/go.mod openapi/go.mod mcp/go.mod cmd/dedo/go.mod examples/todo/go.mod examples/bad-todo/go.mod; do
  check "$mod" 'github.com/raveesh-me/dedo[a-z/]* v[0-9.]+'
done
# Services replace the MCP Go SDK with dedo's build of it: the tag must be
# published, and the examples must build against the same one.
fork=$(sed -n 's/^[[:space:]]*sdkForkRelease = "\(.*\)"$/\1/p' cmd/dedo/sdkfork.go)
if [ -n "$fork" ]; then
  if ! git ls-remote --exit-code --tags https://github.com/raveesh-me/go-sdk "refs/tags/$fork" >/dev/null; then
    echo "raveesh-me/go-sdk has no tag $fork, which cmd/dedo/sdkfork.go replaces the MCP Go SDK with" >&2
    status=1
  fi
  for mod in examples/todo/go.mod examples/contacts/go.mod; do
    if ! grep -q "=> github.com/raveesh-me/go-sdk $fork\$" "$mod"; then
      echo "$mod does not replace the MCP Go SDK with github.com/raveesh-me/go-sdk $fork" >&2
      status=1
    fi
  done
fi
exit $status
