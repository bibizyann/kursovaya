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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/workload"
)

var _ = Describe("TechProcess Controller", func() {
	const (
		resourceName      = "board-a-line-one"
		templateName      = "board-a"
		resourceNamespace = "default"
	)

	ctx := context.Background()
	processKey := types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}
	templateKey := types.NamespacedName{Name: templateName, Namespace: resourceNamespace}

	BeforeEach(func() {
		template := &productionv1alpha1.TechProcessTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: templateName, Namespace: resourceNamespace},
			Spec: productionv1alpha1.TechProcessTemplateSpec{
				Revision: "1.0.0",
				Components: []productionv1alpha1.ComponentTemplate{{
					Name:  "vision",
					Image: "equipment-simulator:test",
					RequiredCapabilities: metav1.LabelSelector{MatchLabels: map[string]string{
						"factory.local/camera": "true",
					}},
					ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(8080)},
					}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, template)).To(Succeed())

		process := &productionv1alpha1.TechProcess{
			ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: resourceNamespace},
			Spec: productionv1alpha1.TechProcessSpec{
				TemplateRef: corev1.LocalObjectReference{Name: templateName},
				TargetSelector: metav1.LabelSelector{MatchLabels: map[string]string{
					"factory.local/line": "line-one",
				}},
				Parameters: map[string]string{"product": "board-a"},
			},
		}
		Expect(k8sClient.Create(ctx, process)).To(Succeed())
	})

	AfterEach(func() {
		process := &productionv1alpha1.TechProcess{}
		if err := k8sClient.Get(ctx, processKey, process); err == nil {
			Expect(k8sClient.Delete(ctx, process)).To(Succeed())
		}
		template := &productionv1alpha1.TechProcessTemplate{}
		if err := k8sClient.Get(ctx, templateKey, template); err == nil {
			Expect(k8sClient.Delete(ctx, template)).To(Succeed())
		}

		deployments := &appsv1.DeploymentList{}
		Expect(k8sClient.List(ctx, deployments, client.InNamespace(resourceNamespace))).To(Succeed())
		for index := range deployments.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &deployments.Items[index]))).To(Succeed())
		}
		configMaps := &corev1.ConfigMapList{}
		Expect(k8sClient.List(ctx, configMaps, client.InNamespace(resourceNamespace))).To(Succeed())
		for index := range configMaps.Items {
			if configMaps.Items[index].Labels[workload.LabelManagedBy] == "factory-platform" {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &configMaps.Items[index]))).To(Succeed())
			}
		}
	})

	It("creates workloads and reports a ready revision", func() {
		fixedNow := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
		reconciler := &TechProcessReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Now:    func() time.Time { return fixedNow },
		}

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: processKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(defaultRequeueInterval))

		deployment := &appsv1.Deployment{}
		deploymentKey := types.NamespacedName{
			Namespace: resourceNamespace,
			Name:      workload.ResourceName(resourceName, "vision"),
		}
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		Expect(deployment.Spec.Template.Spec.Affinity).NotTo(BeNil())

		parameters := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: resourceNamespace,
			Name:      deployment.Spec.Template.Spec.Volumes[0].ConfigMap.Name,
		}, parameters)).To(Succeed())
		Expect(parameters.Data).To(HaveKeyWithValue("product", "board-a"))

		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: processKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		process := &productionv1alpha1.TechProcess{}
		Expect(k8sClient.Get(ctx, processKey, process)).To(Succeed())
		ready := apiMeta.FindStatusCondition(process.Status.Conditions, productionv1alpha1.ConditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(process.Status.ReadyWorkloads).To(Equal(int32(1)))
		Expect(process.Status.AppliedRevision).NotTo(BeEmpty())
		Expect(process.Status.CompletedAt).NotTo(BeNil())
	})

	It("reports a missing template without creating workloads", func() {
		template := &productionv1alpha1.TechProcessTemplate{}
		Expect(k8sClient.Get(ctx, templateKey, template)).To(Succeed())
		Expect(k8sClient.Delete(ctx, template)).To(Succeed())

		reconciler := &TechProcessReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: processKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(missingTemplateRequeue))

		process := &productionv1alpha1.TechProcess{}
		Expect(k8sClient.Get(ctx, processKey, process)).To(Succeed())
		degraded := apiMeta.FindStatusCondition(process.Status.Conditions, productionv1alpha1.ConditionDegraded)
		Expect(degraded).NotTo(BeNil())
		Expect(degraded.Status).To(Equal(metav1.ConditionTrue))
		Expect(degraded.Reason).To(Equal("TemplateNotFound"))

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Namespace: resourceNamespace,
			Name:      workload.ResourceName(resourceName, "vision"),
		}, deployment)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
