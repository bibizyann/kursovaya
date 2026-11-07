# ADR 0007: Local Prometheus and Grafana stack

- Status: accepted
- Date: 2026-11-07
- Extends: [ADR 0006](0006-simulated-telemetry.md), implementation step 7.

## Decision

Install the pinned `kube-prometheus-stack` chart in the independent `factory-monitoring` namespace. Chart version 92.22.0 is recorded in `config/monitoring/versions.env`; its dependencies are packaged in the versioned chart archive. Keep the local profile focused on equipment and operator metrics: default cluster dashboards, node exporter, kube-state-metrics, and Alertmanager are disabled.

Prometheus Operator discovers two labelled ServiceMonitors and one PrometheusRule from this namespace. The monitors select Services in the platform and telemetry namespaces. Preserve domain `namespace` labels with `honorLabels: true`; otherwise Prometheus target metadata would rename the equipment namespace to `exported_namespace` and break filters and joins. Operator metrics retain service-account authentication and metrics-reader RBAC. The local controller's self-signed certificate uses `insecureSkipVerify`; a real deployment must provide trusted TLS.

Scrape and evaluate every 5 seconds. Recording rules distinguish unavailable receiver, missing/stale equipment observations, and fresh equipment faults. Gate readings, states, and derived operation rates on source availability and freshness. Do not connect graph gaps or synthesize healthy zero values. Use `rate` for cumulative counters and a gauge for persisted rollout duration. Rates across telemetry gaps cannot reconstruct exact operation timing.

Provision three Grafana dashboards and the Prometheus datasource from Git. Use stable dashboard UIDs, namespace/process/workstation/equipment filters, and read-only provisioned dashboards. Generate the admin password on installation and retain it in a Kubernetes Secret. Services remain ClusterIP; users access the interfaces through local port-forward.

Prometheus uses a 2 GiB local PVC, seven-day retention, and a 1 GB historical block limit. This retains history across pod replacement, not across deletion of the Kind cluster. Grafana's local database is ephemeral; desired dashboards and datasource are reprovisioned. No production database, MQTT replay, or external cloud service is added.

Alert rules cover source unavailability, stale observations, equipment faults/calibration, and degraded/stale process status. Alerts are visible in Prometheus and Grafana; external delivery is not configured. They never mutate TechProcess or gate workload readiness.

## Validation

`make monitoring-render` renders the pinned chart and project resources. `make test-monitoring` checks rules with promtool, tests unknown/fault/reset semantics, and executes every dashboard PromQL query against the pinned Prometheus version. The optional `FACTORY_MONITORING_E2E=true` Kind scenario verifies Grafana health and collected A/B metrics for eight devices. Installation and operation are described in the [runbook](../runbooks/monitoring.md).

## References

- [kube-prometheus-stack chart](https://github.com/prometheus-community/helm-charts/tree/kube-prometheus-stack-92.22.0/charts/kube-prometheus-stack).
- [Prometheus Operator API](https://prometheus-operator.dev/docs/api-reference/api/).
- [Prometheus rule tests](https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules/).
- [Grafana provisioning](https://grafana.com/docs/grafana/latest/administration/provisioning/).
