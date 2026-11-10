#!/usr/bin/env bash
# Deploy only into the named Kind cluster, with an isolated kubeconfig.
set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
cluster="${DEV_CLUSTER:-factory-platform-dev}"
kind_bin="${KIND:-kind}"
operator_image="${DEV_OPERATOR_IMG:-factory-platform:dev}"
simulator_image="${SIMULATOR_IMG:-equipment-simulator:dev}"
receiver_image="${RECEIVER_IMG:-telemetry-receiver:dev}"
broker_image="eclipse-mosquitto:2.0.22"
mkdir -p .cache
export KUBECONFIG="$root/.cache/telemetry-${cluster}.kubeconfig"
export KUBECTL_KUBERC=false
if ! "$kind_bin" get clusters | grep -xF -- "$cluster" > /dev/null; then
  "$kind_bin" create cluster --name "$cluster" --image "${KIND_NODE_IMAGE:-kindest/node:v1.35.0}" --config infra/kind/cluster-single.yaml
fi
"$kind_bin" export kubeconfig --name "$cluster" --kubeconfig "$KUBECONFIG"
make docker-build docker-build-simulator docker-build-telemetry-receiver \
  IMG="$operator_image" SIMULATOR_IMG="$simulator_image" RECEIVER_IMG="$receiver_image"
docker pull "$broker_image"
KIND="$kind_bin" bash hack/load-kind-images.sh "$cluster" "$operator_image" "$simulator_image" "$receiver_image" "$broker_image"

# Temporary overlays keep image overrides out of tracked manifests.
overlay=$(mktemp -d "$root/.cache/telemetry-overlay-XXXXXX")
trap 'rm -rf "$overlay"' EXIT
mkdir -p "$overlay/operator" "$overlay/platform" "$overlay/samples"
cat > "$overlay/operator/kustomization.yaml" <<EOF
resources:
  - ../../../config/default
EOF
cat > "$overlay/platform/kustomization.yaml" <<EOF
resources:
  - ../../../config/telemetry
EOF
cat > "$overlay/samples/kustomization.yaml" <<EOF
resources:
  - ../../../config/overlays/telemetry
replacements:
  - sourceValue: $simulator_image
    targets:
      - select:
          kind: TechProcessTemplate
        fieldPaths:
          - spec.components.*.image
EOF
(cd "$overlay/operator" && "$root/bin/kustomize" edit set image "factory-platform=$operator_image" "controller=$operator_image")
(cd "$overlay/platform" && "$root/bin/kustomize" edit set image "telemetry-receiver=$receiver_image")
bin/kustomize build "$overlay/operator" | bin/kubectl apply -f -
bin/kubectl wait --for=condition=Established --timeout=60s crd/techprocesses.production.factory.local crd/techprocesstemplates.production.factory.local
bin/kustomize build "$overlay/platform" | bin/kubectl apply -f -
bin/kubectl rollout status deployment/mosquitto -n factory-telemetry --timeout=120s
bin/kubectl rollout status deployment/telemetry-receiver -n factory-telemetry --timeout=120s
bin/kustomize build "$overlay/samples" | bin/kubectl apply -f -
echo "Stand applied. For inspection: export KUBECONFIG=$KUBECONFIG"
