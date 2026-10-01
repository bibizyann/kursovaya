# MQTT telemetry guidance

- Use MQTT 3.1.1, QoS 1, exact per-device topics, and retained full snapshots.
- Keep receiver ordering/freshness in internal/telemetryreceiver; transport must validate topic/payload identity before Accept.
- Use clean sessions and a fresh Paho client/store per connection. Disable library reconnect and offline buffering; reconnect explicitly without replaying old publications.
- Publisher input must never wait for network I/O. Keep at most one pending snapshot and one in flight; clear pending data across connections.
- Bound dial, handshake, subscribe, publish acknowledgment, and shutdown waits. Close the owned socket on cancellation.
- MQTT outages must not change simulator readiness or pause automatic cycles.
- Never log credentials or complete rejected payloads. Local examples use a loopback-only Mosquitto listener.
- Keep deterministic unit tests broker-free. Broker integration tests use the mqttintegration build tag and an isolated Mosquitto instance.
