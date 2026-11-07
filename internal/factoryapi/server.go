// Package factoryapi serves the read-only workshop API and embedded dispatcher interface.
package factoryapi

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"factory.local/factory-platform/internal/factory"
)

//go:embed web/*
var assets embed.FS

// ClusterReader reads a scoped snapshot of desired and observed workloads.
type ClusterReader interface {
	Read(context.Context, string, string) (factory.ClusterSnapshot, error)
}

// TelemetryReader reads fresh observations independently of workload readiness.
type TelemetryReader interface {
	Read(context.Context, string, string) (factory.TelemetrySnapshot, error)
}

// Config scopes this read-only server to a single process.
type Config struct {
	Namespace  string
	Process    string
	GrafanaURL string
	Timeout    time.Duration
	Cluster    ClusterReader
	Telemetry  TelemetryReader
	Logger     *slog.Logger
}

// New validates configuration and returns a handler with no write routes.
func New(config Config) (http.Handler, error) {
	if config.Namespace == "" || config.Process == "" || config.Cluster == nil || config.Telemetry == nil {
		return nil, fmt.Errorf("namespace, process and both source readers are required")
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.GrafanaURL != "" {
		u, err := url.Parse(config.GrafanaURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return nil, fmt.Errorf("grafana URL must be an absolute HTTP(S) URL without credentials")
		}
	}
	page, err := template.ParseFS(assets, "web/index.html")
	if err != nil {
		return nil, err
	}
	web, err := fs.Sub(assets, "web")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := page.Execute(w, config); err != nil {
			config.Logger.Error("Page rendering failed", "error", err)
		}
	})
	for _, name := range []string{"app.js", "style.css"} {
		mux.Handle("GET /"+name, http.FileServer(http.FS(web)))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "alive"}, config.Logger)
	})
	mux.HandleFunc("GET /api/v1/overview", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, overview(r.Context(), config), config.Logger)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	}), nil
}

func overview(parent context.Context, config Config) factory.Overview {
	ctx, cancel := context.WithTimeout(parent, config.Timeout)
	defer cancel()
	var cluster factory.ClusterSnapshot
	var telemetry factory.TelemetrySnapshot
	var clusterErr, telemetryErr error
	var group sync.WaitGroup
	group.Go(func() { cluster, clusterErr = config.Cluster.Read(ctx, config.Namespace, config.Process) })
	group.Go(func() { telemetry, telemetryErr = config.Telemetry.Read(ctx, config.Namespace, config.Process) })
	group.Wait()
	kube, prom := factory.Source{Available: clusterErr == nil}, factory.Source{Available: telemetryErr == nil}
	if clusterErr != nil {
		kube.Reason = "KubernetesUnavailable"
		config.Logger.Warn("Kubernetes overview read failed", "error", clusterErr)
	}
	if telemetryErr != nil {
		prom.Reason = "PrometheusUnavailable"
		config.Logger.Warn("Prometheus overview read failed", "error", telemetryErr)
	}
	result := factory.Build(config.Namespace, config.Process, time.Now(), cluster, telemetry, kube, prom)
	result.GrafanaURL = grafanaLink(config, "factory-processes", "", "")
	for i := range result.Workstations {
		for j := range result.Workstations[i].Devices {
			device := &result.Workstations[i].Devices[j]
			device.GrafanaURL = grafanaLink(config, "factory-equipment", device.Workstation, device.ID)
		}
	}
	return result
}

func grafanaLink(config Config, dashboard, workstation, device string) string {
	if config.GrafanaURL == "" {
		return ""
	}
	u, _ := url.Parse(config.GrafanaURL)
	u.Path = strings.TrimRight(u.Path, "/") + "/d/" + dashboard
	q := u.Query()
	q.Set("var-namespace", config.Namespace)
	q.Set("var-process", config.Process)
	if workstation != "" {
		q.Set("var-workstation", workstation)
	}
	if device != "" {
		q.Set("var-equipment", device)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func writeJSON(w http.ResponseWriter, value any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Error("Response encoding failed", "error", err)
	}
}
