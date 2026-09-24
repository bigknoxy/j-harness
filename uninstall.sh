#!/usr/bin/env sh
# j-harness uninstaller.
#
#   curl -fsSL https://raw.githubusercontent.com/bigknoxy/j-harness/main/uninstall.sh | sh
#
# Removes the harness binary and, unless KEEP_REGISTRY=1, the starter registry.
# Overridable to match a custom install:
#   PREFIX=$HOME/.local  BINDIR=$HOME/bin  REGISTRY_DIR=...  DATA_DIR=...
#   KEEP_DATA=1  keep the SQLite data directory
#   KEEP_REGISTRY=1  keep the agent registry
set -eu

PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
REGISTRY_DIR="${REGISTRY_DIR:-$HOME/.config/j-harness/registry}"
DATA_DIR="${DATA_DIR:-$HOME/.local/share/j-harness}"
KEEP_DATA="${KEEP_DATA:-0}"
KEEP_REGISTRY="${KEEP_REGISTRY:-0}"

say() { printf '%s\n' "$*"; }

removed=0

if [ -f "$BINDIR/harness" ]; then
  rm -f "$BINDIR/harness"
  say "removed ${BINDIR}/harness"
  removed=1
else
  say "not found: ${BINDIR}/harness"
fi

if [ "$KEEP_REGISTRY" = "1" ]; then
  say "keeping registry ${REGISTRY_DIR} (KEEP_REGISTRY=1)"
elif [ -d "$REGISTRY_DIR" ]; then
  rm -rf "$REGISTRY_DIR"
  say "removed ${REGISTRY_DIR}"
  removed=1
fi

if [ "$KEEP_DATA" = "1" ]; then
  say "keeping data ${DATA_DIR} (KEEP_DATA=1)"
elif [ -d "$DATA_DIR" ]; then
  rm -rf "$DATA_DIR"
  say "removed ${DATA_DIR}"
  removed=1
fi

# j-harness also writes ./data/harness.db beside the working directory by default.
if [ -d "./data" ]; then
  say "note: ./data exists in the current directory (delete it manually if it belongs to j-harness)"
fi

if [ "$removed" = "0" ]; then
  say "nothing to remove"
else
  say "uninstalled. thanks for trying j-harness."
fi
