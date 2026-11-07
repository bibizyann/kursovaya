package factorysource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/factory"
	"factory.local/factory-platform/internal/telemetry"
)

const metricPrefix = "factory_"

// Prometheus reads scoped instant vectors without accepting browser-supplied PromQL.
type Prometheus struct {
	URL          string
	Client       *http.Client
	Freshness    time.Duration
	MaxSampleAge time.Duration
	Now          func() time.Time
}

type vectorSample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

type metricValue struct {
	value     float64
	timestamp float64
	labels    map[string]string
}

// Read returns fresh telemetry only when the receiver and samples are current.
func (p Prometheus) Read(ctx context.Context, namespace, name string) (factory.TelemetrySnapshot, error) {
	result := factory.TelemetrySnapshot{Receiver: factory.Source{Reason: "ReceiverUnavailable"}, Equipment: map[string]factory.Observation{}}
	endpoint, err := prometheusURL(p.URL)
	if err != nil {
		return result, err
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	maxAge := p.MaxSampleAge
	if maxAge <= 0 {
		maxAge = 15 * time.Second
	}
	freshness := p.Freshness
	if freshness <= 0 {
		freshness = 20 * time.Second
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/query"
	query := endpoint.Query()
	query.Set("query", fmt.Sprintf(`{__name__=~"factory_.*",job="factory-telemetry",namespace=%s,tech_process=%s} or up{job="factory-telemetry"}`,
		strconv.Quote(namespace), strconv.Quote(name)))
	query.Set("time", strconv.FormatFloat(float64(now.UnixNano())/1e9, 'f', 3, 64))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return result, err
	}
	httpClient := p.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Second}
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("Prometheus returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string         `json:"resultType"`
			Result     []vectorSample `json:"result"`
		} `json:"data"`
		Warnings []string `json:"warnings"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&payload); err != nil {
		return result, err
	}
	if payload.Status != "success" || payload.Data.ResultType != "vector" || len(payload.Warnings) != 0 {
		return result, fmt.Errorf("incomplete Prometheus vector response")
	}
	devices := map[string]map[string]metricValue{}
	invalid := map[string]bool{}
	upCount := 0
	for _, sample := range payload.Data.Result {
		value, ok := parseSample(sample, now, maxAge)
		if !ok {
			continue
		}
		if sample.Metric["job"] != "factory-telemetry" {
			continue
		}
		metric := sample.Metric["__name__"]
		if metric == "up" {
			upCount++
			result.Receiver.Available = value.value == 1
			continue
		}
		if sample.Metric["namespace"] != namespace || sample.Metric["tech_process"] != name {
			continue
		}
		id := sample.Metric["equipment_id"]
		identity, lookupErr := equipment.Lookup(id)
		if lookupErr != nil || identity.Workstation != sample.Metric["workstation"] {
			continue
		}
		if devices[id] == nil {
			devices[id] = map[string]metricValue{}
		}
		if _, exists := devices[id][metric]; exists {
			invalid[id] = true
		}
		devices[id][metric] = value
	}
	result.Receiver.Available = result.Receiver.Available && upCount == 1
	if !result.Receiver.Available {
		return result, nil
	}
	result.Receiver.Reason = ""
	for _, device := range equipment.Catalog() {
		if invalid[device.ID] {
			result.Equipment[device.ID] = factory.Observation{State: factory.StateUnknown, Readings: []factory.Reading{}}
			continue
		}
		result.Equipment[device.ID] = observation(device.ID, devices[device.ID], now, freshness)
	}
	return result, nil
}

func parseSample(sample vectorSample, now time.Time, maxAge time.Duration) (metricValue, bool) {
	var value metricValue
	var encoded string
	if len(sample.Value) != 2 || json.Unmarshal(sample.Value[0], &value.timestamp) != nil || json.Unmarshal(sample.Value[1], &encoded) != nil {
		return value, false
	}
	parsed, err := strconv.ParseFloat(encoded, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return value, false
	}
	age := float64(now.UnixNano())/1e9 - value.timestamp
	if age < -5 || age > maxAge.Seconds() {
		return value, false
	}
	value.value, value.labels = parsed, sample.Metric
	return value, true
}

func observation(id string, values map[string]metricValue, now time.Time, freshness time.Duration) factory.Observation {
	result := factory.Observation{State: factory.StateMissing, Readings: []factory.Reading{}}
	timestamp, exists := values["factory_equipment_last_observation_timestamp_seconds"]
	if !exists {
		return result
	}
	age := float64(now.UnixNano())/1e9 - timestamp.value
	if age < -5 || timestamp.value < 0 || timestamp.value > 253402300799 {
		result.State = factory.StateUnknown
		return result
	}
	observedAt := time.Unix(int64(timestamp.value), int64((timestamp.value-math.Floor(timestamp.value))*1e9)).UTC()
	result.ObservedAt = &observedAt
	result.State = factory.StateStale
	fresh, exists := values["factory_equipment_telemetry_fresh"]
	if !exists || fresh.value != 1 || age > freshness.Seconds() {
		return result
	}
	// All required values must belong to the same scrape, not a previous lookback value.
	result.State = factory.StateUnknown
	for _, name := range []string{"equipment_telemetry_fresh", "equipment_healthy", "equipment_calibrated", "equipment_setup_complete", "equipment_info"} {
		value, ok := values[metricPrefix+name]
		if !ok || value.timestamp != timestamp.timestamp {
			return result
		}
	}
	healthy := booleanValue(values["factory_equipment_healthy"].value)
	calibrated := booleanValue(values["factory_equipment_calibrated"].value)
	setup := booleanValue(values["factory_equipment_setup_complete"].value)
	info := values["factory_equipment_info"]
	if healthy == nil || calibrated == nil || setup == nil || info.value != 1 ||
		info.labels["product"] == "" || info.labels["recipe_id"] == "" || info.labels["revision"] == "" {
		return result
	}
	result.State, result.Healthy, result.Calibrated, result.SetupComplete = factory.StateFresh, healthy, calibrated, setup
	result.Product, result.RecipeID, result.Revision = info.labels["product"], info.labels["recipe_id"], info.labels["revision"]
	definitions, _ := telemetry.ReadingDefinitions(id)
	for _, definition := range definitions {
		if value, ok := values[metricPrefix+definition.Name]; ok && value.timestamp == timestamp.timestamp {
			result.Readings = append(result.Readings, factory.Reading{Name: definition.Name, Value: value.value, Unit: definition.Unit})
		}
	}
	for metric, destination := range map[string]**float64{
		"factory_operations_total": &result.Operations, "factory_operation_errors_total": &result.Errors,
		"factory_rejected_operations_total": &result.Rejected,
	} {
		if value, ok := values[metric]; ok && value.timestamp == timestamp.timestamp && value.value >= 0 {
			*destination = &value.value
		}
	}
	return result
}

func booleanValue(value float64) *bool {
	if value != 0 && value != 1 {
		return nil
	}
	result := value == 1
	return &result
}

func prometheusURL(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil {
		return nil, fmt.Errorf("invalid Prometheus URL")
	}
	return endpoint, nil
}
