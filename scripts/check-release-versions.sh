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
check templates/Makefile.api 'DEDO_VERSION \?= v[0-9.]+'
check guidance/install.md 'raveesh-me/dedo[a-z/-]*@v[0-9.]+'
check guidance/guideline.md 'raveesh-me/dedo[a-z/-]*@v[0-9.]+'
check readme.md 'Latest release: \[v[0-9.]+\]'
# Inside the repository: the workspace replacements and every module's
# requirement on the others name the release too.
check go.work 'dedo[a-z/]* v[0-9.]+ =>'
for mod in lint/go.mod openapi/go.mod mcp/go.mod examples/todo/go.mod examples/bad-todo/go.mod; do
  check "$mod" 'github.com/raveesh-me/dedo[a-z/]* v[0-9.]+'
done
exit $status
