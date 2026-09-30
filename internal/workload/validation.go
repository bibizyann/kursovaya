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
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
)

var configMapKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Validate verifies constraints that involve more than one API field.
func Validate(process *productionv1alpha1.TechProcess, template *productionv1alpha1.TechProcessTemplate) error {
	if strings.TrimSpace(process.Spec.TemplateRef.Name) == "" {
		return fmt.Errorf("templateRef.name must not be empty")
	}
	if _, err := metav1.LabelSelectorAsSelector(&process.Spec.TargetSelector); err != nil {
		return fmt.Errorf("invalid targetSelector: %w", err)
	}
	if len(template.Spec.Components) == 0 {
		return fmt.Errorf("template must contain at least one component")
	}

	for key := range process.Spec.Parameters {
		if !configMapKeyPattern.MatchString(key) {
			return fmt.Errorf("parameter key %q is not a valid ConfigMap key", key)
		}
	}

	seen := make(map[string]struct{}, len(template.Spec.Components))
	for index, component := range template.Spec.Components {
		if component.Name == "" {
			return fmt.Errorf("component %d has an empty name", index)
		}
		if _, exists := seen[component.Name]; exists {
			return fmt.Errorf("component name %q is duplicated", component.Name)
		}
		seen[component.Name] = struct{}{}

		if strings.TrimSpace(component.Image) == "" {
			return fmt.Errorf("component %q has an empty image", component.Name)
		}
		if _, err := metav1.LabelSelectorAsSelector(&component.RequiredCapabilities); err != nil {
			return fmt.Errorf("component %q has invalid requiredCapabilities: %w", component.Name, err)
		}

		switch component.Workload {
		case "", productionv1alpha1.WorkloadKindDeployment:
			if component.Replicas != nil && *component.Replicas < 1 {
				return fmt.Errorf("component %q replicas must be positive", component.Name)
			}
		case productionv1alpha1.WorkloadKindDaemonSet:
			if component.Replicas != nil {
				return fmt.Errorf("component %q cannot set replicas for a DaemonSet", component.Name)
			}
		default:
			return fmt.Errorf("component %q uses unsupported workload kind %q", component.Name, component.Workload)
		}

		for _, variable := range component.Env {
			if strings.HasPrefix(variable.Name, "FACTORY_") {
				return fmt.Errorf("component %q uses reserved environment variable %q", component.Name, variable.Name)
			}
		}
	}

	return nil
}
