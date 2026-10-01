// Package telemetry defines versioned equipment snapshots without transport dependencies.
package telemetry

import "time"

// SchemaVersion is the supported snapshot wire version.
const SchemaVersion = 1

// MaxMessageBytes bounds the size of a JSON snapshot.
const MaxMessageBytes = 64 * 1024

// EquipmentState reports independent readiness checks at the observation time.
type EquipmentState struct {
	// Healthy reports whether the equipment is functioning.
	Healthy bool `json:"healthy"`
	// Calibrated reports the simulated calibration check.
	Calibrated bool `json:"calibrated"`
	// SetupComplete reports preparation for the current recipe.
	SetupComplete bool `json:"setup_complete"`
}

// Counters contains cumulative counts since the source instance started.
type Counters struct {
	// OperationsTotal counts completed operations, including rejected inspections.
	OperationsTotal uint64 `json:"operations_total"`
	// OperationErrorsTotal counts attempts that failed to complete.
	OperationErrorsTotal uint64 `json:"operation_errors_total"`
	// RejectedOperationsTotal counts completed inspections that rejected the product.
	RejectedOperationsTotal uint64 `json:"rejected_operations_total"`
}

// Snapshot is a complete observation of one device in one technical process.
type Snapshot struct {
	// SchemaVersion identifies the wire contract.
	SchemaVersion int `json:"schema_version"`
	// Namespace scopes the technical process.
	Namespace string `json:"namespace"`
	// TechProcess is the technical process name, not a pod name.
	TechProcess string `json:"tech_process"`
	// EquipmentID is the stable catalog device identifier.
	EquipmentID string `json:"equipment_id"`
	// Workstation is the logical workstation from the equipment catalog.
	Workstation string `json:"workstation"`
	// SourceInstanceID uniquely identifies a publisher process lifetime.
	SourceInstanceID string `json:"source_instance_id"`
	// SourceStartedAt orders publisher lifetimes within the same device stream.
	SourceStartedAt time.Time `json:"source_started_at"`
	// Sequence starts at one and increases for each snapshot in this lifetime.
	Sequence uint64 `json:"sequence"`
	// ObservedAt is when the source sampled this snapshot.
	ObservedAt time.Time `json:"observed_at"`
	// Product identifies the product selected at source startup.
	Product string `json:"product"`
	// RecipeID identifies the applied recipe independently of the software revision.
	RecipeID string `json:"recipe_id"`
	// Revision is the applied process revision, normally FACTORY_REVISION.
	Revision string `json:"revision"`
	// SimulationSeed records the optional simulator seed, including zero.
	SimulationSeed *int64 `json:"simulation_seed,omitempty"`
	// State contains the source's readiness observations.
	State EquipmentState `json:"state"`
	// Readings contains available gauges; omitted readings are unavailable.
	Readings map[string]float64 `json:"readings,omitempty"`
	// Counters contains cumulative operation statistics for this source lifetime.
	Counters Counters `json:"counters"`
}
