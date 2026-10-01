package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"
	"time"

	"factory.local/factory-platform/internal/telemetrymqtt"
)

type mqttFlags struct {
	broker        string
	timeout       time.Duration
	retryInterval time.Duration
}

func bindMQTTFlags(flags *flag.FlagSet) *mqttFlags {
	options := &mqttFlags{}
	flags.StringVar(&options.broker, "mqtt-broker", "", "MQTT URL; empty keeps JSON-log-only telemetry")
	flags.DurationVar(&options.timeout, "mqtt-timeout", 3*time.Second, "MQTT network operation timeout")
	flags.DurationVar(&options.retryInterval, "mqtt-retry-interval", time.Second, "MQTT reconnect interval")
	return options
}

func (options *mqttFlags) publisher(telemetryEnabled bool, logger *slog.Logger) (*telemetrymqtt.Publisher, error) {
	if options.broker == "" {
		return nil, nil
	}
	if !telemetryEnabled {
		return nil, errors.New("--mqtt-broker requires enabled telemetry")
	}
	return telemetrymqtt.NewPublisher(telemetrymqtt.Config{
		BrokerURL: options.broker, Timeout: options.timeout, RetryInterval: options.retryInterval,
		Username: os.Getenv("FACTORY_MQTT_USERNAME"), Password: os.Getenv("FACTORY_MQTT_PASSWORD"), Logger: logger,
	})
}
