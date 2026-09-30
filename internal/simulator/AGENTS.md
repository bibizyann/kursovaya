# Equipment simulator guidance

- The simulator must be deterministic enough for repeatable integration and thesis experiments.
- `/healthz` represents process liveness; hardware faults normally affect `/readyz`, not liveness.
- Failure modes are controlled through explicit API calls so fault tests can record exact injection times.
- Keep response schemas stable and versioned under `/api/v1`.
- New fault modes require unit tests and a short description in the experiment runbook.
- Do not add random behavior without an explicit seed exposed in configuration and results.
