//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"

	. "github.com/onsi/gomega"

	"factory.local/factory-platform/test/utils"
)

func verifyMonitoringStack(g Gomega, revision string) {
	const servicePrefix = "/api/v1/namespaces/factory-monitoring/services/"
	health, err := utils.Run(exec.Command("kubectl", "get", "--raw",
		servicePrefix+"factory-grafana:80/proxy/api/health"))
	g.Expect(err).NotTo(HaveOccurred())
	var grafanaHealth struct {
		Database string `json:"database"`
	}
	g.Expect(json.Unmarshal([]byte(health), &grafanaHealth)).To(Succeed())
	g.Expect(grafanaHealth.Database).To(Equal("ok"))
	queries := map[string]string{
		"factory:receiver_up":                                               "1",
		"factory:operator_up":                                               "1",
		"count(factory:equipment_fresh == 1)":                               "8",
		"count(factory_techprocess_ready_workloads == 8)":                   "1",
		fmt.Sprintf("count(factory_equipment_info{revision=%q})", revision): "8",
	}
	for query, expected := range queries {
		data, err := utils.Run(exec.Command("kubectl", "get", "--raw",
			servicePrefix+"factory-monitoring-prometheus:9090/proxy/api/v1/query?query="+url.QueryEscape(query)))
		g.Expect(err).NotTo(HaveOccurred())
		var response struct {
			Status string `json:"status"`
			Data   struct {
				Result []struct {
					Value []json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		g.Expect(json.Unmarshal([]byte(data), &response)).To(Succeed())
		g.Expect(response.Status).To(Equal("success"), query)
		g.Expect(response.Data.Result).To(HaveLen(1), query)
		g.Expect(response.Data.Result[0].Value).To(HaveLen(2))
		var value string
		g.Expect(json.Unmarshal(response.Data.Result[0].Value[1], &value)).To(Succeed())
		g.Expect(value).To(Equal(expected), query)
	}
}
