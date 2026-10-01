package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"factory.local/factory-platform/internal/equipment"
)

func fixture(t *testing.T, equipmentID string) Snapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", equipmentID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestCatalogFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(equipment.Catalog()) {
		t.Fatal("fixtures must cover exactly the equipment catalog")
	}
	for _, device := range equipment.Catalog() {
		t.Run(device.ID, func(t *testing.T) {
			snapshot := fixture(t, device.ID)
			if snapshot.EquipmentID != device.ID || snapshot.Workstation != device.Workstation {
				t.Fatal("fixture identity differs from catalog")
			}
			definitions, err := ReadingDefinitions(device.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(definitions) != len(snapshot.Readings) {
				t.Fatal("fixture must demonstrate every supported reading")
			}
			data, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(bytes.NewReader(data))
			if err != nil || !reflect.DeepEqual(snapshot, decoded) {
				t.Fatalf("round trip failed: %v", err)
			}
		})
	}
}

func TestValidateRejectsInvalidSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Snapshot)
	}{
		{"schema version", func(s *Snapshot) { s.SchemaVersion++ }},
		{"empty namespace", func(s *Snapshot) { s.Namespace = "" }},
		{"namespace topic wildcard", func(s *Snapshot) { s.Namespace = "+" }},
		{"namespace too long", func(s *Snapshot) { s.Namespace = strings.Repeat("a", 64) }},
		{"process topic path", func(s *Snapshot) { s.TechProcess = "line/one" }},
		{"process empty DNS label", func(s *Snapshot) { s.TechProcess = "line..one" }},
		{"unknown equipment", func(s *Snapshot) { s.EquipmentID = "unknown" }},
		{"wrong workstation", func(s *Snapshot) { s.Workstation = "programming-test" }},
		{"empty instance", func(s *Snapshot) { s.SourceInstanceID = "" }},
		{"instance topic wildcard", func(s *Snapshot) { s.SourceInstanceID = "run/#" }},
		{"zero sequence", func(s *Snapshot) { s.Sequence = 0 }},
		{"missing observation time", func(s *Snapshot) { s.ObservedAt = time.Time{} }},
		{"missing start time", func(s *Snapshot) { s.SourceStartedAt = time.Time{} }},
		{"observation before start", func(s *Snapshot) { s.ObservedAt = s.SourceStartedAt.Add(-time.Second) }},
		{"unencodable time", func(s *Snapshot) { s.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"missing product", func(s *Snapshot) { s.Product = "" }},
		{"missing recipe", func(s *Snapshot) { s.RecipeID = "" }},
		{"missing revision", func(s *Snapshot) { s.Revision = "" }},
		{"revision whitespace", func(s *Snapshot) { s.Revision = "rev\n1" }},
		{"rejections exceed completed", func(s *Snapshot) { s.Counters.RejectedOperationsTotal = 13 }},
		{"oven cannot reject an inspection", func(s *Snapshot) { s.Counters.RejectedOperationsTotal = 1 }},
		{"unsupported gauge", func(s *Snapshot) { s.Readings["voltage_volts"] = 1 }},
		{"incorrect unit key", func(s *Snapshot) { s.Readings["temperature_fahrenheit"] = 100 }},
		{"duration without completed operation", func(s *Snapshot) { s.Counters.OperationsTotal = 0 }},
		{"negative duration", func(s *Snapshot) { s.Readings["last_operation_duration_seconds"] = -1 }},
		{"below absolute zero", func(s *Snapshot) { s.Readings["temperature_celsius"] = -274 }},
		{"nonboolean readiness", func(s *Snapshot) { s.Readings["temperature_ready"] = 0.5 }},
		{"NaN", func(s *Snapshot) { s.Readings["temperature_celsius"] = math.NaN() }},
		{"infinity", func(s *Snapshot) { s.Readings["temperature_celsius"] = math.Inf(1) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			snapshot := fixture(t, "reflow-oven-01")
			test.change(&snapshot)
			if err := snapshot.Validate(); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}

func TestValidateVoltageLimits(t *testing.T) {
	snapshot := fixture(t, "test-multimeter-01")
	snapshot.Readings["min_voltage_volts"] = 10
	if err := snapshot.Validate(); err == nil {
		t.Fatal("accepted reversed limits")
	}
	delete(snapshot.Readings, "min_voltage_volts")
	if err := snapshot.Validate(); err == nil {
		t.Fatal("accepted an incomplete limit pair")
	}
	delete(snapshot.Readings, "max_voltage_volts")
	snapshot.Readings["voltage_volts"] = -1
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("signed voltage without limits is valid: %v", err)
	}
}

func TestValidUnavailableReadingsAndRejectedProduct(t *testing.T) {
	snapshot := fixture(t, "optical-inspector-01")
	// A rejected product must not imply equipment failure.
	if !snapshot.State.Healthy || snapshot.Counters.RejectedOperationsTotal == 0 {
		t.Fatal("fixture must describe a healthy inspector rejecting a product")
	}
	// False conditions, zero counters, and absent measurements are valid at startup.
	snapshot.State = EquipmentState{}
	snapshot.Counters = Counters{}
	snapshot.Readings = nil
	seed := int64(0)
	snapshot.SimulationSeed = &seed
	snapshot.TechProcess = "line.one"
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(snapshot, decoded) {
		t.Fatalf("unavailable readings or seed zero lost: %v", err)
	}
}

func TestDecodeRequiresExplicitStateAndCounters(t *testing.T) {
	data, err := json.Marshal(fixture(t, "reflow-oven-01"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"schema_version", "namespace", "tech_process", "equipment_id", "workstation",
		"source_instance_id", "source_started_at", "sequence", "observed_at",
		fieldProduct, fieldRecipeID, fieldRevision, fieldState, fieldCounters,
		"state.healthy", "state.calibrated", "state.setup_complete",
		"counters.operations_total", "counters.operation_errors_total", "counters.rejected_operations_total",
	} {
		for _, useNull := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/null=%t", path, useNull), func(t *testing.T) {
				var object map[string]any
				if err := json.Unmarshal(data, &object); err != nil {
					t.Fatal(err)
				}
				target := object
				parts := strings.Split(path, ".")
				if len(parts) == 2 {
					target = object[parts[0]].(map[string]any)
				}
				key := parts[len(parts)-1]
				if useNull {
					target[key] = nil
				} else {
					delete(target, key)
				}
				encoded, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Decode(bytes.NewReader(encoded)); err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("expected missing %s error, got %v", path, err)
				}
			})
		}
	}
}

