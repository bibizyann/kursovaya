# Controller guidance

- Reconcile is level-based and idempotent. Never rely on receiving every event.
- Fetch the latest parent object before mutating status and tolerate conflicts.
- Resolve and validate the complete template before changing children.
- Child resource names, selectors, labels, and revision hashes must be deterministic.
- Use controller references for garbage collection and watch owned resources.
- Argo CD owns the parent CR; this controller owns derived ConfigMaps and workloads.
- Missing templates, invalid templates, scheduling failures, and rollout timeouts are reported through Conditions rather than panics.
- Hardware-bound workloads default to safe replacement. Do not introduce surge for exclusive devices.
- Keep object construction in `internal/workload` so it can be tested without an API server.

- Persist StartedAt before applying a new revision; record CompletedAt only on first readiness. Later health loss must not reset completion or become a timeout of an already completed rollout.
- Readiness requires the target pod-template revision, observed workload generation, updated replicas, and availability. Never infer new-revision readiness from aggregate ReadyReplicas alone.
- Status records both process generation and template UID/generation. Reject stale reconcile inputs and use optimistic locking for status patches.
- Process metrics are a custom collector over persisted status and current cached CRs. Do not increment rollout histograms/counters on Reconcile or maintain an unbounded per-process metric cache.
- Export stale Conditions as unknown and omit revision-dependent measurements until status matches current inputs. Use bounded labels; revision belongs only on the info metric.
