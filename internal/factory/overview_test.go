package factory

import (
	"testing"
	"time"
)

func TestBuildKeepsInventoryAndIndependentFailures(t *testing.T) {
	healthy := true
	cluster := ClusterSnapshot{Process: Process{State: StateReady, Revision: "new"}}
	telemetry := TelemetrySnapshot{Receiver: Source{Available: true}, Equipment: map[string]Observation{
		"reflow-oven-01": {State: StateFresh, Revision: "old", Healthy: &healthy},
	}}
	available := Source{Available: true}
	for _, tc := range []struct {
		name                 string
		kube, prom           Source
		process, observation string
	}{
		{"all available", available, available, StateReady, StateFresh},
		{"monitoring down", available, Source{}, StateReady, StateUnknown},
		{"cluster down", Source{}, available, StateUnknown, StateFresh},
		{"both down", Source{}, Source{}, StateUnknown, StateUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Build("factory-demo", "line-one", time.Now(), cluster, telemetry, tc.kube, tc.prom)
			count := 0
			for _, station := range got.Workstations {
				count += len(station.Devices)
			}
			if len(got.Workstations) != 6 || count != 8 {
				t.Fatalf("inventory: %#v", got.Workstations)
			}
			oven := got.Workstations[2].Devices[0]
			if got.Process.State != tc.process || oven.Telemetry.State != tc.observation {
				t.Fatalf("process=%s observation=%s", got.Process.State, oven.Telemetry.State)
			}
			if tc.kube.Available && tc.prom.Available && (oven.RevisionMatches == nil || *oven.RevisionMatches) {
				t.Fatal("old telemetry must not confirm the target revision")
			}
			if !tc.kube.Available && oven.RevisionMatches != nil {
				t.Fatal("revision comparison requires Kubernetes")
			}
			if tc.prom.Available && got.Workstations[0].Devices[0].Telemetry.State != StateMissing {
				t.Fatal("never-seen device must remain visible as missing")
			}
		})
	}
}
