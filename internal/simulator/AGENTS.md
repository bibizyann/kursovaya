# Equipment simulator guidance

- The simulator must be deterministic enough for repeatable integration and thesis experiments.
- `/healthz` represents process liveness; hardware faults normally affect `/readyz`, not liveness.
- Failure modes are controlled through explicit API calls so fault tests can record exact injection times.
- Keep response schemas stable and versioned under `/api/v1`.
- New fault modes require unit tests and a short description in the experiment runbook.
- Do not add random behavior without an explicit seed exposed in configuration and results.

- Catalog identity and built-in educational recipes live in `internal/equipment`; keep them independent of Kubernetes and HTTP.
- Catalog devices use `/api/v1/operations`; legacy camera/tester clients retain `/api/v1/measurements`.
- Simulator setup confirmation is separate from calibration. Never report an operation after equipment state changes during its delay.
- Recipes are fixed for the process lifetime and selected at startup. HTTP fault/setup controls are experiment hooks, not a desired-state update channel.

- Periodic telemetry is opt-in through TelemetryConfig; require a positive interval, scope, and explicit seed, including zero.
- Use the same locked readiness state and operation accounting for HTTP and automatic cycles. Snapshot sequence is separate from operation sequence.
- Generate advances one cycle without I/O or sleeping. RunGenerator provides scheduling and calls the sink outside the state lock.
- Unready cycles emit heartbeats without attempting operations. Failed HTTP attempts and interrupted operations increment errors; rejected products increment completed and rejected counters.
- Slow automatic operations remain pending across heartbeats and are invalidated by the same state changes as HTTP operations.
- Test generation with Config.Now and controlled ticks, without wall-clock sleeps. Seed reproducibility also requires the same clock/control/operation order; source instance IDs must remain unique across runs.
- Sink errors must not change readiness, retry old snapshots, or create an unbounded queue.
