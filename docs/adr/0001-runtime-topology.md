# ADR 0001: Runtime topology

- Status: accepted
- Date: 2026-08-30

## Context

The project needs a lightweight development environment, a local factory control plane, and an edge mode capable of continuing the current workload during connectivity loss.

## Decision

Development and automated integration tests use Kind. The first functional MVP uses ordinary Kubernetes workers and does not depend on KubeEdge.

The pilot topology is introduced only after the core operator is stable:

- three K3s server nodes with embedded etcd;
- KubeEdge CloudCore connected to that Kubernetes control plane;
- EdgeCore on hosts selected for the KubeEdge pilot;
- no simultaneous K3s agent and EdgeCore installation as competing worker runtimes on one host.

A compatibility spike with one EdgeCore node is required before expanding the pilot.

## Consequences

- Operator development remains fast and reproducible.
- KubeEdge-specific failures cannot block the MVP.
- Offline behavior must be verified again on the pilot topology.
- The edge agent remains an application-level hardware and telemetry component, not a node orchestrator.
