package main

import (
	"errors"
	"flag"
	"time"

	"factory.local/factory-platform/internal/simulator"
)

type telemetryFlags struct {
	interval  time.Duration
	namespace string
	process   string
	seed      int64
}

func bindTelemetryFlags(flags *flag.FlagSet) *telemetryFlags {
	options := &telemetryFlags{}
	flags.DurationVar(&options.interval, "telemetry-interval", 0,
		"automatic simulation and heartbeat interval; zero disables telemetry")
	flags.StringVar(&options.namespace, "telemetry-namespace", "", "namespace of the observed technical process")
	flags.StringVar(&options.process, "telemetry-tech-process", "", "name of the observed technical process")
	flags.Int64Var(&options.seed, "simulation-seed", 0, "explicit random seed required for telemetry, including zero")
	return options
}

func (options *telemetryFlags) config(flags *flag.FlagSet) (*simulator.TelemetryConfig, error) {
	seedSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "simulation-seed" {
			seedSet = true
		}
	})
	if options.interval == 0 && !seedSet && options.namespace == "" && options.process == "" {
		return nil, nil
	}
	if options.interval <= 0 || !seedSet || options.namespace == "" || options.process == "" {
		return nil, errors.New("telemetry requires a positive --telemetry-interval, --simulation-seed, " +
			"--telemetry-namespace, and --telemetry-tech-process")
	}
	return &simulator.TelemetryConfig{
		Namespace: options.namespace, TechProcess: options.process, Interval: options.interval, Seed: &options.seed,
	}, nil
}
