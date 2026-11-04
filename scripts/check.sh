#!/bin/sh
# Run from any directory; build only into a new temporary directory.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
export CGO_ENABLED=1
go test ./...
go test -race ./...
node --test tests/*.test.js
for file in web/*.js; do
  node --check "$file"
done
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/familiehub-check.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
go build -o "$build_dir/familiehub" .
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git diff --check
fi
printf '\nFamilieHub verification passed. No live server or family data was changed.\n'
