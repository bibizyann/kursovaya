//go:build e2e

package e2e

import (
	"encoding/json"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/test/utils"
)

const (
	telemetryNamespace   = "factory-telemetry"
	mqttBrokerImage      = "eclipse-mosquitto:2.0.22" //nolint:misspell // Official image name.
	mqttBrokerDeployment = "mosquitto"                //nolint:misspell // Official project name.
)

func telemetryPodUIDs(g Gomega) map[string]string {
	data, err := utils.Run(exec.Command("kubectl", "get", "pods", "-n", telemetryNamespace, "-o", "json"))
	g.Expect(err).NotTo(HaveOccurred())
	var pods corev1.PodList
	g.Expect(json.Unmarshal([]byte(data), &pods)).To(Succeed())
	g.Expect(pods.Items).To(HaveLen(2))
	uids := make(map[string]string, len(pods.Items))
	for _, pod := range pods.Items {
		g.Expect(pod.OwnerReferences).To(HaveLen(1))
		g.Expect(pod.OwnerReferences[0].Kind).To(Equal("ReplicaSet"))
		g.Expect(pod.DeletionTimestamp).To(BeNil())
		for _, status := range pod.Status.ContainerStatuses {
			g.Expect(status.Ready).To(BeTrue())
			g.Expect(status.RestartCount).To(BeZero())
		}
		uids[pod.Labels["app.kubernetes.io/name"]] = string(pod.UID)
	}
	return uids
}

func verifyEquipmentMetrics(g Gomega, product, revision string) {
	data, err := utils.Run(exec.Command("kubectl", "get", "--raw",
		"/api/v1/namespaces/"+telemetryNamespace+"/services/telemetry-receiver:9091/proxy/metrics"))
	g.Expect(err).NotTo(HaveOccurred())
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(data))
	g.Expect(err).NotTo(HaveOccurred())
	for _, name := range []string{
		"factory_equipment_telemetry_fresh", "factory_equipment_healthy", "factory_equipment_info",
	} {
		family := families[name]
		g.Expect(family).NotTo(BeNil(), name)
		g.Expect(family.GetMetric()).To(HaveLen(len(equipment.Catalog())), name)
		seen := make(map[string]bool)
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string)
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			device, lookupErr := equipment.Lookup(labels["equipment_id"])
			g.Expect(lookupErr).NotTo(HaveOccurred())
			g.Expect(seen[device.ID]).To(BeFalse())
			seen[device.ID] = true
			g.Expect(labels["namespace"]).To(Equal("factory-demo"))
			g.Expect(labels["tech_process"]).To(Equal("line-one"))
			g.Expect(labels["workstation"]).To(Equal(device.Workstation))
			g.Expect(metric.GetGauge().GetValue()).To(Equal(float64(1)))
			if name == "factory_equipment_info" {
				g.Expect(labels["product"]).To(Equal(product))
				g.Expect(labels["revision"]).To(Equal(revision))
			}
		}
	}
}
