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

package workload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
)

const profileParameterKey = "profile"

func TestRevisionIsStableAndChangesWithDesiredState(t *testing.T) {
	t.Parallel()

	const boardA = "board-a"
	processA, template := testObjects()
	processB := processA.DeepCopy()
	processB.Spec.Parameters = map[string]string{profileParameterKey: boardA, "threshold": "0.95"}
	processA.Spec.Parameters = map[string]string{"threshold": "0.95", profileParameterKey: boardA}

	revisionA, err := Revision(processA, template)
	if err != nil {
		t.Fatalf("revision A: %v", err)
	}
	revisionB, err := Revision(processB, template)
	if err != nil {
		t.Fatalf("revision B: %v", err)
	}
	if revisionA != revisionB {
		t.Fatalf("map order changed revision: %q != %q", revisionA, revisionB)
	}

	processB.Spec.Parameters["threshold"] = "0.90"
	revisionC, err := Revision(processB, template)
	if err != nil {
		t.Fatalf("revision C: %v", err)
	}
	if revisionA == revisionC {
		t.Fatalf("changed parameters did not change revision %q", revisionA)
	}
}

func TestDeploymentUsesSafeDefaultsAndCombinedAffinity(t *testing.T) {
	t.Parallel()

	process, template := testObjects()
	revision := "0123456789ab"
	deployment := Deployment(process, template.Spec.Components[0], revision)

	if deployment.Spec.Strategy.Type != "Recreate" {
		t.Fatalf("strategy = %q, want Recreate", deployment.Spec.Strategy.Type)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("replicas = %v, want 1", deployment.Spec.Replicas)
	}

	affinity := deployment.Spec.Template.Spec.Affinity
	if affinity == nil || affinity.NodeAffinity == nil || affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("required node affinity was not created")
	}
	requirements := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(requirements) != 2 {
		t.Fatalf("affinity requirements = %d, want 2", len(requirements))
	}

	container := deployment.Spec.Template.Spec.Containers[0]
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != parametersMountPath {
		t.Fatalf("unexpected parameter volume mounts: %#v", container.VolumeMounts)
	}
	if got := deployment.Spec.Template.Annotations[AnnotationRevision]; got != revision {
		t.Fatalf("revision annotation = %q, want %q", got, revision)
	}
}

func TestValidateRejectsDuplicateComponents(t *testing.T) {
	t.Parallel()

	process, template := testObjects()
	template.Spec.Components = append(template.Spec.Components, template.Spec.Components[0])
	if err := Validate(process, template); err == nil {
		t.Fatal("Validate accepted duplicate component names")
	}
}

func testObjects() (*productionv1alpha1.TechProcess, *productionv1alpha1.TechProcessTemplate) {
	const boardA = "board-a"
	process := &productionv1alpha1.TechProcess{
		ObjectMeta: metav1.ObjectMeta{Name: "line-one", Namespace: "factory"},
		Spec: productionv1alpha1.TechProcessSpec{
			TemplateRef: corev1.LocalObjectReference{Name: boardA},
			TargetSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				"factory.local/line": "line-one",
			}},
			Parameters: map[string]string{profileParameterKey: boardA},
		},
	}
	template := &productionv1alpha1.TechProcessTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: boardA, Namespace: "factory"},
		Spec: productionv1alpha1.TechProcessTemplateSpec{
			Revision: "1.0.0",
			Components: []productionv1alpha1.ComponentTemplate{{
				Name:  "vision",
				Image: "factory.local/vision@sha256:example",
				RequiredCapabilities: metav1.LabelSelector{MatchLabels: map[string]string{
					"factory.local/camera": "true",
				}},
			}},
		},
	}
	return process, template
}
