#!/bin/sh
# Check prerequisites and download Go modules; never touch an existing database.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
for tool in go node cc; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'Missing prerequisite: %s. See docs/CODEX.md.\n' "$tool" >&2
    exit 1
  fi
done
node -e 'if (Number(process.versions.node.split(".")[0]) < 22) { console.error("Node 22+ is recommended and required by this setup helper"); process.exit(1); }'
printf '#include <sqlite3.h>\nint main(void) { return sqlite3_libversion() == 0; }\n' | cc -x c - -lsqlite3 -fsyntax-only
export CGO_ENABLED=1
go mod download
printf '\nDevelopment prerequisites checked. Run: sh scripts/check.sh\n'
printf 'No production environment variables, database or AI credentials are needed.\n'
