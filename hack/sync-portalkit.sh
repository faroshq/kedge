#!/usr/bin/env bash
# Vendors the shared portal UI kits into each provider portal's src/portalkit/.
# The portals build self-contained (no npm workspace / symlink), so shared UI
# primitives are copied per portal rather than imported across package
# boundaries.
#
#   provider-sdk/portalkit      → vanilla-TS portals  (icons.ts, modal.ts)
#   provider-sdk/portalkit-vue  → Vue SFC portals     (confirm.ts, ConfirmDialog.vue, …)
#
# Each portal vendors only the files it imports (ConditionsPanel.vue pulls in
# ResourceTable.vue and StatusBadge.vue). The src/portalkit/ directory is owned
# by this script: it is recreated on every sync, so removing a file from a
# portal's list below removes it from the portal.
#
# Edit the canonical files under provider-sdk/ and run `make sync-portalkit`.
# CI runs `make verify-portalkit` to fail on drift.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TS_SRC="$ROOT/provider-sdk/portalkit"
VUE_SRC="$ROOT/provider-sdk/portalkit-vue"

# sync <src-dir> <portal> <file...> — replaces the portal's src/portalkit with
# exactly the listed files.
sync() {
  local src="$1" portal="$2"; shift 2
  local dst="$ROOT/$portal/src/portalkit"
  rm -rf "$dst"
  mkdir -p "$dst"
  for f in "$@"; do
    cp "$src/$f" "$dst/$f"
  done
  echo "synced $(basename "$src") -> $portal/src/portalkit ($*)"
}

# Vanilla-TS (string-building) portals.
sync "$TS_SRC" "providers/agents/portal"     icons.ts modal.ts tenant.ts
sync "$TS_SRC" "providers/kuery/portal"      icons.ts modal.ts tenant.ts
sync "$TS_SRC" "providers/quickstart/portal" icons.ts modal.ts tenant.ts

# Vue SFC portals.
sync "$VUE_SRC" "portal"                      confirm.ts ConfirmDialog.vue
sync "$VUE_SRC" "providers/app-studio/portal" confirm.ts ConfirmDialog.vue
sync "$VUE_SRC" "providers/code/portal"       confirm.ts ConfirmDialog.vue ResourceTable.vue ConditionsPanel.vue StatusBadge.vue
sync "$VUE_SRC" "providers/databricks/portal" confirm.ts ConfirmDialog.vue ResourceTable.vue ConditionsPanel.vue StatusBadge.vue
sync "$VUE_SRC" "providers/edges/portal"      confirm.ts ConfirmDialog.vue

# tenant.ts is plain TS (no framework); Vue portals whose api client reads the
# tenant context vendor it from the vanilla canonical.
cp "$TS_SRC/tenant.ts" "$ROOT/providers/app-studio/portal/src/portalkit/tenant.ts"
echo "synced tenant.ts -> providers/app-studio/portal/src/portalkit"
