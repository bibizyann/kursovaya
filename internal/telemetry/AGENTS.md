# Telemetry contract guidance

- Keep this package independent of Kubernetes, MQTT, Prometheus, clocks, and mutable receiver state.
- Schema v1 carries complete snapshots, not increments or control commands.
- Preserve equipment identity separately from the namespace/process scope and the source instance.
- Define supported readings and units explicitly; never accept arbitrary metric names.
- Missing readings are unavailable, not zero. Counters are cumulative within one source instance.
- Decode untrusted JSON through Decode; Validate checks already constructed snapshots.
- Ordering, freshness, counter monotonicity, and instance replacement are receiver responsibilities.
- Keep JSON fixtures for every catalog device and the contract documentation synchronized.
