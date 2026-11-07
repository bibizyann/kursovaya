package controller

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/workload"
)

type processCollector struct {
	reader                                                                     client.Reader
	current, condition, desired, ready, revision, started, completed, duration *prometheus.Desc
}

// NewTechProcessCollector exports persisted process state without counting reconcile events.
// The manager's cached client avoids network calls during ordinary scrapes.
func NewTechProcessCollector(reader client.Reader) prometheus.Collector {
	labels := []string{"namespace", "tech_process"}
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("factory_techprocess_"+name, help, labels, nil)
	}
	return &processCollector{
		reader:  reader,
		current: desc("status_current", "Whether status observed the current process generation and template identity/generation."),
		condition: prometheus.NewDesc("factory_techprocess_condition", "One-hot current condition status; stale or absent conditions are unknown.",
			append(slices.Clone(labels), "condition", "status"), nil),
		desired: desc("desired_workloads", "Desired workload instances in the current reconciled revision."),
		ready:   desc("ready_workloads", "Confirmed ready workload instances of the current revision; mixed revisions are not counted."),
		revision: prometheus.NewDesc("factory_techprocess_revision_info", "Current reconciled revision.",
			append(slices.Clone(labels), "revision"), nil),
		started:   desc("rollout_started_timestamp_seconds", "Persisted start time of the current revision rollout."),
		completed: desc("rollout_completed_timestamp_seconds", "Persisted first readiness time of the current revision."),
		duration:  desc("rollout_duration_seconds", "Completed rollout duration from persisted timestamps; absent until first readiness."),
	}
}

func (c *processCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.current, c.condition, c.desired, c.ready, c.revision, c.started, c.completed, c.duration} {
		ch <- desc
	}
}

func (c *processCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	processes := &productionv1alpha1.TechProcessList{}
	templates := &productionv1alpha1.TechProcessTemplateList{}
	if err := c.reader.List(ctx, processes); err != nil {
		ch <- prometheus.NewInvalidMetric(c.current, err)
		return
	}
	if err := c.reader.List(ctx, templates); err != nil {
		ch <- prometheus.NewInvalidMetric(c.current, err)
		return
	}
	byName := make(map[client.ObjectKey]*productionv1alpha1.TechProcessTemplate, len(templates.Items))
	for index := range templates.Items {
		template := &templates.Items[index]
		byName[client.ObjectKeyFromObject(template)] = template
	}
	for index := range processes.Items {
		process := &processes.Items[index]
		if !process.DeletionTimestamp.IsZero() {
			continue
		}
		template := byName[client.ObjectKey{Namespace: process.Namespace, Name: process.Spec.TemplateRef.Name}]
		c.collectProcess(ch, process, template)
	}
}

func (c *processCollector) collectProcess(ch chan<- prometheus.Metric, process *productionv1alpha1.TechProcess, template *productionv1alpha1.TechProcessTemplate) {
	labels := []string{process.Namespace, process.Name}
	emit := func(desc *prometheus.Desc, value float64) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	}
	current := observedCurrentInputs(process, template)
	currentValue := 0.0
	if current {
		currentValue = 1
	}
	emit(c.current, currentValue)
	revisionMatches := false
	if template != nil && workload.Validate(process, template) == nil {
		revision, err := workload.Revision(process, template)
		revisionMatches = err == nil && process.Status.AppliedRevision == revision
	}
	for _, name := range []string{productionv1alpha1.ConditionReady, productionv1alpha1.ConditionProgressing, productionv1alpha1.ConditionDegraded} {
		status := metav1.ConditionUnknown
		condition := apiMeta.FindStatusCondition(process.Status.Conditions, name)
		if current && condition != nil && condition.ObservedGeneration == process.Generation {
			status = condition.Status
			if name == productionv1alpha1.ConditionReady && status == metav1.ConditionTrue && !revisionMatches {
				status = metav1.ConditionUnknown
			}
		}
		for _, candidate := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
			value := 0.0
			if candidate == status {
				value = 1
			}
			ch <- prometheus.MustNewConstMetric(c.condition, prometheus.GaugeValue, value, process.Namespace, process.Name, name, strings.ToLower(string(candidate)))
		}
	}
	if !current || !revisionMatches {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.revision, prometheus.GaugeValue, 1, process.Namespace, process.Name, process.Status.AppliedRevision)
	if len(process.Status.Components) > 0 {
		emit(c.desired, float64(process.Status.DesiredWorkloads))
		emit(c.ready, float64(process.Status.ReadyWorkloads))
	}
	if process.Status.StartedAt == nil {
		return
	}
	emit(c.started, float64(process.Status.StartedAt.Unix()))
	if process.Status.CompletedAt == nil || process.Status.CompletedAt.Before(process.Status.StartedAt) {
		return
	}
	emit(c.completed, float64(process.Status.CompletedAt.Unix()))
	emit(c.duration, process.Status.CompletedAt.Sub(process.Status.StartedAt.Time).Seconds())
}

func observedCurrentInputs(process *productionv1alpha1.TechProcess, template *productionv1alpha1.TechProcessTemplate) bool {
	if process.Status.ObservedGeneration == 0 || process.Status.ObservedGeneration != process.Generation {
		return false
	}
	if template == nil {
		return process.Status.ObservedTemplateUID == "" && process.Status.ObservedTemplateGeneration == 0
	}
	return process.Status.ObservedTemplateUID == template.UID && process.Status.ObservedTemplateGeneration == template.Generation
}
