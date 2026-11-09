#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
frontend="$root/frontend"
backend="$root/backend"
output="$root/build"
temporary=$(mktemp -d "$root/.build.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

if [ -n "${PNPM_STORE_DIR_OVERRIDE:-}" ]; then
  pnpm --store-dir "$PNPM_STORE_DIR_OVERRIDE" --dir "$frontend" install --frozen-lockfile
else
  pnpm --dir "$frontend" install --frozen-lockfile
fi
pnpm --dir "$frontend" build
mkdir -p "$temporary/frontend"
cp -R "$frontend/dist/." "$temporary/frontend/"
mkdir -p "$temporary/licenses"
cp "$backend/internal/game/dictionary/LICENSE-ESDB.txt" "$temporary/licenses/ESDB.txt"
cd "$backend"
go build -trimpath -ldflags="-s -w" -o "$temporary/letterpress" ./cmd/server
go build -trimpath -ldflags="-s -w" -o "$temporary/letterpress-bot" ./cmd/bot-runtime
rm -rf "$output"
mv "$temporary" "$output"
trap - EXIT HUP INT TERM
