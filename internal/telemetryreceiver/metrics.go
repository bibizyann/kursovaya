package telemetryreceiver

import (
	"net/http"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"factory.local/factory-platform/internal/telemetry"
)

type collector struct {
	receiver                                          *Receiver
	fresh, observed, healthy, calibrated, setup, info *prometheus.Desc
	operations, errors, rejected                      *prometheus.Desc
	readings                                          map[string]*prometheus.Desc
	readingNames                                      []string
}

// Collector creates an exporter backed by current receiver state, without global registration.
func (r *Receiver) Collector() prometheus.Collector {
	labels := []string{"namespace", "tech_process", "workstation", "equipment_id"}
	describe := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("factory_"+name, help, labels, nil)
	}
	c := &collector{
		receiver:   r,
		fresh:      describe("equipment_telemetry_fresh", "Whether a fresh observation is available for the expected device."),
		observed:   describe("equipment_last_observation_timestamp_seconds", "Unix time of the last accepted source observation."),
		healthy:    describe("equipment_healthy", "Whether the equipment reports healthy in a fresh snapshot."),
		calibrated: describe("equipment_calibrated", "Whether the simulated calibration check passed in a fresh snapshot."),
		setup:      describe("equipment_setup_complete", "Whether preparation is complete in a fresh snapshot."),
		info: prometheus.NewDesc("factory_equipment_info", "Applied configuration reported by a fresh source.",
			append(slices.Clone(labels), "product", "recipe_id", "revision"), nil),
		operations: describe("operations_total", "Completed operations in the current source lifetime, including rejected inspections."),
		errors:     describe("operation_errors_total", "Failed operation attempts in the current source lifetime."),
		rejected:   describe("rejected_operations_total", "Completed inspections rejecting a product in the current source lifetime."),
		readings:   make(map[string]*prometheus.Desc),
	}
	for _, key := range r.order {
		// New already validated every inventory entry against the same immutable catalog.
		definitions, _ := telemetry.ReadingDefinitions(key.EquipmentID)
		for _, definition := range definitions {
			if _, exists := c.readings[definition.Name]; !exists {
				c.readings[definition.Name] = describe(definition.Name, "Latest available "+definition.Name+" reading ("+definition.Unit+").")
				c.readingNames = append(c.readingNames, definition.Name)
			}
		}
	}
	slices.Sort(c.readingNames)
	return c
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, descriptor := range []*prometheus.Desc{
		c.fresh, c.observed, c.healthy, c.calibrated, c.setup, c.info, c.operations, c.errors, c.rejected,
	} {
		ch <- descriptor
	}
	for _, name := range c.readingNames {
		ch <- c.readings[name]
	}
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	// Copy once, then release receiver locks before writing to the scrape channel.
	for _, observation := range c.receiver.Observations() {
		labels := []string{observation.Key.Namespace, observation.Key.TechProcess, observation.Workstation, observation.Key.EquipmentID}
		ch <- prometheus.MustNewConstMetric(c.fresh, prometheus.GaugeValue, boolean(observation.Fresh), labels...)
		if observation.Snapshot == nil {
			continue
		}
		snapshot := observation.Snapshot
		ch <- prometheus.MustNewConstMetric(c.observed, prometheus.GaugeValue, float64(snapshot.ObservedAt.Unix())+float64(snapshot.ObservedAt.Nanosecond())/1e9, labels...)
		if !observation.Fresh {
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.healthy, prometheus.GaugeValue, boolean(snapshot.State.Healthy), labels...)
		ch <- prometheus.MustNewConstMetric(c.calibrated, prometheus.GaugeValue, boolean(snapshot.State.Calibrated), labels...)
		ch <- prometheus.MustNewConstMetric(c.setup, prometheus.GaugeValue, boolean(snapshot.State.SetupComplete), labels...)
		ch <- prometheus.MustNewConstMetric(c.info, prometheus.GaugeValue, 1,
			append(slices.Clone(labels), snapshot.Product, snapshot.RecipeID, snapshot.Revision)...)
		ch <- prometheus.MustNewConstMetric(c.operations, prometheus.CounterValue, float64(snapshot.Counters.OperationsTotal), labels...)
		ch <- prometheus.MustNewConstMetric(c.errors, prometheus.CounterValue, float64(snapshot.Counters.OperationErrorsTotal), labels...)
		ch <- prometheus.MustNewConstMetric(c.rejected, prometheus.CounterValue, float64(snapshot.Counters.RejectedOperationsTotal), labels...)
		for _, name := range c.readingNames {
			if value, available := snapshot.Readings[name]; available {
				ch <- prometheus.MustNewConstMetric(c.readings[name], prometheus.GaugeValue, value, labels...)
			}
		}
	}
}

func boolean(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

// MetricsHandler serves receiver metrics using a private registry; mount it at /metrics.
func (r *Receiver) MetricsHandler() http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(r.Collector())
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
