#!/bin/sh
set -eu

repo_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
bundle_dir=${FJORD_LANGUAGE_BUNDLE:-"$repo_dir/languages"}
mkdir -p "$bundle_dir"
if [ "$bundle_dir" != "$repo_dir/languages" ]; then
    cp "$repo_dir/languages/package.json" "$repo_dir/languages/package-lock.json" "$bundle_dir/"
fi
npm ci --prefix "$bundle_dir" --omit=dev --ignore-scripts --engine-strict --no-audit --no-fund
