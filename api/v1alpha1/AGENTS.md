# API guidance

- This directory defines `production.factory.local/v1alpha1` resources.
- Treat serialized field names and enum values as public API.
- Prefer additive changes while the API remains `v1alpha1`.
- Use validation/default markers for constraints that the API server can enforce.
- Keep status reconstructable from cluster state; status must never become a second desired-state channel.
- Use `metav1.Condition`, `observedGeneration`, stable reason strings, and concise diagnostic messages.
- Run `make generate manifests` after changing API types or markers.
- Never manually edit `zz_generated.deepcopy.go`.
