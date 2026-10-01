# ADR 0006: Simulated telemetry boundary

- Status: accepted
- Date: 2026-10-01
- Partially supersedes: [ADR 0003](0003-communication-channels.md), specifically the required hardware edge agent, local gRPC implementation, and persistent telemetry queue/replay.
- Refines: [ADR 0005](0005-equipment-simulation.md) with a planned optional periodic publisher. Existing HTTP operations remain supported.

## Context

The graduation project's implementation focuses on the Kubernetes operator. Equipment applications and physical data acquisition belong to the theoretical integration description. The monitoring implementation starts when an external application has produced a telemetry message; software simulators provide those messages on the development stand.

## Decision

Use the following local pipeline:

```text
equipment simulator -> MQTT / Mosquitto -> Go telemetry receiver /metrics
                                                     |
operator /metrics -----------------------------------+-> Prometheus -> Grafana
```

The receiver, broker, and monitoring stack are platform components, independent of individual TechProcess lifetimes. The operator continues to manage simulator workloads through Kubernetes desired state. MQTT never changes the desired product, recipe, or revision.

Keep one Go module. `internal/telemetry` defines schema v1 and stateless validation without Kubernetes, MQTT, or Prometheus dependencies. The [telemetry contract](../architecture/telemetry.md) specifies identity, readings, counters, time, and receiver ordering rules. Eight JSON fixtures demonstrate the complete reference inventory.

Publish complete snapshots with cumulative counters and a source instance identifier. Use QoS 1 with duplicate handling in the receiver. Each publisher restart starts a new counter lifetime. Periodic generation is opt-in, uses a recorded seed, and shares equipment readiness state with existing simulator APIs. Synthetic readings demonstrate the pipeline; they do not establish physical production performance.

The receiver keeps only current state in memory and exposes metrics. Prometheus stores sampled time series; Grafana displays and aggregates them through PromQL. Configure expected devices so a device that never published can still be reported as missing. Apply freshness checks to retained messages as well as live delivery. Stale readings must not be presented as current measurements.

Do not implement real hardware drivers, an equipment edge agent, local gRPC, physical calibration, a last-known-good hardware cache, a persistent telemetry queue, or a production event database. Describe possible equipment integrations theoretically. A future deployment may replace a simulator with external equipment software that follows the same contract.

## Readiness and outages

Equipment health, setup, and calibration remain simulated checks exposed through workload readiness. A Ready process must refer to the same target revision. MQTT connectivity and monitoring availability are not workload readiness conditions.

An outage produces an unknown monitoring state and missing measurements. After reconnection or receiver restart, new snapshots restore current state. There is no guarantee of complete operation history or historical replay. Counter differences across a gap must not be interpreted as exact production timing.

Sequential workstation rollout and automatic rollback remain separate operator tasks from ADR 0004. This monitoring decision neither implements them nor changes Git ownership from ADR 0002. K3s/KubeEdge and cloud deployment are optional extensions beyond the local stand.

## Implementation sequence

1. Contract, validation, fixtures, and scope documentation.
2. In-memory receiver and Prometheus collector, tested without MQTT.
3. Optional periodic simulator generation.
4. MQTT transport and receiver binary; first complete single-device pipeline.
5. Operator metrics and revision-readiness checks.
6. Images, manifests, samples, Make targets, and CI for eight devices.
7. Prometheus installation and provisioned Grafana dashboards.
8. End-to-end fault scenarios and a reproducible demonstration runbook.

Steps 1–2 are implemented: the contract, fixtures, in-memory receiver, and Prometheus collector. Subsequent steps require the user's explicit approval after each completed step. The current implementation status is tracked in the [project plan](../../План.md).

## Consequences

- No new production CRD is needed for monitoring; component arguments and environment variables carry publisher configuration.
- The receiver is separate from Reconcile and does not own TechProcess resources or child workloads.
- Source timestamps and instance ordering assume synchronized clocks on the stand; future integrations must satisfy or revise that assumption.
- Prometheus is the only required time-series store. Raw experiment summaries can be exported to CSV/JSON without adding a database.
- No-loss queue tests from the earlier roadmap are replaced by freshness and reconnection tests.
