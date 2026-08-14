#!/bin/bash
# fetch-ui.sh — Download metacubexd dashboard assets for embedding.
#
# Reads METACUBEXD_VERSION from versions.env, fetches the release tarball,
# and extracts it into internal/server/static/web/ so go:embed bundles it.
#
# Usage: bash scripts/fetch-ui.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# shellcheck source=../versions.env
source "$ROOT_DIR/versions.env"

WEB_DIR="$ROOT_DIR/internal/server/static/web"
rm -rf "$WEB_DIR"
mkdir -p "$WEB_DIR"

echo "[fetch-ui] downloading metacubexd ${METACUBEXD_VERSION}..."
curl -fsSL "https://github.com/MetaCubeX/metacubexd/releases/download/${METACUBEXD_VERSION}/compressed-dist.tgz" \
  | tar xz -C "$WEB_DIR"

echo "[fetch-ui] done ($(find "$WEB_DIR" -type f | wc -l) files)"
