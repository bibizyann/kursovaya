package workload

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
)

const (
	readinessProcess     = "line-one"
	readinessNamespace   = "factory"
	oldReadinessRevision = "old"
)

func TestDeploymentRevisionReadiness(t *testing.T) {
	process := &productionv1alpha1.TechProcess{ObjectMeta: metav1.ObjectMeta{Name: readinessProcess, Namespace: readinessNamespace}}
	base := Deployment(process, productionv1alpha1.ComponentTemplate{Name: "oven", Image: "simulator:test", Replicas: ptr.To(int32(2))}, "new-revision")
	base.Generation = 2
	base.Status = appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}
	for name, test := range map[string]struct {
		change   func(*appsv1.Deployment)
		ready    int32
		complete bool
	}{
		"complete":              {func(*appsv1.Deployment) {}, 2, true},
		"unobserved generation": {func(d *appsv1.Deployment) { d.Status.ObservedGeneration-- }, 0, false},
		"old ready replicas":    {func(d *appsv1.Deployment) { d.Status.UpdatedReplicas = 0 }, 0, false},
		"mixed revisions":       {func(d *appsv1.Deployment) { d.Status.UpdatedReplicas = 1 }, 0, false},
		"old pod template":      {func(d *appsv1.Deployment) { d.Spec.Template.Annotations[AnnotationRevision] = oldReadinessRevision }, 0, false},
		"old workload label":    {func(d *appsv1.Deployment) { d.Labels[LabelRevision] = oldReadinessRevision }, 0, false},
		"new replica starting":  {func(d *appsv1.Deployment) { d.Status.ReadyReplicas = 1; d.Status.AvailableReplicas = 1 }, 1, false},
		"availability delay":    {func(d *appsv1.Deployment) { d.Status.AvailableReplicas = 1 }, 2, false},
		"terminating replicas":  {func(d *appsv1.Deployment) { d.Status.TerminatingReplicas = ptr.To(int32(1)) }, 0, false},
		"deleting":              {func(d *appsv1.Deployment) { now := metav1.Now(); d.DeletionTimestamp = &now }, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			object := base.DeepCopy()
			test.change(object)
			desired, ready, complete := DeploymentReadiness(object, "new-revision")
			if desired != 2 || ready != test.ready || complete != test.complete {
				t.Fatalf("readiness = %d, %d, %t", desired, ready, complete)
			}
		})
	}
}

func TestDaemonSetRevisionReadiness(t *testing.T) {
	process := &productionv1alpha1.TechProcess{ObjectMeta: metav1.ObjectMeta{Name: readinessProcess, Namespace: readinessNamespace}}
	base := DaemonSet(process, productionv1alpha1.ComponentTemplate{Name: "oven", Image: "simulator:test"}, "revision")
	base.Generation = 2
	base.Status = appsv1.DaemonSetStatus{ObservedGeneration: 2, DesiredNumberScheduled: 2, CurrentNumberScheduled: 2, UpdatedNumberScheduled: 2, NumberReady: 2, NumberAvailable: 2}
	for name, test := range map[string]struct {
		change   func(*appsv1.DaemonSet)
		ready    int32
		complete bool
	}{
		"complete":              {func(*appsv1.DaemonSet) {}, 2, true},
		"unobserved generation": {func(d *appsv1.DaemonSet) { d.Status.ObservedGeneration-- }, 0, false},
		"old ready pods":        {func(d *appsv1.DaemonSet) { d.Status.UpdatedNumberScheduled = 0 }, 0, false},
		"mixed revisions":       {func(d *appsv1.DaemonSet) { d.Status.UpdatedNumberScheduled = 1 }, 0, false},
		"old pod labels":        {func(d *appsv1.DaemonSet) { d.Spec.Template.Labels[LabelRevision] = oldReadinessRevision }, 0, false},
		"unready pod":           {func(d *appsv1.DaemonSet) { d.Status.NumberReady = 1; d.Status.NumberAvailable = 1 }, 1, false},
		"misscheduled pod":      {func(d *appsv1.DaemonSet) { d.Status.NumberMisscheduled = 1 }, 0, false},
		"availability delay":    {func(d *appsv1.DaemonSet) { d.Status.NumberAvailable = 1 }, 2, false},
		"no compatible nodes":   {func(d *appsv1.DaemonSet) { d.Status = appsv1.DaemonSetStatus{ObservedGeneration: 2} }, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			object := base.DeepCopy()
			test.change(object)
			_, ready, complete := DaemonSetReadiness(object, "revision")
			if ready != test.ready || complete != test.complete {
				t.Fatalf("readiness = %d, %t", ready, complete)
			}
		})
	}
}
