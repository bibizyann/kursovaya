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
