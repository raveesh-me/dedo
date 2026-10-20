#!/usr/bin/env bash
# Compares protoc-gen-dedo-mcp and mcpserver with the latest MCP specification.
#
# For the newest dated spec version and for the draft, it extracts the Tool
# definition (scripts/mcp-tool-schema.py), diffs it against the vendored one
# in mcp/mcpserver/testdata, and runs mcpserver's TestToolsMatchSpec against
# it. It writes a Markdown report to $1 and exits 1 when anything needs
# attention: a newer spec version, a changed Tool definition, or tools that
# no longer match.
#
# Needs gh (authenticated), jq, python3 and go.
set -euo pipefail

report=${1:?usage: check-mcp-spec.sh <report.md>}
root=$(cd "$(dirname "$0")/.." && pwd)
snapshot=$root/mcp/mcpserver/testdata/mcp-tool-schema.json
repo=modelcontextprotocol/modelcontextprotocol
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

pinned=$(jq -r '."$comment"' "$snapshot" | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}')
latest=$(gh api "repos/$repo/contents/schema" -q '.[].name' | grep -E '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' | sort | tail -1)
attention=0

{
	echo "Vendored Tool definition: MCP specification \`$pinned\`. Latest dated version: \`$latest\`."
	echo
	if [[ $latest > $pinned ]]; then
		attention=1
		echo "**A newer specification is out.** Review its changelog, update the annotations in \`proto/dedo/v1/options.proto\` and \`DEDO_TOOL_ANNOTATIONS\` if needed, then refresh the snapshot:"
		echo
		echo '```sh'
		echo "gh api -H 'Accept: application/vnd.github.raw' repos/$repo/contents/schema/$latest/schema.json > schema.json"
		echo "python3 scripts/mcp-tool-schema.py schema.json $latest > mcp/mcpserver/testdata/mcp-tool-schema.json"
		echo '```'
		echo
	fi
} > "$report"

for version in $(printf '%s\n' "$latest" draft | sort -u); do
	gh api -H 'Accept: application/vnd.github.raw' "repos/$repo/contents/schema/$version/schema.json" > "$work/$version.json"
	python3 -I "$root/scripts/mcp-tool-schema.py" "$work/$version.json" "$version" > "$work/$version-tool.json"
	# The $comment names the version, so leave it out of the comparison.
	diff_out=$(diff -u <(jq 'del(."$comment")' "$snapshot") <(jq 'del(."$comment")' "$work/$version-tool.json")) || true
	test_out=$(cd "$root/mcp" && MCP_TOOL_SCHEMA="$work/$version-tool.json" go test -count=1 -run TestToolsMatchSpec ./mcpserver/ 2>&1) && passed=1 || passed=0
	{
		echo "## \`$version\`"
		echo
		if [[ -z $diff_out ]]; then
			echo "The Tool definition is unchanged."
		else
			attention=1
			echo "The Tool definition changed:"
			echo
			echo '```diff'
			echo "$diff_out"
			echo '```'
		fi
		echo
		if ((passed)); then
			echo "The served tools match it."
		else
			attention=1
			echo "**The served tools do not match it:**"
			echo
			echo '```'
			echo "$test_out"
			echo '```'
		fi
		echo
	} >> "$report"
done

cat "$report"
exit $((attention))
