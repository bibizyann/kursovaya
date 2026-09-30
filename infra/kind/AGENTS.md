# Kind environment guidance

- The development cluster models capabilities with node labels, not node names.
- Keep at least one camera node, one tester node, and one compatible spare node.
- Changes to labels must be reflected in sample `TechProcess` selectors and e2e assertions.
- `make dev-down` is destructive to the local Kind cluster; it must target only `DEV_CLUSTER`.
- Do not put credentials or host-specific paths in Kind configuration.
