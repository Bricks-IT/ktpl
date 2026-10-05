#!/usr/bin/env bash
# Preflight checks for ktpl. Must pass before any task is declared done.
# Thin wrapper so agents have a stable entry point; the source of truth is `make preflight`.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
exec make preflight
