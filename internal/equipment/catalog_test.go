package equipment

import "testing"

func TestInventoryAndRecipes(t *testing.T) {
	devices := Catalog()
	if len(devices) != 8 {
		t.Fatalf("devices = %d", len(devices))
	}
	stations, ids := map[string]bool{}, map[string]bool{}
	for _, d := range devices {
		if ids[d.ID] {
			t.Fatalf("duplicate ID %s", d.ID)
		}
		ids[d.ID], stations[d.Workstation] = true, true
		a, err := Profile(d.ID, boardA)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Profile(d.ID, boardB)
		if err != nil {
			t.Fatal(err)
		}
		if a.ID == b.ID || a.Operation == "" || a.Operation != b.Operation {
			t.Fatalf("invalid profiles for %s", d.ID)
		}
		for _, r := range []Recipe{a, b} {
			result := Execute(r, 1)
			if !result.Accepted || len(result.Readings) == 0 {
				t.Fatalf("missing operation for %s", d.ID)
			}
		}
	}
	if len(stations) != 6 {
		t.Fatalf("workstations = %d", len(stations))
	}
	devices[0].ID = "changed"
	if Catalog()[0].ID == "changed" {
		t.Fatal("catalog shares mutable storage")
	}
	if _, err := Profile("unknown", boardA); err == nil {
		t.Fatal("accepted unknown device")
	}
	if _, err := Profile("programmer-01", "unknown"); err == nil {
		t.Fatal("accepted unknown product")
	}
}

func TestDeviceSpecificResults(t *testing.T) {
	cases := []struct {
		id, product, reading string
		want                 float64
		artifact             string
	}{
		{"paste-printer-01", boardA, "coverage_percent", 98, "board-a-stencil"},
		{"component-placer-01", boardB, "placed_components", 64, "board-b-placement-v1"},
		{"reflow-oven-01", boardA, "peak_temperature_c", 240, "board-a-thermal-v1"},
		{opticalInspectorID, boardA, "defect_count", 0, "board-a-aoi-v1"},
		{"manual-soldering-01", boardB, "completed_joints", 12, "board-b-work-instruction-v1"},
		{"programmer-01", boardB, "verified", 1, "board-b-firmware-v1.bin"},
		{"test-power-supply-01", boardB, "voltage_v", 5, ""},
		{multimeterID, boardA, "voltage_v", 3.3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			r, err := Profile(tc.id, tc.product)
			if err != nil {
				t.Fatal(err)
			}
			got := Execute(r, 1)
			value, ok := got.Readings[tc.reading]
			if !ok || value != tc.want || got.Artifact != tc.artifact {
				t.Fatalf("result = %+v", got)
			}
		})
	}
	for _, id := range []string{opticalInspectorID, multimeterID} {
		r, _ := Profile(id, boardA)
		if Execute(r, 10).Accepted || !Execute(r, 11).Accepted {
			t.Fatalf("invalid deterministic reject for %s", id)
		}
	}
}
