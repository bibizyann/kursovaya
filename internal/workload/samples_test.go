package workload

import (
	"os"
	"strings"
	"testing"

	productionv1alpha1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/simulator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

func TestWorkshopSamplesMatchCatalogAndKind(t *testing.T) {
	var process productionv1alpha1.TechProcess
	readSample(t, "../../config/samples/production_v1alpha1_techprocess.yaml", &process)
	for _, product := range []string{"board-a", "board-b"} {
		var template productionv1alpha1.TechProcessTemplate
		readSample(t, "../../config/samples/production_v1alpha1_techprocesstemplate_"+strings.ReplaceAll(product, "-", "_")+".yaml", &template)
		if len(template.Spec.Components) != len(equipment.Catalog()) {
			t.Fatal("sample must contain all catalog equipment")
		}
		ids := map[string]bool{}
		for _, component := range template.Spec.Components {
			device, err := equipment.Lookup(component.Name)
			if err != nil {
				t.Fatal(err)
			}
			if ids[device.ID] {
				t.Fatalf("duplicate device %s", device.ID)
			}
			ids[device.ID] = true
			config := simulator.Config{InitialMode: simulator.ModeNormal}
			for _, arg := range component.Args {
				if value, ok := strings.CutPrefix(arg, "--equipment="); ok {
					config.EquipmentID = value
				}
				if value, ok := strings.CutPrefix(arg, "--product="); ok {
					config.Product = value
				}
			}
			if config.EquipmentID != device.ID || config.Product != product {
				t.Fatalf("wrong args for %s: %v", device.ID, component.Args)
			}
			if _, err := simulator.New(config); err != nil {
				t.Fatal(err)
			}
			if component.ReadinessProbe == nil || component.ReadinessProbe.HTTPGet == nil || component.ReadinessProbe.HTTPGet.Path != "/readyz" {
				t.Fatal("equipment readiness must gate deployment")
			}
			selector, err := metav1.LabelSelectorAsSelector(&component.RequiredCapabilities)
			if err != nil {
				t.Fatal(err)
			}
			if selector.Empty() || !selector.Matches(labels.Set{"factory.local/" + device.Capability: "true"}) {
				t.Fatalf("incorrect capability for %s", device.ID)
			}
			target, err := metav1.LabelSelectorAsSelector(&process.Spec.TargetSelector)
			if err != nil {
				t.Fatal(err)
			}
			for _, filename := range []string{"cluster.yaml", "cluster-single.yaml"} {
				var cluster struct {
					Kind       string `json:"kind"`
					APIVersion string `json:"apiVersion"`
					Nodes      []struct {
						Role   string            `json:"role"`
						Labels map[string]string `json:"labels"`
					} `json:"nodes"`
				}
				readSample(t, "../../infra/kind/"+filename, &cluster)
				matches := 0
				for _, node := range cluster.Nodes {
					if selector.Matches(labels.Set(node.Labels)) && target.Matches(labels.Set(node.Labels)) {
						matches++
					}
				}
				want := 1
				if filename == "cluster.yaml" {
					want = 2
				}
				if matches != want {
					t.Fatalf("%s: %s has %d compatible nodes, want %d", filename, device.ID, matches, want)
				}
			}
		}
	}
}

func readSample(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.UnmarshalStrict(data, target); err != nil {
		t.Fatal(err)
	}
}
