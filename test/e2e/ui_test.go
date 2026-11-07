//go:build e2e

package e2e

import (
	"encoding/json"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"factory.local/factory-platform/internal/factory"
	"factory.local/factory-platform/test/utils"
)

const overviewProxy = "/api/v1/namespaces/factory-ui/services/factory-api:8081/proxy"

func installOverview() {
	By("installing the overview with read-only namespace RBAC")
	DeferCleanup(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "namespace", "factory-ui", "--ignore-not-found"))
	})
	_, err := utils.Run(exec.Command("kubectl", "apply", "-k", "config/ui"))
	Expect(err).NotTo(HaveOccurred())
	_, err = utils.Run(exec.Command("kubectl", "set", "image", "deployment/factory-api",
		"api=factory-api:e2e", "-n", "factory-ui"))
	Expect(err).NotTo(HaveOccurred())
	_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "deployment/factory-api",
		"-n", "factory-ui", "--timeout=120s"))
	Expect(err).NotTo(HaveOccurred())
	page, err := utils.Run(exec.Command("kubectl", "get", "--raw", overviewProxy+"/"))
	Expect(err).NotTo(HaveOccurred())
	Expect(page).To(ContainSubstring("Сборочный участок"))
	output, err := utils.Run(exec.Command("kubectl", "auth", "can-i", "patch", "techprocesses.production.factory.local",
		"-n", "factory-demo", "--as=system:serviceaccount:factory-ui:factory-api"))
	Expect(err).To(HaveOccurred())
	Expect(output).To(ContainSubstring("no"))
}

func readOverview(g Gomega) factory.Overview {
	data, err := utils.Run(exec.Command("kubectl", "get", "--raw", overviewProxy+"/api/v1/overview"))
	g.Expect(err).NotTo(HaveOccurred())
	var overview factory.Overview
	g.Expect(json.Unmarshal([]byte(data), &overview)).To(Succeed())
	return overview
}

func verifyOverview(g Gomega, product, revision string, monitoring bool) {
	overview := readOverview(g)
	g.Expect(overview.Kubernetes.Available).To(BeTrue())
	g.Expect(overview.Process.State).To(Equal("Ready"))
	g.Expect(overview.Process.Revision).To(Equal(revision))
	g.Expect(overview.Process.Template).To(Equal(product))
	g.Expect(overview.Workstations).To(HaveLen(6))
	count := 0
	for _, station := range overview.Workstations {
		for _, device := range station.Devices {
			count++
			g.Expect(device.Workload.State).To(Equal("Ready"))
			g.Expect(device.Workload.Product).To(Equal(product))
			g.Expect(device.Workload.Pods).To(HaveLen(1))
			g.Expect(device.Workload.Pods[0].Node).NotTo(BeEmpty())
			if monitoring {
				g.Expect(device.Telemetry.State).To(Equal("Fresh"))
				g.Expect(device.Telemetry.Revision).To(Equal(revision))
				g.Expect(device.RevisionMatches).NotTo(BeNil())
				g.Expect(*device.RevisionMatches).To(BeTrue())
			}
		}
	}
	g.Expect(count).To(Equal(8))
}

func verifyOverviewReceiverOutage(revision string) {
	By("showing unknown telemetry without changing process readiness during receiver outage")
	DeferCleanup(func() {
		_, _ = utils.Run(exec.Command("kubectl", "scale", "deployment/telemetry-receiver",
			"-n", telemetryNamespace, "--replicas=1"))
	})
	_, err := utils.Run(exec.Command("kubectl", "scale", "deployment/telemetry-receiver",
		"-n", telemetryNamespace, "--replicas=0"))
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		overview := readOverview(g)
		g.Expect(overview.Receiver.Available).To(BeFalse())
		g.Expect(overview.Process.State).To(Equal("Ready"))
		g.Expect(overview.Process.Revision).To(Equal(revision))
		for _, station := range overview.Workstations {
			for _, device := range station.Devices {
				g.Expect(device.Telemetry.State).To(Equal("Unknown"))
				g.Expect(device.Telemetry.Readings).To(BeEmpty())
			}
		}
	}, time.Minute, 2*time.Second).Should(Succeed())
	_, err = utils.Run(exec.Command("kubectl", "scale", "deployment/telemetry-receiver",
		"-n", telemetryNamespace, "--replicas=1"))
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		verifyOverview(g, "board-b", revision, true)
	}, time.Minute, 2*time.Second).Should(Succeed())
}
