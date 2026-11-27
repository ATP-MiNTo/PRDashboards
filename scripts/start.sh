#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

fail() {
  echo "error: $*" >&2
  exit 1
}

# VERSION goes into the linker flags, so it must be plain MAJOR.MINOR.PATCH before it is used.
version=$(tr -d '\r' <VERSION)
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "VERSION must look like 1.2.3, found '$version'"

version_ge() {
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

command -v go >/dev/null 2>&1 || fail "go is not installed. Install it from https://go.dev/dl/"
command -v node >/dev/null 2>&1 || fail "node is not installed. Install it from https://nodejs.org/"
command -v pnpm >/dev/null 2>&1 || fail "pnpm is not installed. Run 'corepack enable' or see https://pnpm.io/installation"

go_want=$(awk '$1 == "go" { print $2; exit }' server/go.mod)
go_have=$(go env GOVERSION | awk '{ sub(/^go/, "", $1); print $1 }')
version_ge "$go_have" "$go_want" || fail "Go $go_want or newer is required, found $go_have"

node_want=$(tr -d 'v \r\n' <.nvmrc | cut -d. -f1)
node_have=$(node -p 'process.versions.node.split(".")[0]')
[ "$node_have" -ge "$node_want" ] || fail "Node $node_want or newer is required, found $node_have"

exe=""
case "$(uname -s)" in
  MINGW* | MSYS* | CYGWIN*) exe=".exe" ;;
esac
bin="bin/prdash$exe"

if command -v sha256sum >/dev/null 2>&1; then
  sha=(sha256sum)
else
  sha=(shasum -a 256)
fi

source_hash() {
  {
    find web server api -type f \
      -not -path '*/node_modules/*' -not -path '*/.next/*' \
      -not -path 'web/out/*' -not -path 'server/web/*' \
      -not -path 'web/public/icons/*' -not -name manifest.webmanifest \
      -not -name next-env.d.ts -not -name '*.tsbuildinfo' -print0
    printf '%s\0' VERSION package.json pnpm-workspace.yaml pnpm-lock.yaml go.work .nvmrc
  } | LC_ALL=C sort -z | xargs -0 "${sha[@]}" | "${sha[@]}" | cut -d' ' -f1
}

hash=$(source_hash)
if [ ! -f .build-hash ] || [ "$(cat .build-hash)" != "$hash" ] || [ ! -x "$bin" ]; then
  echo "==> pnpm install"
  pnpm install --frozen-lockfile
  echo "==> web build"
  pnpm -C web build
  echo "==> embed web build"
  find server/web -mindepth 1 -maxdepth 1 ! -name .gitkeep -exec rm -rf {} +
  cp -R web/out/. server/web/
  echo "==> go build"
  go build -trimpath -ldflags "-X main.version=$version" -o "$bin" ./server/cmd/prdash
  printf '%s\n' "$hash" >.build-hash
else
  echo "==> sources unchanged, skipping build"
fi

exec "$bin" "$@"
