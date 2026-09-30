# ADR 0002: Desired state and resource ownership

- Status: accepted
- Date: 2026-08-30

## Context

The design contains a web API, Argo CD, a Kubernetes operator, and Helm. Allowing more than one of them to manage the same workload would cause drift and ambiguous rollback behavior.

## Decision

The target write path is:

```text
UI -> Factory API -> Git commit -> Argo CD -> TechProcess CR -> operator -> child resources
```

Ownership is divided as follows:

- Argo CD owns Git-managed CRs and platform installation manifests.
- The operator owns resources derived from a `TechProcess`.
- Helm packages the platform and catalog objects; Helm is not invoked inside the first operator implementation.
- Factory API reads cluster status but writes desired state through Git.
- Rollback creates a new Git commit that restores a previously accepted revision.

Direct `kubectl apply` is allowed only for local development and tests.

## Consequences

- Every managed resource has one controller responsible for its desired fields.
- Production changes are auditable in Git.
- The backend needs optimistic concurrency around Git updates.
- Argo CD rollback commands are not the business rollback mechanism.
