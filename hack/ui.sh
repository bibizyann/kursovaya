#!/usr/bin/env bash
# Build and install the read-only overview into an explicitly named Kind stand.
set -euo pipefail
cd "$(dirname "$0")/.."
cluster="${DEV_CLUSTER:-factory-platform-dev}"
kind_bin="${KIND:-kind}"
container_tool="${CONTAINER_TOOL:-docker}"
mkdir -p .cache
export KUBECONFIG="$PWD/.cache/telemetry-$cluster.kubeconfig"
export KUBECTL_KUBERC=false
"$kind_bin" export kubeconfig --name "$cluster" --kubeconfig "$KUBECONFIG"
bin/kubectl get namespace factory-demo >/dev/null
# Content-address the binary, including embedded assets, to avoid reusing a stale :dev image.
image="${UI_IMG:-factory-api:$(sha256sum bin/factory-api | cut -c1-16)}"
"$container_tool" build -f Dockerfile.factory-api -t "$image" .
"$kind_bin" load docker-image --name "$cluster" "$image"
overlay=$(mktemp -d "$PWD/.cache/ui-overlay.XXXXXX")
trap 'rm -rf "$overlay"' EXIT
cat > "$overlay/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../config/ui
images:
  - name: factory-api
    newName: ${image%:*}
    newTag: ${image##*:}
EOF
bin/kustomize build "$overlay" | bin/kubectl apply -f -
bin/kubectl rollout status deployment/factory-api -n factory-ui --timeout=120s
echo "Factory overview installed. Run: make ui-open DEV_CLUSTER=$cluster"