func TestDecodeRejectsMalformedMessages(t *testing.T) {
	data, err := json.Marshal(fixture(t, "reflow-oven-01"))
	if err != nil {
		t.Fatal(err)
	}
	valid := string(data)
	for name, input := range map[string]string{
		"empty":              "",
		"null":               "null",
		"truncated":          valid[:len(valid)-1],
		"trailing object":    valid + "{}",
		"trailing garbage":   valid + "oops",
		"oversized":          strings.Repeat(" ", MaxMessageBytes) + valid,
		"unknown field":      strings.Replace(valid, `"schema_version":1`, `"extra":true,"schema_version":1`, 1),
		"unknown state":      strings.Replace(valid, `"healthy":true`, `"health":true,"healthy":true`, 1),
		"negative counter":   strings.Replace(valid, `"operations_total":12`, `"operations_total":-1`, 1),
		"fractional counter": strings.Replace(valid, `"operations_total":12`, `"operations_total":1.5`, 1),
		"numeric overflow":   strings.Replace(valid, `"operations_total":12`, `"operations_total":18446744073709551616`, 1),
		"null reading":       strings.Replace(valid, `"temperature_celsius":239.7`, `"temperature_celsius":null`, 1),
		"string reading":     strings.Replace(valid, `"temperature_celsius":239.7`, `"temperature_celsius":"239.7"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(input)); err == nil {
				t.Fatal("accepted malformed message")
			}
		})
	}
}

func TestDefinitionsAreIndependent(t *testing.T) {
	definitions, err := ReadingDefinitions("reflow-oven-01")
	if err != nil {
		t.Fatal(err)
	}
	definitions[0].Name = "corrupted"
	if err := fixture(t, "reflow-oven-01").Validate(); err != nil {
		t.Fatalf("caller modified shared definitions: %v", err)
	}
	if _, err := ReadingDefinitions("unknown"); err == nil {
		t.Fatal("accepted unknown device")
	}
}
