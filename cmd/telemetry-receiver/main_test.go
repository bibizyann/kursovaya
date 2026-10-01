package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

const testExpected = "--expected=factory-demo/line-one/reflow-oven-01"

func TestExpectedStreamParsing(t *testing.T) {
	key, err := parseExpected("factory-demo/line-one/reflow-oven-01")
	if err != nil || key.Namespace != "factory-demo" || key.TechProcess != "line-one" ||
		key.EquipmentID != "reflow-oven-01" {
		t.Fatalf("parsed key: %+v, %v", key, err)
	}
	for _, value := range []string{"", "a/b", "a/b/c/d", "/b/c", "a//c", "a/b/"} {
		if _, err := parseExpected(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestReceiverRejectsInvalidConfiguration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, args := range [][]string{
		nil,
		{"--expected=invalid"},
		{"--expected=factory-demo/line-one/unknown"},
		{testExpected, "--mqtt-broker=invalid"},
		{testExpected, "--freshness-timeout=-1s"},
		{testExpected, "unexpected"},
	} {
		if err := run(context.Background(), args, logger); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
