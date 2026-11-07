# Factory overview

- Keep view models and aggregation independent of Kubernetes and HTTP clients.
- Missing values are unknown, never healthy zeroes. Telemetry cannot change process readiness.
- Preserve all eight catalog devices even when a source is unavailable.
- Do not confuse logical workstations with Kubernetes nodes.
- Changes to desired state belong to the future GitOps write path, not this read model.
