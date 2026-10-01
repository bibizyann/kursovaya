# Kind environment guidance

- The development cluster models capabilities with node labels, not node names.
- The multi-node profile has an SMT worker, an inspection/test worker, and a simulated spare covering all six workstation capabilities.
- The single-node profile carries all capabilities. Labels represent simulated equipment availability, not real hardware failover.
- Changes to labels must be reflected in sample `TechProcess` selectors and e2e assertions.
- `make dev-down` is destructive to the local Kind cluster; it must target only `DEV_CLUSTER`.
- Do not put credentials or host-specific paths in Kind configuration.
