package main

import (
	"flag"
	"testing"
)

func TestMQTTRequiresTelemetry(t *testing.T) {
	flags := flag.NewFlagSet("mqtt", flag.ContinueOnError)
	options := bindMQTTFlags(flags)
	if publisher, err := options.publisher(false, nil); publisher != nil || err != nil {
		t.Fatal("MQTT enabled by default")
	}
	if err := flags.Parse([]string{"--mqtt-broker=tcp://127.0.0.1:1883"}); err != nil {
		t.Fatal(err)
	}
	if _, err := options.publisher(false, nil); err == nil {
		t.Fatal("MQTT accepted without telemetry")
	}
	if publisher, err := options.publisher(true, nil); publisher == nil || err != nil {
		t.Fatalf("publisher configuration: %v", err)
	}
}
