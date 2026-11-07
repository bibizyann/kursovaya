// Package factorysource implements bounded, read-only infrastructure adapters for the overview.
package factorysource

import (
	"context"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	productionv1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/factory"
	"factory.local/factory-platform/internal/workload"
)

// Kubernetes reads a single configured process without mutation or cluster-wide lists.
type Kubernetes struct{ Reader client.Reader }

// Read collects desired configuration and verified child workload readiness.
func (k Kubernetes) Read(ctx context.Context, namespace, name string) (factory.ClusterSnapshot, error) {
	result := factory.ClusterSnapshot{Process: factory.Process{State: factory.StateUnknown}, Components: map[string]factory.Component{}}
	process := &productionv1.TechProcess{}
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := k.Reader.Get(ctx, key, process); err != nil {
		if apierrors.IsNotFound(err) {
			result.Process.Reason = "ProcessNotFound"
			return result, nil
		}
		return result, err
	}
	template := &productionv1.TechProcessTemplate{}
	templateKey := client.ObjectKey{Namespace: namespace, Name: process.Spec.TemplateRef.Name}
	if err := k.Reader.Get(ctx, templateKey, template); err != nil {
		if !apierrors.IsNotFound(err) {
			return result, err
		}
		template = nil
	}
	result.Process = processView(process, template)
	if template == nil {
		return result, nil
	}
	children, err := k.children(ctx, namespace, name)
	if err != nil {
		return result, err
	}
	allReady := len(template.Spec.Components) > 0
	var desired, ready int32
	for _, component := range template.Spec.Components {
		view := children.component(process, component, result.Process.Revision)
		result.Components[component.Name] = view
		allReady = allReady && view.State == factory.StateReady
		desired += view.Desired
		ready += view.Ready
	}
	if result.Process.StatusCurrent && result.Process.Revision != "" && result.Process.Revision == result.Process.ObservedRevision {
		result.Process.Desired, result.Process.Ready = &desired, &ready
	}
	if result.Process.State == factory.StateReady && !allReady {
		result.Process.State, result.Process.Reason = factory.StateProgressing, "WorkloadsNotReady"
	}
	// A second input read prevents a mixed-generation response during profile changes.
	latest := &productionv1.TechProcess{}
	if err := k.Reader.Get(ctx, key, latest); err != nil {
		return result, err
	}
	latestTemplate := &productionv1.TechProcessTemplate{}
	if err := k.Reader.Get(ctx, templateKey, latestTemplate); err != nil {
		return result, err
	}
	if latest.UID != process.UID || latest.Generation != process.Generation ||
		latestTemplate.UID != template.UID || latestTemplate.Generation != template.Generation {
		return result, fmt.Errorf("process inputs changed during overview read")
	}
	return result, nil
}

func processView(process *productionv1.TechProcess, template *productionv1.TechProcessTemplate) factory.Process {
	view := factory.Process{State: factory.StateUnknown, Reason: "StatusNotCurrent", Template: process.Spec.TemplateRef.Name,
		ObservedRevision: process.Status.AppliedRevision}
	current := process.Status.ObservedGeneration > 0 && process.Status.ObservedGeneration == process.Generation
	if template == nil {
		current = current && process.Status.ObservedTemplateGeneration == 0 && process.Status.ObservedTemplateUID == ""
	} else {
		current = current && process.Status.ObservedTemplateUID == template.UID &&
			process.Status.ObservedTemplateGeneration == template.Generation
		if workload.Validate(process, template) == nil {
			view.Revision, _ = workload.Revision(process, template)
		}
	}
	view.StatusCurrent = current
	if !process.DeletionTimestamp.IsZero() {
		view.Reason = "ProcessDeleting"
		return view
	}
	if !current {
		return view
	}
	for _, state := range []string{factory.StateDegraded, factory.StateReady, factory.StateProgressing} {
		condition := meta.FindStatusCondition(process.Status.Conditions, state)
		if condition != nil && condition.ObservedGeneration == process.Generation && condition.Status == metav1.ConditionTrue {
			view.State, view.Reason, view.Message = state, condition.Reason, condition.Message
			break
		}
	}
	if view.Revision == "" || view.Revision != process.Status.AppliedRevision {
		if view.State != factory.StateDegraded {
			view.State, view.Reason = factory.StateUnknown, "RevisionNotObserved"
		}
		return view
	}
	if len(process.Status.Components) > 0 {
		view.Desired, view.Ready = &process.Status.DesiredWorkloads, &process.Status.ReadyWorkloads
	}
	if process.Status.StartedAt != nil {
		view.StartedAt = &process.Status.StartedAt.Time
	}
	if process.Status.CompletedAt != nil {
		view.CompletedAt = &process.Status.CompletedAt.Time
	}
	return view
}

