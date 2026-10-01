# ADR 0005: Reference equipment simulation

- Status: accepted
- Date: 2026-10-01
- Refines: [ADR 0004](0004-assembly-workshop-model.md)
- Refined by: [ADR 0006](0006-simulated-telemetry.md) for optional periodic telemetry and source lifetime semantics. The HTTP operation API remains unchanged.

## Context

The workshop model needs runnable equipment instances before monitoring can use meaningful observations. The existing component API already supports arguments, capability selectors, and readiness probes. Real device drivers are outside the simulator MVP.

## Decision

Keep a pure Go catalog and educational product recipes in `internal/equipment`. Each of the eight stable device IDs has a model, kind, workstation, capability, and deterministic operation. Recipes for board A and B are versioned built-in simulation fixtures, not qualified manufacturing settings. A recipe change requires a new simulator build and template revision; externally managed recipes can be added later.

Run one simulator process and Deployment per device. The eight instances belong logically to six workstations; the programming station has three independent device simulators. This first step does not coordinate their measurements into a functional test of the same board or guarantee their co-location on one host. Board movement and shared board identities are not modeled.

Use the existing TechProcessTemplate component name for the device ID and arguments to select the device and product. No new CRD or workload-builder behavior is required. Expose the deployed FACTORY_REVISION in status and operation results. The descriptive product in TechProcess parameters is not consumed by the simulator; startup arguments select its recipe.

The single-node Kind profile has all six capabilities. The multi-node profile has an SMT worker, an inspection/test worker, and a simulated spare. These labels describe simulator availability; they do not imply that physical machines can move between hosts.

Catalog devices expose POST /api/v1/operations. Keep POST /api/v1/measurements for legacy camera/tester instances. Existing health, readiness, mode, and calibration endpoints remain available. Preparation confirmation is separate from calibration and can be required at startup or invalidated with the unprepared fault mode. HTTP setup/fault endpoints are local experiment controls; desired product changes still arrive through workload replacement.

## Consequences

- Default examples start with preparation already confirmed; experiments can explicitly require confirmation.
- Rejected inspection results do not mean equipment is unhealthy. Every tenth AOI or voltage-check operation deterministically rejects the simulated product.
- Operations remain request-driven by default. ADR 0006 adds opt-in background cycles and JSON telemetry snapshots; physical timing and MQTT publishing are not implemented at this stage.
- Sequence numbers and fault/setup state reset on process restart. ADR 0006 replaces the planned edge-agent persistence with explicit telemetry source lifetimes; missing history is not replayed in the MVP.
- Sequential workstation rollout and automatic rollback from ADR 0004 remain pending. Current component updates retain the existing operator behavior.
- Sample/catalog/Kind consistency tests and an e2e A-to-B scenario validate this mapping.
