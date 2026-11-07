//go:build monitoring

package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

type dashboard struct {
	UID    string `json:"uid"`
	Panels []struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
}

func command(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRulesAndDashboardQueries(t *testing.T) {
	root, err := filepath.Abs("../../config/monitoring")
	if err != nil {
		t.Fatal(err)
	}
	image := ""
	for line := range strings.SplitSeq(string(readFile(t, filepath.Join(root, "versions.env"))), "\n") {
		if value, found := strings.CutPrefix(line, "PROMETHEUS_IMAGE="); found {
			image = value
		}
	}
	if image == "" {
		t.Fatal("PROMETHEUS_IMAGE must be pinned")
	}
	var resource struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := yaml.Unmarshal(readFile(t, filepath.Join(root, "rules.yaml")), &resource); err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	// Prometheus runs without root and must be able to read the mounted test configuration.
	if err := os.Chmod(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(temp, "rules.yaml"), resource.Spec)
	writeFile(t, filepath.Join(temp, "rules.test.yaml"), readFile(t, filepath.Join(root, "tests/rules.test.yaml")))
	for _, args := range [][]string{{"check", "rules", "/work/rules.yaml"}, {"test", "rules", "/work/rules.test.yaml"}} {
		base := make([]string, 0, 9+len(args))
		base = append(base, "run", "--rm", "--network=none", "-v", temp+":/work:ro",
			"-w", "/work", "--entrypoint=/bin/promtool", image)
		t.Log(command(t, append(base, args...)...))
	}
	writeFile(t, filepath.Join(temp, "prometheus.yaml"), []byte(`global:
  evaluation_interval: 5s
rule_files: [/work/rules.yaml]
scrape_configs: []
`))
	name := fmt.Sprintf("factory-monitoring-check-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput(); err != nil {
			t.Logf("cleanup: %v: %s", err, out)
		}
	})
	command(t, "run", "-d", "--name", name, "-p", "127.0.0.1::9090", "-v", temp+":/work:ro", image,
		"--config.file=/work/prometheus.yaml", "--storage.tsdb.path=/prometheus")
	address := command(t, "port", name, "9090/tcp")
	baseURL := "http://" + address
	httpClient := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		response, requestErr := httpClient.Get(baseURL + "/-/ready")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
	}
	if !ready {
		t.Fatal("temporary Prometheus did not become ready")
	}
	files, err := filepath.Glob(filepath.Join(root, "dashboards/*.json"))
	if err != nil || len(files) != 3 {
		t.Fatalf("expected three dashboards, got %v: %v", files, err)
	}
	seen := make(map[string]bool)
	replace := strings.NewReplacer("$namespace", ".*", "$process", ".*", "$workstation", ".*", "$equipment", ".*")
	for _, path := range files {
		var board dashboard
		if err := json.Unmarshal(readFile(t, path), &board); err != nil {
			t.Fatal(err)
		}
		if board.UID == "" || seen[board.UID] || len(board.Panels) == 0 {
			t.Fatalf("invalid dashboard identity or panels: %s", path)
		}
		seen[board.UID] = true
		for _, panel := range board.Panels {
			for _, target := range panel.Targets {
				query := replace.Replace(target.Expr)
				if query == "" {
					t.Fatalf("empty query in %s", panel.Title)
				}
				response, err := httpClient.Get(baseURL + "/api/v1/query?query=" + url.QueryEscape(query))
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Status string `json:"status"`
					Error  string `json:"error"`
				}
				err = json.NewDecoder(response.Body).Decode(&result)
				_ = response.Body.Close()
				if err != nil || result.Status != "success" {
					t.Fatalf("%s / %s: %s: %v (%s)", board.UID, panel.Title, query, err, result.Error)
				}
			}
		}
	}
}
