package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"factory.local/factory-platform/internal/telemetrymqtt"
	"factory.local/factory-platform/internal/telemetryreceiver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], logger); err != nil {
		logger.Error("Telemetry receiver stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	flags := flag.NewFlagSet("telemetry-receiver", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:9091", "HTTP address for /metrics and /healthz")
	broker := flags.String("mqtt-broker", "tcp://127.0.0.1:1883", "MQTT broker URL")
	timeout := flags.Duration("mqtt-timeout", 3*time.Second, "MQTT network operation timeout")
	retryInterval := flags.Duration("mqtt-retry-interval", time.Second, "MQTT reconnect interval")
	freshness := flags.Duration("freshness-timeout", telemetryreceiver.DefaultFreshnessTimeout, "maximum snapshot age")
	skew := flags.Duration("max-future-skew", telemetryreceiver.DefaultMaxFutureSkew, "allowed source clock lead")
	var expected []telemetryreceiver.StreamKey
	flags.Func("expected", "expected namespace/process/equipment-id; repeat for each stream", func(value string) error {
		key, err := parseExpected(value)
		if err != nil {
			return err
		}
		expected = append(expected, key)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	receiver, err := telemetryreceiver.New(telemetryreceiver.Config{
		Expected: expected, FreshnessTimeout: *freshness, MaxFutureSkew: *skew,
	})
	if err != nil {
		return err
	}
	subscriber, err := telemetrymqtt.NewSubscriber(telemetrymqtt.Config{
		BrokerURL: *broker, Timeout: *timeout, RetryInterval: *retryInterval, Logger: logger,
		Username: os.Getenv("FACTORY_MQTT_USERNAME"), Password: os.Getenv("FACTORY_MQTT_PASSWORD"),
	}, receiver)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", receiver.MetricsHandler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	httpDone := make(chan error, 1)
	subscriberDone := make(chan error, 1)
	go func() { httpDone <- server.Serve(listener) }()
	go func() { subscriberDone <- subscriber.Run(ctx) }()
	logger.Info("Telemetry receiver listening", "address", listener.Addr().String(), "streams", len(expected))
	select {
	case <-ctx.Done():
	case err = <-httpDone:
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		_ = server.Close()
		err = shutdownErr
	}
	if subscriberErr := <-subscriberDone; subscriberErr != nil {
		return subscriberErr
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func parseExpected(value string) (telemetryreceiver.StreamKey, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return telemetryreceiver.StreamKey{}, fmt.Errorf("expected stream must be namespace/process/equipment-id: %q", value)
	}
	return telemetryreceiver.StreamKey{Namespace: parts[0], TechProcess: parts[1], EquipmentID: parts[2]}, nil
}
