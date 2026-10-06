#!/usr/bin/env bash
# Scaffold a new golden example: examples/<NN>-<name>/{templates,rendered}
set -euo pipefail

name="${1:?usage: new-example.sh <kebab-name>}"
if [[ ! "$name" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]]; then
  echo "error: name must be kebab-case" >&2
  exit 2
fi

root="$(git rev-parse --show-toplevel)"
cd "$root/examples"

last="$(find . -maxdepth 1 -mindepth 1 -type d -name '[0-9][0-9]-*' | sed 's|./||' | sort | tail -n1 | cut -c1-2)"
next="$(printf '%02d' $((10#${last:-0} + 1)))"
dir="${next}-${name}"

mkdir -p "$dir/templates" "$dir/rendered"
cat > "$dir/templates/objects.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: demo
data:
  key: value
EOF

echo "created examples/$dir"
echo "next: edit templates/, then run: go test ./internal/cli -update -ginkgo.label-filter=golden -ginkgo.focus='$dir'"
