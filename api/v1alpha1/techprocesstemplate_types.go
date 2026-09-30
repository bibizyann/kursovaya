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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// WorkloadKind identifies the Kubernetes resource used for a component.
// +kubebuilder:validation:Enum=Deployment;DaemonSet
type WorkloadKind string

const (
	// WorkloadKindDeployment creates a Deployment for a component.
	WorkloadKindDeployment WorkloadKind = "Deployment"
	// WorkloadKindDaemonSet creates one component instance on each matching node.
	WorkloadKindDaemonSet WorkloadKind = "DaemonSet"
)

// ComponentTemplate describes one containerized part of a technical process.
type ComponentTemplate struct {
	// Name is unique inside the template and is used in child resource names.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Workload selects Deployment or DaemonSet.
	// +kubebuilder:default=Deployment
	// +optional
	Workload WorkloadKind `json:"workload,omitempty"`

	// Image is an immutable image reference or digest.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// Replicas applies to Deployment workloads and defaults to one.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// RequiredCapabilities are AND-ed with TechProcess targetSelector.
	// +optional
	RequiredCapabilities metav1.LabelSelector `json:"requiredCapabilities,omitempty"`

	// Command overrides the image entrypoint.
	// +optional
	Command []string `json:"command,omitempty"`

	// Args overrides image arguments.
	// +optional
	Args []string `json:"args,omitempty"`

	// Env contains component-specific environment variables.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Ports declares application ports exposed by the container.
	// +optional
	Ports []corev1.ContainerPort `json:"ports,omitempty"`

	// Resources declares compute resource requests and limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// ReadinessProbe verifies software and equipment readiness.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// LivenessProbe verifies that the component must be restarted.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
}

// TechProcessTemplateSpec defines a versioned catalog entry.
type TechProcessTemplateSpec struct {
	// Revision is the human-readable catalog version.
	// +kubebuilder:validation:MinLength=1
	Revision string `json:"revision"`

	// Description explains the process purpose to dispatchers.
	// +optional
	Description string `json:"description,omitempty"`

	// Components are the workloads required by the process.
	// +kubebuilder:validation:MinItems=1
	Components []ComponentTemplate `json:"components"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.spec.revision`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TechProcessTemplate is the Schema for the techprocesstemplates API.
type TechProcessTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              TechProcessTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// TechProcessTemplateList contains a list of TechProcessTemplate.
type TechProcessTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TechProcessTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TechProcessTemplate{}, &TechProcessTemplateList{})
		return nil
	})
}
