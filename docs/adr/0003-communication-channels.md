# ADR 0003: Communication channels

- Status: accepted
- Date: 2026-08-30

## Decision

- Kubernetes resources carry desired process state.
- MQTT carries telemetry and retained edge status with sequence identifiers.
- gRPC is used only for local typed interactions between the edge agent and equipment-facing workloads.
- Prometheus metrics describe platform health and rollout performance.

The initial MQTT broker is Eclipse Mosquitto. Kafka is outside MVP scope. Yandex IoT Core is not used because it is being discontinued.

## Consequences

- There is no split-brain command path between MQTT and Kubernetes.
- Telemetry outages do not stop an already running production process.
- The edge agent must persist a bounded telemetry queue and replay it idempotently.
