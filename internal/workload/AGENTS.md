# Workload builder guidance

- This package contains pure, deterministic object construction and validation.
- It must not make Kubernetes API calls.
- Keep names, selectors, labels, annotations, and revision hashes stable because updates depend on them.
- Workload selectors are immutable after creation; changing selector construction requires an API migration plan.
- Target and capability selectors are combined with AND semantics.
- Parameter ConfigMaps are revision-specific and immutable.
- Default rollout behavior must remain safe for exclusive hardware.
- Add focused unit tests for every change in object construction or revision calculation.
