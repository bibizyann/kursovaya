/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/workload"
)

const (
	defaultRequeueInterval = 5 * time.Second
	missingTemplateRequeue = 30 * time.Second
	defaultRolloutTimeout  = 5 * time.Minute
)

// TechProcessReconciler reconciles a TechProcess object.
type TechProcessReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Now is injectable for deterministic timeout tests. It defaults to time.Now.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=production.factory.local,resources=techprocesses,verbs=get;list;watch
// +kubebuilder:rbac:groups=production.factory.local,resources=techprocesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=production.factory.local,resources=techprocesses/finalizers,verbs=update
// +kubebuilder:rbac:groups=production.factory.local,resources=techprocesstemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile converges one TechProcess to its referenced template and reports rollout readiness.
//
//nolint:gocyclo // Reconcile deliberately keeps the complete state transition visible in one level-based flow.
func (r *TechProcessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithValues("techProcess", req.NamespacedName)

	process := &productionv1alpha1.TechProcess{}
	if err := r.Get(ctx, req.NamespacedName, process); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	template := &productionv1alpha1.TechProcessTemplate{}
	templateKey := types.NamespacedName{Namespace: process.Namespace, Name: process.Spec.TemplateRef.Name}
	if err := r.Get(ctx, templateKey, template); err != nil {
		if apierrors.IsNotFound(err) {
			statusErr := r.setDegradedStatus(ctx, process, nil, "TemplateNotFound",
				fmt.Sprintf("TechProcessTemplate %q does not exist", templateKey.Name))
			return ctrl.Result{RequeueAfter: missingTemplateRequeue}, statusErr
		}
		return ctrl.Result{}, err
	}

	if err := workload.Validate(process, template); err != nil {
		statusErr := r.setDegradedStatus(ctx, process, template, "InvalidDesiredState", err.Error())
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	revision, err := workload.Revision(process, template)
	if err != nil {
		statusErr := r.setDegradedStatus(ctx, process, template, "RevisionCalculationFailed", err.Error())
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	if process.Status.AppliedRevision != revision || process.Status.StartedAt == nil {
		if err := r.setStatus(ctx, process, template, statusUpdate{
			revision: revision, reason: "ApplyingRevision", message: "Applying the desired revision to workloads",
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.applyParameters(ctx, process, revision); err != nil {
		statusErr := r.setDegradedStatus(ctx, process, template, "ParametersApplyFailed", err.Error())
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	desiredDeployments := make(map[string]struct{})
	desiredDaemonSets := make(map[string]struct{})
	for _, component := range template.Spec.Components {
		kind := component.Workload
		if kind == "" {
			kind = productionv1alpha1.WorkloadKindDeployment
		}

		switch kind {
		case productionv1alpha1.WorkloadKindDeployment:
			desired := workload.Deployment(process, component, revision)
			if err := r.deleteDaemonSetIfPresent(ctx, process, desired.Name); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.applyDeployment(ctx, process, desired); err != nil {
				statusErr := r.setDegradedStatus(ctx, process, template, "WorkloadApplyFailed", err.Error())
				return ctrl.Result{}, errors.Join(err, statusErr)
			}
			desiredDeployments[desired.Name] = struct{}{}
		case productionv1alpha1.WorkloadKindDaemonSet:
			desired := workload.DaemonSet(process, component, revision)
			if err := r.deleteDeploymentIfPresent(ctx, process, desired.Name); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.applyDaemonSet(ctx, process, desired); err != nil {
				statusErr := r.setDegradedStatus(ctx, process, template, "WorkloadApplyFailed", err.Error())
				return ctrl.Result{}, errors.Join(err, statusErr)
			}
			desiredDaemonSets[desired.Name] = struct{}{}
		}
	}

	if err := r.pruneStaleWorkloads(ctx, process, desiredDeployments, desiredDaemonSets); err != nil {
		statusErr := r.setDegradedStatus(ctx, process, template, "WorkloadPruneFailed", err.Error())
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	components, desiredCount, readyCount, ready, err := r.collectStatus(ctx, process, template, revision)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !ready && r.rolloutTimedOut(process, revision) {
		message := fmt.Sprintf("revision %s did not become ready before timeout", revision)
		if err := r.setStatus(ctx, process, template, statusUpdate{
			revision:         revision,
			components:       components,
			desiredWorkloads: desiredCount,
			readyWorkloads:   readyCount,
			degraded:         true,
			reason:           "RolloutTimedOut",
			message:          message,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, nil
	}

	reason := "WorkloadsProgressing"
	message := fmt.Sprintf("%d of %d workload instances are ready", readyCount, desiredCount)
	if ready {
		reason = "RevisionReady"
		message = fmt.Sprintf("revision %s is ready", revision)
	}
	if err := r.setStatus(ctx, process, template, statusUpdate{
		revision:         revision,
		components:       components,
		desiredWorkloads: desiredCount,
		readyWorkloads:   readyCount,
		ready:            ready,
		reason:           reason,
		message:          message,
	}); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciled process revision", "revision", revision, "ready", ready,
		"readyWorkloads", readyCount, "desiredWorkloads", desiredCount)
	if !ready {
		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, nil
	}
	return ctrl.Result{}, nil
}

func (r *TechProcessReconciler) applyParameters(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	revision string,
) error {
	desired := workload.ParametersConfigMap(process, revision)
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.Labels = desired.Labels
		current.Immutable = desired.Immutable
		current.Data = desired.Data
		return controllerutil.SetControllerReference(process, current, r.Scheme)
	})
	return err
}

func (r *TechProcessReconciler) applyDeployment(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	desired *appsv1.Deployment,
) error {
	current := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.Labels = desired.Labels
		current.Spec = desired.Spec
		return controllerutil.SetControllerReference(process, current, r.Scheme)
	})
	return err
}

func (r *TechProcessReconciler) applyDaemonSet(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	desired *appsv1.DaemonSet,
) error {
	current := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.Labels = desired.Labels
		current.Spec = desired.Spec
		return controllerutil.SetControllerReference(process, current, r.Scheme)
	})
	return err
}

func (r *TechProcessReconciler) deleteDeploymentIfPresent(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	name string,
) error {
	object := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: process.Namespace, Name: name}, object); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(object, process) {
		return fmt.Errorf("deployment %s/%s exists but is not controlled by TechProcess", object.Namespace, object.Name)
	}
	return r.Delete(ctx, object)
}

