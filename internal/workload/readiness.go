package workload

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeploymentReadiness counts confirmed ready instances of the requested revision.
// Mixed old/new replica sets report zero confirmed ready instances until old replicas are gone.
func DeploymentReadiness(object *appsv1.Deployment, revision string) (desired, ready int32, complete bool) {
	desired = 1
	if object.Spec.Replicas != nil {
		desired = *object.Spec.Replicas
	}
	if !revisionObserved(object.ObjectMeta, object.Spec.Template.ObjectMeta, revision, object.Status.ObservedGeneration) {
		return desired, 0, false
	}
	status := object.Status
	if status.Replicas != status.UpdatedReplicas || (status.TerminatingReplicas != nil && *status.TerminatingReplicas > 0) {
		return desired, 0, false
	}
	ready = max(0, min(status.ReadyReplicas, status.UpdatedReplicas, desired))
	complete = desired > 0 && status.UpdatedReplicas == desired && ready == desired &&
		status.AvailableReplicas == desired && status.UnavailableReplicas == 0
	return desired, ready, complete
}

// DaemonSetReadiness counts confirmed ready instances on nodes running the requested revision.
func DaemonSetReadiness(object *appsv1.DaemonSet, revision string) (desired, ready int32, complete bool) {
	desired = object.Status.DesiredNumberScheduled
	if !revisionObserved(object.ObjectMeta, object.Spec.Template.ObjectMeta, revision, object.Status.ObservedGeneration) {
		return desired, 0, false
	}
	status := object.Status
	if status.CurrentNumberScheduled != status.UpdatedNumberScheduled || status.NumberMisscheduled != 0 {
		return desired, 0, false
	}
	ready = max(0, min(status.NumberReady, status.UpdatedNumberScheduled, desired))
	complete = desired > 0 && status.UpdatedNumberScheduled == desired && ready == desired &&
		status.NumberAvailable == desired && status.NumberUnavailable == 0
	return desired, ready, complete
}

func revisionObserved(object, podTemplate metav1.ObjectMeta, revision string, observedGeneration int64) bool {
	return revision != "" && object.DeletionTimestamp.IsZero() && observedGeneration >= object.Generation &&
		object.Labels[LabelRevision] == revision && podTemplate.Labels[LabelRevision] == revision &&
		podTemplate.Annotations[AnnotationRevision] == revision
}
