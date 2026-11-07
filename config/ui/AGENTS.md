# Local overview deployment

- Install in factory-ui; read only the explicitly configured factory-demo namespace.
- Keep RBAC read-only. Do not grant Secrets, nodes, pod exec, or mutation access.
- Readiness/liveness describe the API process, not Kubernetes or monitoring availability.
- Keep Services ClusterIP and expose the UI through loopback port-forward.
- The manifest is a single-line demo profile; document scope changes explicitly.
