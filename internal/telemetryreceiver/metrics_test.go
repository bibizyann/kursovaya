package telemetryreceiver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/telemetry"
)

const (
	metricFresh       = "factory_equipment_telemetry_fresh"
	metricObserved    = "factory_equipment_last_observation_timestamp_seconds"
	metricHealthy     = "factory_equipment_healthy"
	metricInfo        = "factory_equipment_info"
	metricOperations  = "factory_operations_total"
	metricTemperature = "factory_temperature_celsius"
)

type metricSample struct {
	value   float64
	counter bool
	labels  map[string]string
}

func gather(t *testing.T, r *Receiver) map[string][]metricSample {
	t.Helper()
	registry := prometheus.NewPedanticRegistry()
	if err := registry.Register(r.Collector()); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string][]metricSample)
	for _, family := range families {
		for _, metric := range family.Metric {
			s := metricSample{labels: make(map[string]string)}
			if metric.Counter != nil {
				s.value, s.counter = metric.Counter.GetValue(), true
			} else {
				s.value = metric.Gauge.GetValue()
			}
			for _, label := range metric.Label {
				s.labels[label.GetName()] = label.GetValue()
			}
			result[family.GetName()] = append(result[family.GetName()], s)
		}
	}
	return result
}

func onlyMetric(t *testing.T, metrics map[string][]metricSample, name string) metricSample {
	t.Helper()
	if len(metrics[name]) != 1 {
		t.Fatalf("expected one %s sample, got %d", name, len(metrics[name]))
	}
	return metrics[name][0]
}

func TestMissingAndExpiredMetrics(t *testing.T) {
	r, clock := receiverFor(t, ovenID)
	metrics := gather(t, r)
	if len(metrics) != 1 || onlyMetric(t, metrics, metricFresh).value != 0 {
		t.Fatal("missing device must export only freshness=0")
	}
	s := sample(t, ovenID)
	accept(t, r, s)
	metrics = gather(t, r)
	if onlyMetric(t, metrics, metricFresh).value != 1 || onlyMetric(t, metrics, metricTemperature).value != 239.7 {
		t.Fatal("fresh reading was not exported")
	}
	clock.advance(15 * time.Second)
	metrics = gather(t, r)
	if len(metrics) != 2 || onlyMetric(t, metrics, metricFresh).value != 0 ||
		onlyMetric(t, metrics, metricObserved).value != float64(s.ObservedAt.Unix()) {
		t.Fatal("expired device must retain only freshness and last observation time")
	}
	if len(metrics[metricHealthy]) != 0 || len(metrics[metricOperations]) != 0 || len(metrics[metricInfo]) != 0 {
		t.Fatal("expired status, counters, or revision appeared current")
	}
}

func TestSnapshotCountersAndState(t *testing.T) {
	r, clock := receiverFor(t, inspectorID)
	s := sample(t, inspectorID)
	s.Counters.OperationErrorsTotal = 3
	accept(t, r, s)
	for range 2 {
		metrics := gather(t, r)
		completed := onlyMetric(t, metrics, metricOperations)
		if !completed.counter || completed.value != 12 || onlyMetric(t, metrics, "factory_rejected_operations_total").value != 1 {
			t.Fatal("scrape did not preserve cumulative counters")
		}
		errors := onlyMetric(t, metrics, "factory_operation_errors_total")
		if !errors.counter || errors.value != 3 {
			t.Fatal("failed attempts were not exported as a separate counter")
		}
		if onlyMetric(t, metrics, metricHealthy).value != 1 {
			t.Fatal("rejected inspection incorrectly changed equipment health")
		}
	}
	if ok, err := r.Accept(s); ok || err != nil {
		t.Fatal("duplicate was not ignored")
	}
	if onlyMetric(t, gather(t, r), metricOperations).value != 12 {
		t.Fatal("duplicate delivery increased counter")
	}
	s.Sequence++
	s.State = telemetry.EquipmentState{}
	accept(t, r, s)
	metrics := gather(t, r)
	if onlyMetric(t, metrics, metricFresh).value != 1 || onlyMetric(t, metrics, metricHealthy).value != 0 ||
		onlyMetric(t, metrics, "factory_equipment_calibrated").value != 0 ||
		onlyMetric(t, metrics, "factory_equipment_setup_complete").value != 0 {
		t.Fatal("fresh fault state must be explicit zero, not missing")
	}
	s.SourceInstanceID = newInstance
	s.SourceStartedAt, s.ObservedAt = clock.now(), clock.now()
	s.Sequence = 1
	s.Readings = nil
	s.Counters = telemetry.Counters{}
	accept(t, r, s)
	if onlyMetric(t, gather(t, r), metricOperations).value != 0 {
		t.Fatal("source restart did not reset the exported counter")
	}
}

func TestRevisionReplacementAndOmittedReadings(t *testing.T) {
	r, clock := receiverFor(t, ovenID)
	s := sample(t, ovenID)
	accept(t, r, s)
	s.SourceInstanceID = newInstance
	s.SourceStartedAt, s.ObservedAt = clock.now(), clock.now()
	s.Revision = "new-revision"
	s.Readings = nil
	accept(t, r, s)
	metrics := gather(t, r)
	if len(metrics[metricTemperature]) != 0 || onlyMetric(t, metrics, metricInfo).labels["revision"] != s.Revision {
		t.Fatal("old reading or revision survived replacement")
	}
	for name, samples := range metrics {
		for _, m := range samples {
			wantLabels := 4
			if name == metricInfo {
				wantLabels = 7
			}
			if len(m.labels) != wantLabels || m.labels["namespace"] != s.Namespace ||
				m.labels["tech_process"] != s.TechProcess || m.labels["workstation"] != s.Workstation ||
				m.labels["equipment_id"] != s.EquipmentID {
				t.Fatalf("unexpected labels on %s: %v", name, m.labels)
			}
		}
	}
}

func TestAllCatalogGauges(t *testing.T) {
	ids := make([]string, 0, len(equipment.Catalog()))
	for _, device := range equipment.Catalog() {
		ids = append(ids, device.ID)
	}
	r, _ := receiverFor(t, ids...)
	if len(gather(t, r)[metricFresh]) != len(ids) {
		t.Fatal("missing expected devices are invisible")
	}
	for _, id := range ids {
		accept(t, r, sample(t, id))
	}
	metrics := gather(t, r)
	for _, id := range ids {
		s := sample(t, id)
		for name, value := range s.Readings {
			found := false
			for _, m := range metrics["factory_"+name] {
				if m.labels["equipment_id"] == id {
					found = true
					if m.counter || m.value != value {
						t.Fatalf("incorrect gauge %s for %s", name, id)
					}
				}
			}
			if !found {
				t.Fatalf("missing gauge %s for %s", name, id)
			}
		}
	}
}

func TestMetricsHTTPHandler(t *testing.T) {
	r, _ := receiverFor(t, ovenID)
	accept(t, r, sample(t, ovenID))
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", r.MetricsHandler())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics HTTP response = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{"# TYPE " + metricOperations + " counter", "# TYPE " + metricTemperature + " gauge", "239.7"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics response", expected)
		}
	}
	// Separate handlers never register collectors globally or contaminate each other.
	empty, _ := receiverFor(t, ovenID)
	response = httptest.NewRecorder()
	empty.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), metricTemperature) {
		t.Fatal("private registry leaked another receiver's observations")
	}
}
