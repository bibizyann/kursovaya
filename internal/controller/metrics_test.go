package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/workload"
)

const (
	processConditionMetric = "factory_techprocess_condition"
	processCurrentMetric   = "factory_techprocess_status_current"
	processDurationMetric  = "factory_techprocess_rollout_duration_seconds"
	processReadyMetric     = "factory_techprocess_ready_workloads"
)

func metricObjects(t *testing.T) (client.Client, *productionv1alpha1.TechProcess, *productionv1alpha1.TechProcessTemplate) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := productionv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	template := &productionv1alpha1.TechProcessTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "board-a", Namespace: "factory", UID: "template-uid", Generation: 1},
		Spec:       productionv1alpha1.TechProcessTemplateSpec{Revision: "a", Components: []productionv1alpha1.ComponentTemplate{{Name: "oven", Image: "simulator:test"}}},
	}
	process := &productionv1alpha1.TechProcess{
		ObjectMeta: metav1.ObjectMeta{Name: "line-one", Namespace: "factory", UID: "process-uid", Generation: 1},
		Spec:       productionv1alpha1.TechProcessSpec{TemplateRef: corev1.LocalObjectReference{Name: template.Name}},
	}
	revision, err := workload.Revision(process, template)
	if err != nil {
		t.Fatal(err)
	}
	start := metav1.NewTime(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	completed := metav1.NewTime(start.Add(30 * time.Second))
	process.Status = productionv1alpha1.TechProcessStatus{
		ObservedGeneration: 1, ObservedTemplateGeneration: 1, ObservedTemplateUID: template.UID,
		AppliedRevision: revision, StartedAt: &start, CompletedAt: &completed, DesiredWorkloads: 1, ReadyWorkloads: 1,
		Components: []productionv1alpha1.ComponentStatus{{Name: "oven", Kind: productionv1alpha1.WorkloadKindDeployment, Desired: 1, Ready: 1}},
		Conditions: []metav1.Condition{{Type: productionv1alpha1.ConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: 1}},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(process).WithObjects(process, template).Build()
	return reader, process, template
}

type processMetricSample struct {
	value  float64
	labels map[string]string
}

func gatherProcesses(t *testing.T, reader client.Reader) map[string][]processMetricSample {
	t.Helper()
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(NewTechProcessCollector(reader))
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string][]processMetricSample)
	for _, family := range families {
		for _, metric := range family.Metric {
			if metric.Gauge == nil {
				t.Fatal("process metric must be a gauge")
			}
			sample := processMetricSample{value: metric.Gauge.GetValue(), labels: make(map[string]string)}
			for _, label := range metric.Label {
				sample.labels[label.GetName()] = label.GetValue()
			}
			result[family.GetName()] = append(result[family.GetName()], sample)
		}
	}
	return result
}

func conditionValue(metrics map[string][]processMetricSample, condition, status string) float64 {
	for _, sample := range metrics[processConditionMetric] {
		if sample.labels["condition"] == condition && sample.labels["status"] == status {
			return sample.value
		}
	}
	return -1
}

func TestProcessMetricsPersistAcrossScrapesAndCollectorRestart(t *testing.T) {
	reader, process, _ := metricObjects(t)
	for range 3 {
		metrics := gatherProcesses(t, reader)
		if len(metrics[processDurationMetric]) != 1 || metrics[processDurationMetric][0].value != 30 {
			t.Fatal("duration changed or disappeared")
		}
		if metrics[processCurrentMetric][0].value != 1 || metrics[processReadyMetric][0].value != 1 || conditionValue(metrics, "Ready", "true") != 1 {
			t.Fatal("current ready state lost")
		}
		if conditionValue(metrics, "Degraded", "unknown") != 1 {
			t.Fatal("absent condition was represented as false")
		}
		for name, samples := range metrics {
			for _, sample := range samples {
				want := 2
				if name == processConditionMetric {
					want = 4
				}
				if name == "factory_techprocess_revision_info" {
					want = 3
				}
				if len(sample.labels) != want || sample.labels["namespace"] != process.Namespace || sample.labels["tech_process"] != process.Name {
					t.Fatal("unexpected metric labels")
				}
			}
		}
	}
	if err := reader.Delete(context.Background(), process); err != nil {
		t.Fatal(err)
	}
	if len(gatherProcesses(t, reader)) != 0 {
		t.Fatal("deleted process left stale series")
	}
}

