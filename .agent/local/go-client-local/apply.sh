#!/usr/bin/env bash
# Re-applies the local go_client layer on top of a freshly-synced go_client/.
#
# The layer carries the local deltas that must survive every upstream sync:
#   1. fingerprint switching (-fingerprint flag + profile/TLS selection), and
#   2. GOOS=windows build compatibility (SO_REUSEADDR listenUDP + TUN-fd
#      unavailable on Windows).
# Source of truth: this file + go-client-local.patch in this directory.
# Upstream sync (sync.yml) wipes go_client/ (rm -rf + cp -r), so this script is
# run right after the copy to bring the deltas back as a reviewable layer inside
# the same sync PR. `git apply` fails loudly if the upstream code drifted away
# from the patch anchors — never silently drop the layer.
#
# Exit codes: 0 = applied or already applied · 1 = would fail to apply · 2 = usage

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"
PATCH_DIR="$SCRIPT_DIR"
PATCH="$PATCH_DIR/go-client-local.patch"
GO_CLIENT="$ROOT/go_client"

if [ ! -f "$PATCH" ]; then
  echo "GO-CLIENT-LAYER: patch not found: $PATCH" >&2
  exit 2
fi
if [ ! -d "$GO_CLIENT" ]; then
  echo "GO-CLIENT-LAYER: go_client not found: $GO_CLIENT" >&2
  exit 2
fi

apply_from="$ROOT"
check_args=(--check)
if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  : # running inside the repo — apply paths match a/go_client/..., b/go_client/...
else
  echo "GO-CLIENT-LAYER: not a git work tree: $ROOT" >&2
  exit 2
fi

cd "$apply_from"

# Already applied? Then reverse-check must succeed and forward-check must fail.
if (git apply --check --reverse "$PATCH" >/dev/null 2>&1); then
  echo "GO-CLIENT-LAYER: already applied, skipping"
  exit 0
fi

if (git apply "${check_args[@]}" "$PATCH" >/dev/null 2>&1); then
  git apply "$PATCH"
  echo "GO-CLIENT-LAYER: applied (local go_client layer restored)"
else
  echo "GO-CLIENT-LAYER: FAILED to apply — upstream go_client drifted from the patch." >&2
  echo "  Fix/rebase $PATCH against the new upstream (see .agent/local/go-client-local/)." >&2
  echo "  Do NOT silence this: skipping it silently drops the local layer." >&2
  exit 1
fi