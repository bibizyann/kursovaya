// Command factory-api serves the read-only workshop overview.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	productionv1 "factory.local/factory-platform/api/v1alpha1"
	"factory.local/factory-platform/internal/factoryapi"
	"factory.local/factory-platform/internal/factorysource"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], logger); err != nil {
		logger.Error("Factory API stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	flags := flag.NewFlagSet("factory-api", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:8081", "HTTP listen address")
	kubeconfig := flags.String("kubeconfig", "", "Explicit kubeconfig path; empty uses the in-cluster service account")
	namespace := flags.String("namespace", "factory-demo", "Allowed process namespace")
	process := flags.String("process", "line-one", "Allowed technical process name")
	prometheus := flags.String("prometheus-url", "http://factory-monitoring-prometheus.factory-monitoring.svc:9090", "Prometheus base URL")
	grafana := flags.String("grafana-url", "http://localhost:3000", "Browser-accessible Grafana base URL")
	timeout := flags.Duration("source-timeout", 2*time.Second, "Maximum overview source request duration")
	freshness := flags.Duration("freshness-timeout", 20*time.Second, "Maximum telemetry observation age")
	sampleAge := flags.Duration("max-sample-age", 15*time.Second, "Maximum Prometheus scrape age")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || len(validation.IsDNS1123Label(*namespace)) != 0 ||
		len(validation.IsDNS1123Subdomain(*process)) != 0 || *timeout <= 0 || *timeout > 10*time.Second || *freshness <= 0 || *sampleAge <= 0 {
		return fmt.Errorf("invalid scope, durations or positional arguments")
	}
	var config *rest.Config
	var err error
	if *kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	}
	if err != nil {
		return err
	}
	config.Timeout = *timeout
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{productionv1.AddToScheme, appsv1.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			return err
		}
	}
	reader, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	handler, err := factoryapi.New(factoryapi.Config{
		Namespace: *namespace, Process: *process, GrafanaURL: *grafana, Timeout: *timeout, Logger: logger,
		Cluster: factorysource.Kubernetes{Reader: reader},
		Telemetry: factorysource.Prometheus{URL: *prometheus, Client: &http.Client{Timeout: *timeout},
			Freshness: *freshness, MaxSampleAge: *sampleAge},
	})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("Factory API listening", "address", *listen, "namespace", *namespace, "process", *process)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
