// Package equipment describes the reference assembly workshop without Kubernetes dependencies.
package equipment

import "fmt"

const (
	neoDenManufacturer         = "NeoDen"
	programmingTestWorkstation = "programming-test"
	opticalInspectorID         = "optical-inspector-01"
	multimeterID               = "test-multimeter-01"
	boardA                     = "board-a"
	boardB                     = "board-b"
)

// Device is one simulated device in the reference workshop inventory.
type Device struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Workstation  string `json:"workstation"`
	Capability   string `json:"capability"`
}

var inventory = [...]Device{
	{"paste-printer-01", "paste-printer", neoDenManufacturer, "YS350", "paste-printing", "paste-printing"},
	{"component-placer-01", "component-placer", neoDenManufacturer, "NeoDen4", "component-placement", "component-placement"},
	{"reflow-oven-01", "reflow-oven", neoDenManufacturer, "IN6", "reflow-soldering", "reflow-soldering"},
	{opticalInspectorID, "optical-inspector", neoDenManufacturer, "ND880", "optical-inspection", "optical-inspection"},
	{"manual-soldering-01", "manual-soldering", "Weller", "WT1010M", "manual-assembly", "manual-assembly"},
	{"programmer-01", "programmer", "STMicroelectronics", "STLINK-V3SET", programmingTestWorkstation, programmingTestWorkstation},
	{"test-power-supply-01", "power-supply", "Keysight", "E36312A", programmingTestWorkstation, programmingTestWorkstation},
	{multimeterID, "multimeter", "Keysight", "34461A", programmingTestWorkstation, programmingTestWorkstation},
}

// Catalog returns an independent copy of the workshop inventory.
func Catalog() []Device {
	return append([]Device(nil), inventory[:]...)
}

// Lookup resolves a stable equipment identifier.
func Lookup(id string) (Device, error) {
	for _, device := range inventory {
		if device.ID == id {
			return device, nil
		}
	}
	return Device{}, fmt.Errorf("unknown equipment %q", id)
}

// Recipe contains educational simulation settings, not qualified manufacturing parameters.
// Only settings relevant to a device are populated.
type Recipe struct {
	ID                 string  `json:"id"`
	Product            string  `json:"product"`
	Operation          string  `json:"operation"`
	Program            string  `json:"program,omitempty"`
	Stencil            string  `json:"stencil,omitempty"`
	Fixture            string  `json:"fixture,omitempty"`
	ComponentCount     int     `json:"componentCount,omitempty"`
	PeakTemperatureC   float64 `json:"peakTemperatureC,omitempty"`
	SolderTemperatureC float64 `json:"solderTemperatureC,omitempty"`
	JointCount         int     `json:"jointCount,omitempty"`
	Firmware           string  `json:"firmware,omitempty"`
	VoltageV           float64 `json:"voltageV,omitempty"`
	CurrentLimitA      float64 `json:"currentLimitA,omitempty"`
	MinVoltageV        float64 `json:"minVoltageV,omitempty"`
	MaxVoltageV        float64 `json:"maxVoltageV,omitempty"`
}

// Profile returns the versioned built-in recipe for a device and product.
func Profile(id, product string) (Recipe, error) {
	device, err := Lookup(id)
	if err != nil {
		return Recipe{}, err
	}
	if product != boardA && product != boardB {
		return Recipe{}, fmt.Errorf("unsupported product %q", product)
	}
	r := Recipe{ID: product + "/" + id + "/v1", Product: product}
	components, peak, joints, voltage := 42, 240.0, 8, 3.3
	if product == boardB {
		components, peak, joints, voltage = 64, 245, 12, 5
	}
	switch device.Kind {
	case "paste-printer":
		r.Operation, r.Stencil = "print-paste", product+"-stencil"
	case "component-placer":
		r.Operation, r.Program, r.ComponentCount = "place-components", product+"-placement-v1", components
	case "reflow-oven":
		r.Operation, r.Program, r.PeakTemperatureC = "reflow", product+"-thermal-v1", peak
	case "optical-inspector":
		r.Operation, r.Program = "inspect", product+"-aoi-v1"
	case "manual-soldering":
		r.Operation, r.Program, r.SolderTemperatureC, r.JointCount = "solder", product+"-work-instruction-v1", 350, joints
	case "programmer":
		r.Operation, r.Firmware, r.Fixture = "program", product+"-firmware-v1.bin", product+"-fixture"
	case "power-supply":
		r.Operation, r.VoltageV, r.CurrentLimitA = "supply-power", voltage, 0.5
	case "multimeter":
		r.Operation, r.VoltageV, r.MinVoltageV, r.MaxVoltageV = "measure-voltage", voltage, voltage*0.95, voltage*1.05
	}
	return r, nil
}

// Result contains device-specific deterministic operation output.
type Result struct {
	Operation string             `json:"operation"`
	Accepted  bool               `json:"accepted"`
	Readings  map[string]float64 `json:"readings,omitempty"`
	Artifact  string             `json:"artifact,omitempty"`
}

// Execute simulates a completed operation. Every tenth inspection or voltage check fails.
// It does not model physical movement, actual firmware writes, or thermal dynamics.
func Execute(recipe Recipe, sequence uint64) Result {
	r := Result{Operation: recipe.Operation, Accepted: true, Readings: make(map[string]float64)}
	switch recipe.Operation {
	case "print-paste":
		r.Artifact, r.Readings["coverage_percent"] = recipe.Stencil, 98
	case "place-components":
		r.Artifact, r.Readings["placed_components"] = recipe.Program, float64(recipe.ComponentCount)
	case "reflow":
		r.Artifact, r.Readings["peak_temperature_c"] = recipe.Program, recipe.PeakTemperatureC
	case "inspect":
		r.Artifact = recipe.Program
		r.Readings["defect_count"] = 0
		if sequence%10 == 0 {
			r.Accepted, r.Readings["defect_count"] = false, 1
		}
	case "solder":
		r.Artifact = recipe.Program
		r.Readings["tip_temperature_c"], r.Readings["completed_joints"] = recipe.SolderTemperatureC, float64(recipe.JointCount)
	case "program":
		r.Artifact, r.Readings["verified"] = recipe.Firmware, 1
	case "supply-power":
		r.Readings["voltage_v"], r.Readings["current_limit_a"] = recipe.VoltageV, recipe.CurrentLimitA
	case "measure-voltage":
		voltage := recipe.VoltageV
		if sequence%10 == 0 {
			voltage = recipe.MaxVoltageV + 0.1
		}
		r.Readings["voltage_v"] = voltage
		r.Accepted = voltage >= recipe.MinVoltageV && voltage <= recipe.MaxVoltageV
	default:
		r.Accepted = false
	}
	return r
}
