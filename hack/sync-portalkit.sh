#!/usr/bin/env bash
# Copies the canonical portalkit (provider-sdk/portalkit) into each vanilla-TS
# provider portal's src/portalkit/. The portals build self-contained (no npm
# workspace / symlink), so shared UI primitives are vendored per portal rather
# than imported across package boundaries.
#
# Edit the canonical files under provider-sdk/portalkit and run `make
# sync-portalkit`. CI runs `make verify-portalkit` to fail on drift.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/provider-sdk/portalkit"

# Vanilla-TS (string-building) portals that consume the kit. The Vue portals use
# lucide-vue-next + ConfirmDialog.vue instead and are intentionally excluded.
PORTALS=(
  "providers/agents/portal"
  "providers/kuery/portal"
  "providers/quickstart/portal"
)

FILES=(icons.ts modal.ts)

for p in "${PORTALS[@]}"; do
  dst="$ROOT/$p/src/portalkit"
  mkdir -p "$dst"
  for f in "${FILES[@]}"; do
    cp "$SRC/$f" "$dst/$f"
  done
  echo "synced portalkit -> $p/src/portalkit"
done
