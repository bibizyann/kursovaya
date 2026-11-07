package factorysource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"factory.local/factory-platform/internal/factory"
)

const infoMetric = "factory_equipment_info"

func telemetrySamples(now time.Time) []map[string]any {
	values := map[string]float64{"up": 1, "factory_equipment_telemetry_fresh": 1, "factory_equipment_last_observation_timestamp_seconds": float64(now.Unix()), "factory_equipment_healthy": 1, "factory_equipment_calibrated": 1, "factory_equipment_setup_complete": 1, infoMetric: 1, "factory_temperature_celsius": 0, "factory_operations_total": 10}
	result := make([]map[string]any, 0, len(values))
	for name, value := range values {
		labels := map[string]string{"__name__": name, "job": "factory-telemetry", "namespace": "factory-demo", "tech_process": testProcess, "equipment_id": testEquipment, "workstation": "reflow-soldering"}
		if name == infoMetric {
			labels["product"] = testProduct
			labels["recipe_id"] = "oven-v1"
			labels["revision"] = "rev-a"
		}
		result = append(result, map[string]any{"metric": labels, "value": []any{float64(now.Unix()), fmt.Sprint(value)}})
	}
	return result
}

func changeMetric(samples []map[string]any, name string, value string, age float64) {
	for _, s := range samples {
		if s["metric"].(map[string]string)["__name__"] == name {
			v := s["value"].([]any)
			v[1] = value
			v[0] = v[0].(float64) - age
		}
	}
}

func TestPrometheusFreshnessAndUnknown(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		name   string
		change func([]map[string]any) []map[string]any
		state  string
		up     bool
	}{
		{"fresh zero", func(s []map[string]any) []map[string]any { return s }, factory.StateFresh, true},
		{"fault", func(s []map[string]any) []map[string]any {
			changeMetric(s, "factory_equipment_healthy", "0", 0)
			return s
		}, factory.StateFresh, true},
		{"receiver down", func(s []map[string]any) []map[string]any { changeMetric(s, "up", "0", 0); return s }, "", false},
		{"old scrape", func(s []map[string]any) []map[string]any { changeMetric(s, "up", "1", 16); return s }, "", false},
		{"stale source despite fresh gauge", func(s []map[string]any) []map[string]any {
			changeMetric(s, "factory_equipment_last_observation_timestamp_seconds", fmt.Sprint(now.Unix()-21), 0)
			return s
		}, factory.StateStale, true},
		{"stale flag", func(s []map[string]any) []map[string]any {
			changeMetric(s, "factory_equipment_telemetry_fresh", "0", 0)
			return s
		}, factory.StateStale, true},
		{"future source", func(s []map[string]any) []map[string]any {
			changeMetric(s, "factory_equipment_last_observation_timestamp_seconds", fmt.Sprint(now.Unix()+6), 0)
			return s
		}, factory.StateUnknown, true},
		{"mismatched scrape", func(s []map[string]any) []map[string]any {
			changeMetric(s, "factory_equipment_healthy", "1", 5)
			return s
		}, factory.StateUnknown, true},
		{"NaN", func(s []map[string]any) []map[string]any {
			changeMetric(s, "factory_equipment_healthy", "NaN", 0)
			return s
		}, factory.StateUnknown, true},
		{"duplicate", func(s []map[string]any) []map[string]any {
			for _, v := range s {
				if v["metric"].(map[string]string)["__name__"] == infoMetric {
					return append(s, v)
				}
			}
			return s
		}, factory.StateUnknown, true},
		{"missing", func(s []map[string]any) []map[string]any {
			for _, v := range s {
				if v["metric"].(map[string]string)["__name__"] == "up" {
					return []map[string]any{v}
				}
			}
			return nil
		}, factory.StateMissing, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := tc.change(telemetrySamples(now))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				scope := fmt.Sprintf(`namespace="factory-demo",tech_process=%q`, testProcess)
				if r.URL.Path != "/api/v1/query" || !strings.Contains(r.URL.Query().Get("query"), scope) {
					t.Error("unscoped query", r.URL)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": samples}})
			}))
			defer server.Close()
			got, err := (Prometheus{URL: server.URL, Now: func() time.Time { return now }}).Read(context.Background(), "factory-demo", testProcess)
			if err != nil {
				t.Fatal(err)
			}
			observation := got.Equipment[testEquipment]
			if got.Receiver.Available != tc.up || observation.State != tc.state {
				t.Fatalf("got %#v %#v", got.Receiver, observation)
			}
			if tc.state != factory.StateFresh && (observation.Healthy != nil || len(observation.Readings) != 0 || observation.Revision != "") {
				t.Fatal("unknown observation exposed stale values")
			}
			if tc.name == "fresh zero" && (len(observation.Readings) != 1 || observation.Readings[0].Value != 0) {
				t.Fatal("valid zero lost")
			}
			if tc.name == "fault" && (observation.Healthy == nil || *observation.Healthy) {
				t.Fatal("fresh fault hidden")
			}
		})
	}
}

func TestPrometheusRejectsFailedAndPartialResponses(t *testing.T) {
	for _, body := range []string{`{"status":"error"}`, `{"status":"success","data":{"resultType":"matrix","result":[]}}`, `{"status":"success","warnings":["partial"],"data":{"resultType":"vector","result":[]}}`, `invalid`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := (Prometheus{URL: server.URL}).Read(context.Background(), "factory-demo", testProcess)
		server.Close()
		if err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
