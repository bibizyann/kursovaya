package factory

const (
	// StateReady confirms workload readiness for the target revision.
	StateReady = "Ready"
	// StateProgressing describes a revision still converging.
	StateProgressing = "Progressing"
	// StateDegraded describes a reported process failure.
	StateDegraded = "Degraded"
	// StateUnknown means current state cannot be established.
	StateUnknown = "Unknown"
	// StateFresh identifies a complete current telemetry observation.
	StateFresh = "Fresh"
	// StateStale identifies an observation outside the freshness window.
	StateStale = "Stale"
	// StateMissing identifies a device without an observation.
	StateMissing = "Missing"
	// StateNotConfigured identifies catalog equipment outside the selected template.
	StateNotConfigured = "NotConfigured"
)
