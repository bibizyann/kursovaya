# Overview sources

- Adapters are read-only and must honor request cancellation and deadlines.
- Never retain successful source values after a failed read as current observations.
- Kubernetes reads must check process/template generations and workload owner references.
- Prometheus queries are fixed and scoped. Check scrape age, source time, receiver availability and duplicate series.
- Do not use monitoring availability as a process readiness input.
