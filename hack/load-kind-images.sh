#!/usr/bin/env bash
# Load host-platform images without incomplete multi-platform indexes.
set -euo pipefail
cluster="${1:?Usage: load-kind-images.sh CLUSTER IMAGE [IMAGE...]}"
shift
if [[ $# -eq 0 ]]; then
  echo "At least one image is required." >&2
  exit 1
fi
kind_bin="${KIND:-kind}"
if docker image save --help | grep -q -- '--platform'; then
  archive_dir=$(mktemp -d)
  trap 'rm -rf "$archive_dir"' EXIT
  for image in "$@"; do
    platform=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")
    docker image save --platform "$platform" --output "$archive_dir/image.tar" "$image"
    "$kind_bin" load image-archive --name "$cluster" "$archive_dir/image.tar"
  done
else
  "$kind_bin" load docker-image --name "$cluster" "$@"
fi
