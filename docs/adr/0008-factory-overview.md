# ADR 0008: Read-only factory overview

- Status: accepted for UI stage U1
- Date: 2026-11-08
- Implements: [Factory UI plan, U1](../plans/factory-ui.md)

## Decision

Add `cmd/factory-api` to the existing Go module. Serve embedded HTML/CSS/JavaScript and `GET /api/v1/overview` from one binary using standard-library HTTP, templates and JSON. Runtime assets do not depend on CDNs or a frontend build service.

Scope each instance to one configured namespace and TechProcess. Read Kubernetes with namespace-scoped RBAC; read Prometheus with fixed instant queries. Do not add write routes, Kubernetes mutation permissions, simulator controls or Git integration in U1. Future desired-state changes retain ADR 0002 ownership.

Separate pure view models (`internal/factory`), infrastructure readers (`internal/factorysource`) and HTTP/presentation (`internal/factoryapi`). Read sources concurrently with a shared deadline, returning explicit partial availability. No successful snapshot is cached across a source error as current state.

Always display the eight catalog devices grouped into six logical workstations. Nodes remain separate placement attributes. Check process observedGeneration, template UID/generation, desired revision and live child readiness. Only owned workloads count; pods must have a verified Deployment/ReplicaSet or DaemonSet owner chain. Re-read input generations after child reads to reject changes during aggregation. Reads across multiple Kubernetes objects are not atomic.

Telemetry never gates workload readiness. Query raw metrics to avoid an extra recording-rule interval. Require a current receiver `up`, a current scrape, a source timestamp within the freshness interval, and complete state/identity from the same scrape. Reject duplicate series and non-finite values. Missing/stale/unknown observations expose no current physical readings; valid measured zeroes remain zeroes. Compare observed and desired revision separately from process Ready.

The local profile assumes one receiver scrape target. Multiple targets are ambiguous rather than arbitrarily merged. Adding receiver replicas or multiple independently scraped factories requires an explicit target-selection design.

Poll three seconds after the previous browser request completes, with manual refresh and a browser timeout. Preserve inventory but invalidate current statuses after an API failure. Link existing Grafana dashboards using their `namespace`, `process`, `workstation`, and `equipment` variables. Configure the browser-visible Grafana URL separately from the in-cluster Prometheus URL.

Deploy in `factory-ui` with a ClusterIP Service and loopback port-forward. U1 has no account management and is intended for the local read-only stand. Process probes do not depend on Kubernetes/Prometheus availability. Shared/network-facing use requires an authenticated TLS access profile before exposure.

## Consequences

- The operator, simulators and receiver keep their existing APIs and ownership.
- A UI/API outage cannot stop the process.
- There are no new CRDs, production databases, command queues or replay promises.
- Desired recipes shown in U1 are built-in profiles selected by component startup arguments. Editing recipes and selecting products are later stages.
- Browser cadence is source-read duration plus three seconds; meeting the 15-second end-to-end target requires measurement.
- RBAC, container assets, Make targets, source tests and optional Kind/browser tests accompany the interface.
