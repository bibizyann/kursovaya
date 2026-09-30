# ADR 0004: Assembly workshop model

- Status: accepted
- Date: 2026-10-01

## Context

The graduation project needs a concrete small-batch electronics assembly workshop. The existing camera and tester examples demonstrate deployment mechanics but do not describe the full assembly route or equipment-specific changeovers.

## Decision

The workshop receives bare printed circuit boards and components and produces assembled, programmed, and tested boards. PCB fabrication is outside its scope.

Use six workstations with eight devices as the reference equipment inventory:

1. Solder paste printing: NeoDen YS350.
2. SMD component placement: NeoDen4.
3. Reflow soldering: NeoDen IN6.
4. Optical inspection: NeoDen ND880 offline AOI.
5. Manual through-hole assembly and rework: Weller WT1010M.
6. Programming and functional testing: STLINK-V3SET, Keysight E36312A, and Keysight 34461A.

The [workshop model](../architecture/assembly-workshop.md) describes the route, reference device identifiers, changeover responsibilities, sources, and integration limitations.

Equipment, workstations, edge nodes, and Kubernetes workloads are distinct entities. Placement follows required capabilities; neither one node per machine nor one pod per machine is required. The exact mapping remains to be designed.

The MVP demonstrates the complete software-management scenario with simulators. Physical equipment and board movement are not simulated as a logistics system. Product A and B recipes must be specified before implementation; the example product family is not a finalized circuit design.

Software and recipe changes proceed sequentially by workstation. If a transition fails, affected workstations must automatically return to the previously accepted software and recipe revision, including partially changed equipment where applicable. The implementation of this recovery remains open: ADR 0002 continues to define Git ownership and rollback through a new commit. Offline recovery, persistence, and prevention of repeated failed rollouts require a follow-up design decision.

Physical preparation remains an operator responsibility. Software rollback does not undo stencil, feeder, or fixture changes.

## Consequences

- The domain model must distinguish equipment inventory, product recipes, and observed state. This decision does not introduce a new CRD or finalize API fields.
- Simulators need equipment-specific behavior and controllable faults rather than renamed copies of the current camera/tester behavior.
- Container deployment manages external applications and adapters, not vendor machine firmware or real-time motion control.
- Real integration requires verification of each device's documented interfaces. A simulator API does not imply that the physical device supports it.
- Readiness must refer to the same target revision and include applicable equipment, setup, and calibration checks. Changing a recipe is not equivalent to metrological calibration.
- Metrics and monitoring are a separate next design step.
- Existing samples and Kind capability labels remain examples of the current prototype until the new model is implemented and tested.
