# Telemetry receiver guidance

- Accept only configured namespace/process/device streams; keep memory bounded by that inventory.
- Keep ordering and freshness logic separate from the Prometheus collector. Do not add Kubernetes or MQTT clients here.
- Inject time through Config.Now; tests must not sleep to exercise expiry.
- Copy incoming and outgoing mutable snapshot fields. A caller must not mutate a snapshot concurrently with Accept.
- Ignore older sequences and source lifetimes without refreshing the stored observation.
- Retain ordering state after expiry, but export equipment state, counters, readings, and revision info only while fresh.
- Never label metric series with message sequence, source instance ID, timestamps, or simulation seed.
- Use a custom collector with constant metrics, not counter increments on message receipt or global registrations.
- Changes to metric names, labels, and freshness semantics must update docs/architecture/telemetry.md and focused tests.
