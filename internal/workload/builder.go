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

// Package workload builds deterministic Kubernetes resources for technical processes.
package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
)

const (
	// LabelManagedBy identifies resources controlled by the factory operator.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// LabelProcess identifies the parent TechProcess.
	LabelProcess = "factory.local/tech-process"
	// LabelComponent identifies a process component.
	LabelComponent = "factory.local/component"
	// LabelRevision identifies an applied process revision.
	LabelRevision = "factory.local/revision"

	// AnnotationRevision forces pod-template changes for every process revision.
	AnnotationRevision = "factory.local/revision"

	parametersVolumeName = "factory-parameters"
	parametersMountPath  = "/etc/factory/parameters"
)

// Revision returns a deterministic short hash for the desired process state.
func Revision(process *productionv1alpha1.TechProcess, template *productionv1alpha1.TechProcessTemplate) (string, error) {
	payload := struct {
		TemplateName     string
		TemplateRevision string
		Components       []productionv1alpha1.ComponentTemplate
		TargetSelector   metav1.LabelSelector
		Parameters       map[string]string
		Rollout          productionv1alpha1.RolloutSpec
	}{
		TemplateName:     template.Name,
		TemplateRevision: template.Spec.Revision,
		Components:       template.Spec.Components,
		TargetSelector:   process.Spec.TargetSelector,
		Parameters:       process.Spec.Parameters,
		Rollout:          process.Spec.Rollout,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])[:12], nil
}

// ParametersConfigMapName returns the revision-specific parameters ConfigMap name.
func ParametersConfigMapName(processName, revision string) string {
	return ResourceName(processName, "params-"+revision)
}

// ParametersConfigMap builds an immutable process parameters ConfigMap.
func ParametersConfigMap(process *productionv1alpha1.TechProcess, revision string) *corev1.ConfigMap {
	data := make(map[string]string, len(process.Spec.Parameters))
	maps.Copy(data, process.Spec.Parameters)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ParametersConfigMapName(process.Name, revision),
			Namespace: process.Namespace,
			Labels: map[string]string{
				LabelManagedBy: "factory-platform",
				LabelProcess:   LabelValue(process.Name),
				LabelRevision:  revision,
			},
		},
		Immutable: ptr.To(true),
		Data:      data,
	}
}

// Deployment builds a Deployment for one process component.
func Deployment(
	process *productionv1alpha1.TechProcess,
	component productionv1alpha1.ComponentTemplate,
	revision string,
) *appsv1.Deployment {
	labels := componentLabels(process.Name, component.Name, revision)
	replicas := int32(1)
	if component.Replicas != nil {
		replicas = *component.Replicas
	}

	strategy := appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	if process.Spec.Rollout.Strategy == productionv1alpha1.RolloutStrategyRollingUpdate {
		strategy = appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: ptr.To(intstr.FromInt32(1)),
				MaxSurge:       ptr.To(intstr.FromInt32(0)),
			},
		}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(process.Name, component.Name),
			Namespace: process.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(process.Name, component.Name)},
			Strategy: strategy,
			Template: podTemplate(process, component, revision, labels),
		},
	}
}

// DaemonSet builds a DaemonSet for one process component.
func DaemonSet(
	process *productionv1alpha1.TechProcess,
	component productionv1alpha1.ComponentTemplate,
	revision string,
) *appsv1.DaemonSet {
	labels := componentLabels(process.Name, component.Name, revision)
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(process.Name, component.Name),
			Namespace: process.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(process.Name, component.Name)},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxUnavailable: ptr.To(intstr.FromInt32(1)),
					MaxSurge:       ptr.To(intstr.FromInt32(0)),
				},
			},
			Template: podTemplate(process, component, revision, labels),
		},
	}
}

// ResourceName creates a deterministic DNS label and preserves uniqueness when truncated.
func ResourceName(parts ...string) string {
	base := strings.Trim(strings.Join(parts, "-"), "-")
	if len(base) <= 63 {
		return base
	}

	sum := sha256.Sum256([]byte(base))
	suffix := hex.EncodeToString(sum[:])[:8]
	prefix := strings.TrimRight(base[:63-len(suffix)-1], "-")
	return prefix + "-" + suffix
}

// LabelValue returns a valid, deterministic label value for an object name.
func LabelValue(value string) string {
	if len(value) <= 63 {
		return value
	}
	return ResourceName(value)
}

func podTemplate(
	process *productionv1alpha1.TechProcess,
	component productionv1alpha1.ComponentTemplate,
	revision string,
	labels map[string]string,
) corev1.PodTemplateSpec {
	env := append([]corev1.EnvVar{}, component.Env...)
	env = append(env,
		corev1.EnvVar{Name: "FACTORY_TECH_PROCESS", Value: process.Name},
		corev1.EnvVar{Name: "FACTORY_REVISION", Value: revision},
		corev1.EnvVar{Name: "FACTORY_PARAMETERS_DIR", Value: parametersMountPath},
	)

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
			Annotations: map[string]string{
				AnnotationRevision: revision,
			},
		},
		Spec: corev1.PodSpec{
			Affinity: nodeAffinity(process.Spec.TargetSelector, component.RequiredCapabilities),
			Containers: []corev1.Container{{
				Name:            component.Name,
				Image:           component.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         append([]string(nil), component.Command...),
				Args:            append([]string(nil), component.Args...),
				Env:             env,
				Ports:           append([]corev1.ContainerPort(nil), component.Ports...),
				Resources:       *component.Resources.DeepCopy(),
				ReadinessProbe:  component.ReadinessProbe.DeepCopy(),
				LivenessProbe:   component.LivenessProbe.DeepCopy(),
				VolumeMounts: []corev1.VolumeMount{{
					Name:      parametersVolumeName,
					MountPath: parametersMountPath,
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: parametersVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: ParametersConfigMapName(process.Name, revision),
						},
					},
				},
			}},
		},
	}
}

func componentLabels(processName, componentName, revision string) map[string]string {
	labels := selectorLabels(processName, componentName)
	labels[LabelManagedBy] = "factory-platform"
	labels[LabelRevision] = revision
	return labels
}

func selectorLabels(processName, componentName string) map[string]string {
	return map[string]string{
		LabelProcess:   LabelValue(processName),
		LabelComponent: LabelValue(componentName),
	}
}

func nodeAffinity(selectors ...metav1.LabelSelector) *corev1.Affinity {
	requirements := make([]corev1.NodeSelectorRequirement, 0)
	for _, selector := range selectors {
		keys := make([]string, 0, len(selector.MatchLabels))
		for key := range selector.MatchLabels {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			requirements = append(requirements, corev1.NodeSelectorRequirement{
				Key:      key,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{selector.MatchLabels[key]},
			})
		}
		for _, expression := range selector.MatchExpressions {
			requirements = append(requirements, corev1.NodeSelectorRequirement{
				Key:      expression.Key,
				Operator: corev1.NodeSelectorOperator(expression.Operator),
				Values:   append([]string(nil), expression.Values...),
			})
		}
	}

	if len(requirements) == 0 {
		return nil
	}

	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: requirements}},
			},
		},
	}
}