type childSnapshot struct {
	deployments appsv1.DeploymentList
	daemonsets  appsv1.DaemonSetList
	replicasets appsv1.ReplicaSetList
	pods        corev1.PodList
}

func (k Kubernetes) children(ctx context.Context, namespace, name string) (childSnapshot, error) {
	var children childSnapshot
	opts := []client.ListOption{client.InNamespace(namespace), client.MatchingLabels{workload.LabelProcess: workload.LabelValue(name)}}
	for _, list := range []client.ObjectList{&children.deployments, &children.daemonsets, &children.replicasets, &children.pods} {
		if err := k.Reader.List(ctx, list, opts...); err != nil {
			return children, err
		}
	}
	return children, nil
}

func (children childSnapshot) component(process *productionv1.TechProcess, component productionv1.ComponentTemplate, revision string) factory.Component {
	view := factory.Component{State: factory.StateProgressing, Image: component.Image, Pods: []factory.Pod{}}
	view.Product = productArgument(component.Args)
	if recipe, err := equipment.Profile(component.Name, view.Product); err == nil {
		view.Recipe = &recipe
	}
	name := workload.ResourceName(process.Name, component.Name)
	var owner types.UID
	var complete bool
	if component.Workload == productionv1.WorkloadKindDaemonSet {
		for _, ds := range children.daemonsets.Items {
			if ds.Name == name && metav1.IsControlledBy(&ds, process) {
				owner = ds.UID
				view.Desired, view.Ready, complete = workload.DaemonSetReadiness(&ds, revision)
			}
		}
	} else {
		view.Desired = 1
		if component.Replicas != nil {
			view.Desired = *component.Replicas
		}
		for _, dep := range children.deployments.Items {
			if dep.Name == name && metav1.IsControlledBy(&dep, process) {
				owner = dep.UID
				view.Desired, view.Ready, complete = workload.DeploymentReadiness(&dep, revision)
			}
		}
	}
	if complete {
		view.State = factory.StateReady
	}
	if revision == "" {
		view.State = factory.StateUnknown
	}
	if owner == "" {
		return view
	}
	owners := map[types.UID]bool{owner: true}
	for _, rs := range children.replicasets.Items {
		if ref := metav1.GetControllerOf(&rs); ref != nil && ref.UID == owner {
			owners[rs.UID] = true
		}
	}
	for _, pod := range children.pods.Items {
		ref := metav1.GetControllerOf(&pod)
		if ref == nil || !owners[ref.UID] || pod.Labels[workload.LabelComponent] != component.Name {
			continue
		}
		p := factory.Pod{Name: pod.Name, Node: pod.Spec.NodeName, Phase: string(pod.Status.Phase),
			Revision: pod.Labels[workload.LabelRevision], Terminating: !pod.DeletionTimestamp.IsZero()}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				p.Ready = condition.Status == corev1.ConditionTrue
			}
			if condition.Status == corev1.ConditionFalse && condition.Reason != "" {
				p.Reason = condition.Reason
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Waiting != nil {
				p.Reason = status.State.Waiting.Reason
			}
		}
		view.Pods = append(view.Pods, p)
	}
	slices.SortFunc(view.Pods, func(a, b factory.Pod) int { return strings.Compare(a.Name, b.Name) })
	return view
}

func productArgument(args []string) string {
	for i, arg := range args {
		if product, ok := strings.CutPrefix(arg, "--product="); ok {
			return product
		}
		if arg == "--product" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
