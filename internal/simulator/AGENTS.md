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
