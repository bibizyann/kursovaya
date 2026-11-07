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
	"k8s.io/apimachinery/pkg/types"
)

// RolloutStrategy describes how workloads that belong to a process are replaced.
// +kubebuilder:validation:Enum=Recreate;RollingUpdate
type RolloutStrategy string

const (
	// RolloutStrategyRecreate stops the previous workload before starting its replacement.
	RolloutStrategyRecreate RolloutStrategy = "Recreate"
	// RolloutStrategyRollingUpdate gradually replaces stateless workloads.
	RolloutStrategyRollingUpdate RolloutStrategy = "RollingUpdate"
)

const (
	// ConditionReady reports whether the complete process revision can produce output.
	ConditionReady = "Ready"
	// ConditionProgressing reports whether the process is converging to its desired revision.
	ConditionProgressing = "Progressing"
	// ConditionDegraded reports an error that prevents the process from becoming ready.
	ConditionDegraded = "Degraded"
)

// RolloutSpec configures process workload replacement.
type RolloutSpec struct {
	// Strategy is the replacement strategy. Hardware-bound workloads should use Recreate.
	// +kubebuilder:default=Recreate
	// +optional
	Strategy RolloutStrategy `json:"strategy,omitempty"`
}

// TechProcessSpec defines the desired state of TechProcess.
type TechProcessSpec struct {
	// TemplateRef identifies a TechProcessTemplate in the same namespace.
	// +required
	TemplateRef corev1.LocalObjectReference `json:"templateRef"`

	// TargetSelector limits all process workloads to the selected factory nodes.
	// +required
	TargetSelector metav1.LabelSelector `json:"targetSelector"`

	// Parameters are exposed to workloads through a read-only ConfigMap volume.
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// Rollout configures workload replacement.
	// +optional
	Rollout RolloutSpec `json:"rollout,omitempty"`

	// Timeout is the maximum time allowed for the revision to become ready.
	// +kubebuilder:default="5m"
	// +optional
	Timeout metav1.Duration `json:"timeout,omitempty"`
}

// ComponentStatus reports rollout progress for one template component.
type ComponentStatus struct {
	// Name is the component name from the selected template.
	Name string `json:"name"`
	// Kind is the Kubernetes workload kind created for the component.
	Kind WorkloadKind `json:"kind"`
	// Desired is the number of desired workload instances.
	Desired int32 `json:"desired"`
	// Ready counts confirmed ready instances of the reconciled revision.
	Ready int32 `json:"ready"`
}

// WorkstationStatus reports process and equipment readiness on one node.
// Equipment fields are populated after the edge-agent integration is enabled.
type WorkstationStatus struct {
	// Name is the Kubernetes node name.
	Name string `json:"name"`
	// Revision is the process revision observed on the node.
	Revision string `json:"revision,omitempty"`
	// WorkloadReady reports Kubernetes workload readiness.
	WorkloadReady bool `json:"workloadReady"`
	// EquipmentReady reports hardware connectivity.
	EquipmentReady bool `json:"equipmentReady"`
	// CalibrationReady reports whether equipment calibration succeeded.
	CalibrationReady bool `json:"calibrationReady"`
	// Message contains a short diagnostic explanation.
	Message string `json:"message,omitempty"`
}

// TechProcessStatus defines the observed state of TechProcess.
type TechProcessStatus struct {
	// ObservedGeneration is the most recent generation processed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// ObservedTemplateGeneration identifies the template spec used for these conditions.
	// Zero means the referenced template was missing.
	// +optional
	ObservedTemplateGeneration int64 `json:"observedTemplateGeneration,omitempty"`
	// ObservedTemplateUID distinguishes deletion and recreation of the referenced template.
	// +optional
	ObservedTemplateUID types.UID `json:"observedTemplateUID,omitempty"`
	// Conditions summarize process availability, progress, and degradation.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// AppliedRevision is the deterministic revision currently being reconciled.
	AppliedRevision string `json:"appliedRevision,omitempty"`
	// DesiredWorkloads is the number of desired workload instances.
	DesiredWorkloads int32 `json:"desiredWorkloads,omitempty"`
	// ReadyWorkloads counts confirmed ready instances of the reconciled revision.
	ReadyWorkloads int32 `json:"readyWorkloads,omitempty"`
	// Components contains per-component rollout progress.
	// +listType=map
	// +listMapKey=name
	// +optional
	Components []ComponentStatus `json:"components,omitempty"`
	// Workstations contains per-node readiness once edge-agent status is available.
	// +listType=map
	// +listMapKey=name
	// +optional
	Workstations []WorkstationStatus `json:"workstations,omitempty"`
	// StartedAt is persisted before applying children for a new revision.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// CompletedAt records the first readiness of the current revision and survives later health changes.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.templateRef.name`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyWorkloads`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.status.desiredWorkloads`
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.status.appliedRevision`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TechProcess is the Schema for the techprocesses API.
type TechProcess struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              TechProcessSpec `json:"spec"`
	// +optional
	Status TechProcessStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TechProcessList contains a list of TechProcess.
type TechProcessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TechProcess `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TechProcess{}, &TechProcessList{})
		return nil
	})
}
