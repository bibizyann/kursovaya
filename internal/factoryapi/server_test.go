package factoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"factory.local/factory-platform/internal/factory"
)

const testProcess = "line-one"

type clusterFunc func(context.Context, string, string) (factory.ClusterSnapshot, error)

func (f clusterFunc) Read(ctx context.Context, ns, name string) (factory.ClusterSnapshot, error) {
	return f(ctx, ns, name)
}

type telemetryFunc func(context.Context, string, string) (factory.TelemetrySnapshot, error)

func (f telemetryFunc) Read(ctx context.Context, ns, name string) (factory.TelemetrySnapshot, error) {
	return f(ctx, ns, name)
}

func testConfig() Config {
	return Config{Namespace: "factory-demo", Process: testProcess, GrafanaURL: "http://localhost:3000", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cluster: clusterFunc(func(context.Context, string, string) (factory.ClusterSnapshot, error) {
			return factory.ClusterSnapshot{Process: factory.Process{State: factory.StateReady}}, nil
		}),
		Telemetry: telemetryFunc(func(context.Context, string, string) (factory.TelemetrySnapshot, error) {
			return factory.TelemetrySnapshot{Receiver: factory.Source{Available: true}}, nil
		})}
}

func TestReadOnlyRoutesAndHeaders(t *testing.T) {
	handler, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		status       int
		contains     string
	}{
		{http.MethodGet, "/", 200, "Сборочный участок"}, {http.MethodGet, "/app.js", 200, "/api/v1/overview"}, {http.MethodGet, "/style.css", 200, ".sidebar"}, {http.MethodGet, "/healthz", 200, "alive"},
		{"POST", "/api/v1/overview", 405, ""}, {"PUT", "/api/v1/mode", 404, ""}, {http.MethodGet, "/index.html", 404, ""}, {http.MethodGet, "/unknown", 404, ""},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s %s = %d", tc.method, tc.path, rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing response security headers")
		}
	}
}

func TestPartialOverviewAndRecovery(t *testing.T) {
	config := testConfig()
	failed := true
	config.Telemetry = telemetryFunc(func(_ context.Context, ns, name string) (factory.TelemetrySnapshot, error) {
		if ns != "factory-demo" || name != testProcess {
			t.Fatal("request changed scope")
		}
		if failed {
			return factory.TelemetrySnapshot{}, errors.New("secret upstream error")
		}
		return factory.TelemetrySnapshot{Receiver: factory.Source{Available: true}}, nil
	})
	handler, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []bool{true, false} {
		failed = failure
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview?namespace=other&query=anything", nil))
		var got factory.Overview
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Process.State != factory.StateReady || got.Prometheus.Available == failure || len(got.Workstations) != 6 {
			t.Fatalf("partial result: %#v", got)
		}
		if strings.Contains(rec.Body.String(), "secret upstream error") {
			t.Fatal("upstream error leaked")
		}
		link, err := url.Parse(got.Workstations[2].Devices[0].GrafanaURL)
		if err != nil {
			t.Fatal(err)
		}
		if link.Query().Get("var-process") != testProcess || link.Query().Get("var-equipment") != "reflow-oven-01" {
			t.Fatal("incorrect Grafana filters")
		}
	}
}

func TestSourceDeadlinePreservesOtherSource(t *testing.T) {
	config := testConfig()
	config.Timeout = 10 * time.Millisecond
	config.Cluster = clusterFunc(func(ctx context.Context, _, _ string) (factory.ClusterSnapshot, error) {
		<-ctx.Done()
		return factory.ClusterSnapshot{}, ctx.Err()
	})
	handler, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	var got factory.Overview
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kubernetes.Available || !got.Prometheus.Available || got.Process.State != factory.StateUnknown {
		t.Fatal("deadline invalidated the independent source")
	}
}

func TestRejectsUnsafeGrafanaURLs(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "//example.com", "https://user:password@example.com"} {
		config := testConfig()
		config.GrafanaURL = value
		if _, err := New(config); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}
