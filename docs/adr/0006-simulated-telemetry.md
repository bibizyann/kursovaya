# ADR 0006: Simulated telemetry boundary

- Status: accepted
- Date: 2026-10-01
- Partially supersedes: [ADR 0003](0003-communication-channels.md), specifically the required hardware edge agent, local gRPC implementation, and persistent telemetry queue/replay.
- Refines: [ADR 0005](0005-equipment-simulation.md) with an optional periodic publisher. Existing HTTP operations remain supported.

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

## Simulator generation

One locked simulator state backs HTTP operations, readiness, cumulative counters, and automatic cycles. Require an explicit seed and record it in each snapshot. Reproducibility assumes the same clock steps and control/operation order; source lifetime IDs remain unique between runs. Snapshot sequence is independent of the completed-operation sequence.

A ready cycle completes one synthetic operation. An unready cycle emits only a heartbeat. Slow automatic operations remain pending across ticks; a readiness/control epoch change aborts them and increments the error counter. Explicit failed HTTP attempts also increment errors. Rejected products count as completed operations, not equipment faults.

Call the snapshot sink outside the state lock. Sink errors do not affect readiness or trigger historical retries. Step 3 introduced a JSON log sink; step 4 also offers snapshots to a bounded MQTT worker, so network waits cannot pause automatic cycles. Synthetic timing and fluctuations are educational fixtures, not a model of physical throughput or thermal dynamics.

## MQTT sessions and bounded delivery

Use [Eclipse Paho v1.5.1](https://github.com/eclipse-paho/paho.mqtt.golang/tree/v1.5.1) with MQTT 3.1.1, QoS 1, and retained full snapshots. The subscriber derives exact topics from its expected inventory and validates topic/payload identity before calling the domain receiver.

The publisher retains at most one pending snapshot and one in flight. New pending data replaces old pending data; disconnected offers are dropped. Every connection uses a new client and memory store with a clean session. Disable library-managed reconnect and reconnect explicitly, discarding pending publications, so reconnect does not replay old client history. A packet sent before disconnection may already have reached the broker; receiver deduplication remains necessary.

Keep generator, network worker, and HTTP lifecycle separate. Bound connection, acknowledgment, and shutdown waits. Broker outage does not block simulator operation generation or change readiness. Retained data is subject to the same source-time checks as live delivery. The local [Mosquitto configuration](../../infra/mqtt/mosquitto.conf) disables disk persistence and bounds packet size; deployment-wide installation remains step 6.

## Operator status and metrics

Export process Conditions, confirmed desired-revision workload counts, revision info, and rollout timestamps/duration through the existing manager registry. Use a custom collector over cached CRs and persisted status, not increments on Reconcile. Completed duration is a gauge derived from StartedAt/CompletedAt, so repeated reconciliation and operator restart cannot count one rollout multiple times. Historical completeness still depends on Prometheus sampling.

Persist rollout start before child updates and first completion after readiness of the target revision. Preserve completion through later health changes; only a new desired revision starts a new rollout. Deployment/DaemonSet aggregate ready counts alone cannot prove target-revision readiness: verify pod-template revision, observed generation, updated counts, and availability. Mixed revisions expose zero confirmed ready instances until old instances are gone.

Record observed template UID/generation alongside process observedGeneration. Check reconcile inputs again before status writes and use optimistic locking. Stale metric Conditions become unknown; revision-dependent metrics are omitted until current inputs match. MQTT and the telemetry receiver remain independent of these readiness checks.

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

Steps 1–5 are implemented: contract, receiver, generator, MQTT transport, operator metrics, and target-revision readiness checks. The single-device Mosquitto pipeline and reconnection are verified locally. Subsequent steps require the user's explicit approval after each completed step. The current implementation status is tracked in the [project plan](../../План.md).

## Consequences

- No new production CRD is needed for monitoring; component arguments and environment variables carry publisher configuration.
- The receiver is separate from Reconcile and does not own TechProcess resources or child workloads.
- Source timestamps and instance ordering assume synchronized clocks on the stand; future integrations must satisfy or revise that assumption.
- Prometheus is the only required time-series store. Raw experiment summaries can be exported to CSV/JSON without adding a database.
- No-loss queue tests from the earlier roadmap are replaced by freshness and reconnection tests.
