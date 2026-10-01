package main

import (
	"flag"
	"io"
	"testing"
)

const testTelemetryInterval = "--telemetry-interval=1s"

func TestTelemetryFlagsRequireExplicitSeed(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		enabled bool
		invalid bool
	}{
		{name: "disabled by default"},
		{name: "explicitly disabled", args: []string{"--telemetry-interval=0s"}},
		{name: "seed without interval", args: []string{"--simulation-seed=42"}, invalid: true},
		{name: "missing seed", args: []string{testTelemetryInterval,
			"--telemetry-namespace=factory", "--telemetry-tech-process=line-one"}, invalid: true},
		{name: "missing scope", args: []string{testTelemetryInterval, "--simulation-seed=0"}, invalid: true},
		{name: "negative interval", args: []string{"--telemetry-interval=-1s"}, invalid: true},
		{name: "explicit zero seed", args: []string{testTelemetryInterval,
			"--telemetry-namespace=factory", "--telemetry-tech-process=line-one", "--simulation-seed=0"}, enabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet(test.name, flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			options := bindTelemetryFlags(flags)
			if err := flags.Parse(test.args); err != nil {
				t.Fatal(err)
			}
			config, err := options.config(flags)
			if (err != nil) != test.invalid || (config != nil) != test.enabled {
				t.Fatalf("configuration = %+v, error = %v", config, err)
			}
			if config != nil && (config.Seed == nil || *config.Seed != 0) {
				t.Fatal("explicit zero seed was lost")
			}
		})
	}
}