func TestProcessMetricsInvalidateChangedInputs(t *testing.T) {
	for _, change := range []string{"process", "template generation", "template UID", "missing template"} {
		t.Run(change, func(t *testing.T) {
			reader, process, template := metricObjects(t)
			switch change {
			case "process":
				process.Generation++
				if err := reader.Update(context.Background(), process); err != nil {
					t.Fatal(err)
				}
			case "template generation":
				template.Generation++
				if err := reader.Update(context.Background(), template); err != nil {
					t.Fatal(err)
				}
			case "template UID":
				template.UID = "replacement"
				if err := reader.Update(context.Background(), template); err != nil {
					t.Fatal(err)
				}
			case "missing template":
				if err := reader.Delete(context.Background(), template); err != nil {
					t.Fatal(err)
				}
			}
			metrics := gatherProcesses(t, reader)
			if metrics[processCurrentMetric][0].value != 0 || conditionValue(metrics, "Ready", "unknown") != 1 || len(metrics[processDurationMetric]) != 0 || len(metrics[processReadyMetric]) != 0 {
				t.Fatal("stale state exported as current")
			}
		})
	}
}

func TestProcessMetricsReportMissingTemplateDegradation(t *testing.T) {
	reader, process, template := metricObjects(t)
	if err := reader.Delete(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	process.Status.ObservedTemplateUID, process.Status.ObservedTemplateGeneration = "", 0
	process.Status.Conditions = []metav1.Condition{{Type: productionv1alpha1.ConditionDegraded, Status: metav1.ConditionTrue, ObservedGeneration: 1}}
	if err := reader.Status().Update(context.Background(), process); err != nil {
		t.Fatal(err)
	}
	metrics := gatherProcesses(t, reader)
	if metrics[processCurrentMetric][0].value != 1 || conditionValue(metrics, "Degraded", "true") != 1 || len(metrics[processDurationMetric]) != 0 {
		t.Fatal("current missing-template condition not exported")
	}
}

const staleConditionCase = "stale condition"

func TestProcessMetricsOmitIncompleteOrInvalidDuration(t *testing.T) {
	for _, state := range []string{"no completion", "clock regression", "wrong revision", staleConditionCase} {
		t.Run(state, func(t *testing.T) {
			reader, process, _ := metricObjects(t)
			switch state {
			case "no completion":
				process.Status.CompletedAt = nil
			case "clock regression":
				earlier := metav1.NewTime(process.Status.StartedAt.Add(-time.Second))
				process.Status.CompletedAt = &earlier
			case "wrong revision":
				process.Status.AppliedRevision = "old"
			case staleConditionCase:
				process.Status.Conditions[0].ObservedGeneration = 0
			}
			if err := reader.Status().Update(context.Background(), process); err != nil {
				t.Fatal(err)
			}
			metrics := gatherProcesses(t, reader)
			if state == staleConditionCase {
				if conditionValue(metrics, "Ready", "unknown") != 1 {
					t.Fatal("stale condition treated as ready")
				}
				return
			}
			if len(metrics[processDurationMetric]) != 0 {
				t.Fatal("invented completed duration")
			}
		})
	}
}

type failingMetricReader struct{ client.Reader }

func (f failingMetricReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("cache unavailable")
}

func TestProcessMetricsExposeReadFailure(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(NewTechProcessCollector(failingMetricReader{}))
	if _, err := registry.Gather(); err == nil {
		t.Fatal("read failure silently emitted empty metrics")
	}
}
