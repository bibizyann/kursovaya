# Telemetry deployment guidance

- This is a local development profile, not a production MQTT security configuration.
- Broker and receiver belong to the platform namespace and have no TechProcess owner references.
- Use one broker and one receiver replica with Recreate; neither has persistent storage.
- Keep Services internal (ClusterIP). Anonymous MQTT is for the isolated Kind stand only.
- Run containers without root, privilege escalation, or service-account tokens.
- Receiver probes check its HTTP process, not broker connectivity or telemetry freshness.
- Keep the expected inventory aligned with the telemetry sample overlay and the equipment catalog.
- Changes to the full stand must be covered by the eight-device A/B e2e scenario.
