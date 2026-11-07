// Package factory builds the read-only workshop overview without infrastructure clients.
package factory

import (
	"time"

	"factory.local/factory-platform/internal/equipment"
)

// Source describes the availability of one independent observation source.
type Source struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Process is a revision-aware view of the selected technical process.
type Process struct {
	State            string     `json:"state"`
	Reason           string     `json:"reason,omitempty"`
	Message          string     `json:"message,omitempty"`
	Template         string     `json:"template,omitempty"`
	Revision         string     `json:"revision,omitempty"`
	ObservedRevision string     `json:"observedRevision,omitempty"`
	StatusCurrent    bool       `json:"statusCurrent"`
	Desired          *int32     `json:"desired,omitempty"`
	Ready            *int32     `json:"ready,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

// Pod describes a verified child workload instance, including previous revisions.
type Pod struct {
	Name        string `json:"name"`
	Node        string `json:"node,omitempty"`
	Phase       string `json:"phase"`
	Revision    string `json:"revision,omitempty"`
	Ready       bool   `json:"ready"`
	Terminating bool   `json:"terminating"`
	Reason      string `json:"reason,omitempty"`
}

// Component describes desired configuration and live Kubernetes readiness.
type Component struct {
	State   string            `json:"state"`
	Image   string            `json:"image,omitempty"`
	Product string            `json:"product,omitempty"`
	Recipe  *equipment.Recipe `json:"recipe,omitempty"`
	Desired int32             `json:"desired"`
	Ready   int32             `json:"ready"`
	Pods    []Pod             `json:"pods"`
}

// ClusterSnapshot is one consistent-enough read of process inputs and their children.
type ClusterSnapshot struct {
	Process    Process
	Components map[string]Component
}

// Reading is a fresh measurement with its contract unit.
type Reading struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Observation contains only current equipment values; stale observations retain their time only.
type Observation struct {
	State         string     `json:"state"`
	ObservedAt    *time.Time `json:"observedAt,omitempty"`
	Healthy       *bool      `json:"healthy,omitempty"`
	SetupComplete *bool      `json:"setupComplete,omitempty"`
	Calibrated    *bool      `json:"calibrated,omitempty"`
	Product       string     `json:"product,omitempty"`
	RecipeID      string     `json:"recipeId,omitempty"`
	Revision      string     `json:"revision,omitempty"`
	Readings      []Reading  `json:"readings"`
	Operations    *float64   `json:"operations,omitempty"`
	Errors        *float64   `json:"errors,omitempty"`
	Rejected      *float64   `json:"rejected,omitempty"`
}

// TelemetrySnapshot separates receiver reachability from per-device freshness.
type TelemetrySnapshot struct {
	Receiver  Source
	Equipment map[string]Observation
}

// Device joins catalog identity, desired configuration and independent telemetry.
type Device struct {
	equipment.Device
	Workload        Component   `json:"workload"`
	Telemetry       Observation `json:"telemetry"`
	RevisionMatches *bool       `json:"revisionMatches,omitempty"`
	GrafanaURL      string      `json:"grafanaUrl,omitempty"`
}

// Workstation groups devices by their logical production step.
type Workstation struct {
	ID      string   `json:"id"`
	Devices []Device `json:"devices"`
}

// Overview is the versioned HTTP representation of the reference workshop.
type Overview struct {
	SchemaVersion int           `json:"schemaVersion"`
	Namespace     string        `json:"namespace"`
	Name          string        `json:"name"`
	GeneratedAt   time.Time     `json:"generatedAt"`
	Kubernetes    Source        `json:"kubernetes"`
	Prometheus    Source        `json:"prometheus"`
	Receiver      Source        `json:"receiver"`
	Process       Process       `json:"process"`
	Workstations  []Workstation `json:"workstations"`
	GrafanaURL    string        `json:"grafanaUrl,omitempty"`
}

// Build assembles catalog-ordered cards while preserving independent source failures.
func Build(namespace, name string, now time.Time, cluster ClusterSnapshot, telemetry TelemetrySnapshot, kube, prom Source) Overview {
	result := Overview{SchemaVersion: 1, Namespace: namespace, Name: name, GeneratedAt: now.UTC(),
		Kubernetes: kube, Prometheus: prom, Receiver: telemetry.Receiver, Process: cluster.Process,
		Workstations: make([]Workstation, 0, 6)}
	if !kube.Available {
		result.Process = Process{State: StateUnknown, Reason: "KubernetesUnavailable"}
	}
	if !prom.Available {
		result.Receiver = Source{Reason: "PrometheusUnavailable"}
	}
	for _, identity := range equipment.Catalog() {
		device := Device{Device: identity, Workload: Component{State: StateUnknown, Pods: []Pod{}},
			Telemetry: Observation{State: StateUnknown, Readings: []Reading{}}}
		if kube.Available {
			if component, ok := cluster.Components[identity.ID]; ok {
				device.Workload = component
			} else if result.Process.Revision != "" {
				device.Workload.State = StateNotConfigured
			}
		}
		if prom.Available && telemetry.Receiver.Available {
			device.Telemetry.State = StateMissing
			if observation, ok := telemetry.Equipment[identity.ID]; ok {
				device.Telemetry = observation
			}
		}
		if device.Telemetry.State == StateFresh && result.Process.Revision != "" {
			matches := device.Telemetry.Revision == result.Process.Revision
			device.RevisionMatches = &matches
		}
		if len(result.Workstations) == 0 || result.Workstations[len(result.Workstations)-1].ID != identity.Workstation {
			result.Workstations = append(result.Workstations, Workstation{ID: identity.Workstation, Devices: []Device{}})
		}
		index := len(result.Workstations) - 1
		result.Workstations[index].Devices = append(result.Workstations[index].Devices, device)
	}
	return result
}
