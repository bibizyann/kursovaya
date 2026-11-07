#!/usr/bin/env bash
# Install or render the pinned local monitoring stack.
set -euo pipefail
cd "$(dirname "$0")/.."
source config/monitoring/versions.env
action="${1:-render}"
helm_bin="${HELM:-helm}"
chart_dir="$PWD/.cache/monitoring"
chart="$chart_dir/kube-prometheus-stack-$MONITORING_CHART_VERSION.tgz"
mkdir -p "$chart_dir"
if [[ ! -f "$chart" ]]; then
  "$helm_bin" pull kube-prometheus-stack --repo https://prometheus-community.github.io/helm-charts \
    --version "$MONITORING_CHART_VERSION" --destination "$chart_dir"
fi
if [[ "$action" == render ]]; then
  "$helm_bin" template factory-monitoring "$chart" --namespace factory-monitoring \
    --kube-version 1.35.0 -f config/monitoring/values.yaml > "$chart_dir/stack.yaml"
  bin/kustomize build config/monitoring > "$chart_dir/factory.yaml"
  echo "Rendered monitoring manifests in $chart_dir"
  exit 0
fi
if [[ "$action" != install ]]; then
  echo "Usage: $0 [render|install]" >&2
  exit 1
fi
cluster="${DEV_CLUSTER:-factory-platform-dev}"
export KUBECONFIG="$PWD/.cache/telemetry-$cluster.kubeconfig"
export KUBECTL_KUBERC=false
"${KIND:-kind}" export kubeconfig --name "$cluster" --kubeconfig "$KUBECONFIG"
bin/kubectl create namespace factory-monitoring --dry-run=client -o yaml | bin/kubectl apply -f -
# Keep the password stable across installs; do not print it in installation logs.
if [[ -z "$(bin/kubectl get secret factory-grafana-admin -n factory-monitoring --ignore-not-found -o name)" ]]; then
  secret_dir=$(mktemp -d)
  trap 'rm -rf "$secret_dir"' EXIT
  openssl rand -base64 24 | tr -d '\n' > "$secret_dir/admin-password"
  printf admin > "$secret_dir/admin-user"
  bin/kubectl create secret generic factory-grafana-admin -n factory-monitoring \
    --from-file="$secret_dir/admin-user" --from-file="$secret_dir/admin-password"
fi
"$helm_bin" upgrade --install factory-monitoring "$chart" --namespace factory-monitoring \
  -f config/monitoring/values.yaml --wait --timeout=10m
bin/kubectl wait --for=condition=Established --timeout=60s \
  crd/servicemonitors.monitoring.coreos.com crd/prometheusrules.monitoring.coreos.com
bin/kustomize build config/monitoring | bin/kubectl apply -f -
bin/kubectl rollout status statefulset/prometheus-factory-monitoring-prometheus -n factory-monitoring --timeout=180s
echo "Monitoring installed. Export KUBECONFIG=$KUBECONFIG before port-forwarding."
