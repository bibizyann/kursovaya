# Monitoring guidance

- Install the pinned kube-prometheus-stack chart; keep release, namespace, service names, datasource UID, and dashboard links consistent with the runbook.
- Monitoring is independent of TechProcess ownership and readiness. Never write CRs from alerts or dashboards.
- Preserve exported domain labels with honorLabels on both ServiceMonitors; Kubernetes service namespaces differ from equipment namespaces.
- Gate equipment queries on receiver availability and freshness. Missing data is unknown, never a healthy zero. Keep time-series gaps visible.
- Use rate/increase for counters and seconds for rollout duration. Synthetic operation rates are not board throughput or OEE.
- Keep exactly three provisioned dashboards in Git. Do not rely on manual Grafana imports or UI edits.
- Test PromQL rules with promtool, including missing targets, stale telemetry, faults, and counter resets. Validate dashboard queries against Prometheus.
- The local profile uses ClusterIP and port-forward. Operator metrics retain bearer-token authentication; self-signed TLS verification is skipped only for the local stand.
- Prometheus uses a local PVC. State clearly that deleting the Kind cluster deletes its history; Grafana definitions are restored from Git.