func (r *TechProcessReconciler) deleteDaemonSetIfPresent(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	name string,
) error {
	object := &appsv1.DaemonSet{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: process.Namespace, Name: name}, object); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(object, process) {
		return fmt.Errorf("daemonSet %s/%s exists but is not controlled by TechProcess", object.Namespace, object.Name)
	}
	return r.Delete(ctx, object)
}

func (r *TechProcessReconciler) pruneStaleWorkloads(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	desiredDeployments map[string]struct{},
	desiredDaemonSets map[string]struct{},
) error {
	matchingLabels := client.MatchingLabels{
		workload.LabelManagedBy: "factory-platform",
		workload.LabelProcess:   workload.LabelValue(process.Name),
	}

	deployments := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployments, client.InNamespace(process.Namespace), matchingLabels); err != nil {
		return err
	}
	for index := range deployments.Items {
		object := &deployments.Items[index]
		if _, desired := desiredDeployments[object.Name]; desired || !metav1.IsControlledBy(object, process) {
			continue
		}
		if err := r.Delete(ctx, object); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	daemonSets := &appsv1.DaemonSetList{}
	if err := r.List(ctx, daemonSets, client.InNamespace(process.Namespace), matchingLabels); err != nil {
		return err
	}
	for index := range daemonSets.Items {
		object := &daemonSets.Items[index]
		if _, desired := desiredDaemonSets[object.Name]; desired || !metav1.IsControlledBy(object, process) {
			continue
		}
		if err := r.Delete(ctx, object); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func (r *TechProcessReconciler) collectStatus(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	template *productionv1alpha1.TechProcessTemplate,
	revision string,
) ([]productionv1alpha1.ComponentStatus, int32, int32, bool, error) {
	components := make([]productionv1alpha1.ComponentStatus, 0, len(template.Spec.Components))
	var totalDesired int32
	var totalReady int32
	allReady := true

	for _, component := range template.Spec.Components {
		kind := component.Workload
		if kind == "" {
			kind = productionv1alpha1.WorkloadKindDeployment
		}
		status := productionv1alpha1.ComponentStatus{Name: component.Name, Kind: kind}
		key := types.NamespacedName{Namespace: process.Namespace, Name: workload.ResourceName(process.Name, component.Name)}

		switch kind {
		case productionv1alpha1.WorkloadKindDeployment:
			object := &appsv1.Deployment{}
			if err := r.Get(ctx, key, object); err != nil {
				return nil, 0, 0, false, err
			}
			var complete bool
			status.Desired, status.Ready, complete = workload.DeploymentReadiness(object, revision)
			allReady = allReady && complete && metav1.IsControlledBy(object, process)
		case productionv1alpha1.WorkloadKindDaemonSet:
			object := &appsv1.DaemonSet{}
			if err := r.Get(ctx, key, object); err != nil {
				return nil, 0, 0, false, err
			}
			var complete bool
			status.Desired, status.Ready, complete = workload.DaemonSetReadiness(object, revision)
			allReady = allReady && complete && metav1.IsControlledBy(object, process)
		}

		totalDesired += status.Desired
		totalReady += status.Ready
		components = append(components, status)
	}
	return components, totalDesired, totalReady, allReady, nil
}

func (r *TechProcessReconciler) rolloutTimedOut(process *productionv1alpha1.TechProcess, revision string) bool {
	if process.Status.AppliedRevision != revision || process.Status.StartedAt == nil || process.Status.CompletedAt != nil {
		return false
	}
	timeout := process.Spec.Timeout.Duration
	if timeout <= 0 {
		timeout = defaultRolloutTimeout
	}
	return r.now().Sub(process.Status.StartedAt.Time) >= timeout
}

type statusUpdate struct {
	revision         string
	components       []productionv1alpha1.ComponentStatus
	desiredWorkloads int32
	readyWorkloads   int32
	ready            bool
	degraded         bool
	reason           string
	message          string
}

func (r *TechProcessReconciler) setDegradedStatus(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	template *productionv1alpha1.TechProcessTemplate,
	reason string,
	message string,
) error {
	return r.setStatus(ctx, process, template, statusUpdate{
		revision: process.Status.AppliedRevision,
		degraded: true,
		reason:   reason,
		message:  message,
	})
}

func (r *TechProcessReconciler) setStatus(
	ctx context.Context,
	process *productionv1alpha1.TechProcess,
	template *productionv1alpha1.TechProcessTemplate,
	update statusUpdate,
) error {
	current := &productionv1alpha1.TechProcess{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(process), current); err != nil {
		return err
	}
	if current.UID != process.UID || current.Generation != process.Generation {
		return statusConflict(process, "process changed during reconciliation")
	}
	latestTemplate := &productionv1alpha1.TechProcessTemplate{}
	templateErr := r.Get(ctx, types.NamespacedName{Namespace: process.Namespace, Name: process.Spec.TemplateRef.Name}, latestTemplate)
	if templateErr != nil && !apierrors.IsNotFound(templateErr) {
		return templateErr
	}
	if (template == nil && templateErr == nil) || (template != nil && (templateErr != nil ||
		latestTemplate.UID != template.UID || latestTemplate.Generation != template.Generation)) {
		return statusConflict(process, "template changed during reconciliation")
	}
	before := current.DeepCopy()
	now := metav1.NewTime(r.now())

	if update.revision != "" && (current.Status.AppliedRevision != update.revision || current.Status.StartedAt == nil) {
		current.Status.AppliedRevision = update.revision
		current.Status.StartedAt = &now
		current.Status.CompletedAt = nil
	}
	current.Status.ObservedTemplateGeneration = 0
	current.Status.ObservedTemplateUID = ""
	if template != nil {
		current.Status.ObservedTemplateGeneration = template.Generation
		current.Status.ObservedTemplateUID = template.UID
	}
	current.Status.ObservedGeneration = current.Generation
	current.Status.Components = update.components
	current.Status.DesiredWorkloads = update.desiredWorkloads
	current.Status.ReadyWorkloads = update.readyWorkloads

	switch {
	case update.degraded:
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionReady, metav1.ConditionFalse,
			update.reason, update.message, current.Generation)
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionProgressing, metav1.ConditionFalse,
			update.reason, update.message, current.Generation)
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionDegraded, metav1.ConditionTrue,
			update.reason, update.message, current.Generation)
	case update.ready:
		if current.Status.CompletedAt == nil {
			current.Status.CompletedAt = &now
		}
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionReady, metav1.ConditionTrue,
			update.reason, update.message, current.Generation)
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionProgressing, metav1.ConditionFalse,
			update.reason, update.message, current.Generation)
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionDegraded, metav1.ConditionFalse,
			update.reason, update.message, current.Generation)
	default:
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionReady, metav1.ConditionFalse,
			update.reason, update.message, current.Generation)
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionProgressing, metav1.ConditionTrue,
			update.reason, update.message, current.Generation)
		setCondition(&current.Status.Conditions, productionv1alpha1.ConditionDegraded, metav1.ConditionFalse,
			update.reason, update.message, current.Generation)
	}

	if reflect.DeepEqual(before.Status, current.Status) {
		process.Status = *current.Status.DeepCopy()
		return nil
	}
	if err := r.Status().Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	process.Status = *current.Status.DeepCopy()
	return nil
}

func statusConflict(process *productionv1alpha1.TechProcess, message string) error {
	return apierrors.NewConflict(productionv1alpha1.SchemeGroupVersion.WithResource("techprocesses").GroupResource(), process.Name, errors.New(message))
}

func setCondition(
	conditions *[]metav1.Condition,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
	observedGeneration int64,
) {
	apiMeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: observedGeneration,
		Reason:             reason,
		Message:            message,
	})
}

func (r *TechProcessReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *TechProcessReconciler) requestsForTemplate(
	ctx context.Context,
	object client.Object,
) []reconcile.Request {
	processes := &productionv1alpha1.TechProcessList{}
	if err := r.List(ctx, processes, client.InNamespace(object.GetNamespace())); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for index := range processes.Items {
		process := &processes.Items[index]
		if process.Spec.TemplateRef.Name != object.GetName() {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(process)})
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *TechProcessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&productionv1alpha1.TechProcess{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&productionv1alpha1.TechProcessTemplate{}, handler.EnqueueRequestsFromMapFunc(r.requestsForTemplate)).
		Named("techprocess").
		Complete(r)
}
