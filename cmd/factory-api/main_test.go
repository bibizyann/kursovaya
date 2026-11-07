package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestRejectsInvalidOptionsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"--namespace=Bad"}, {"--process=../other"}, {"--source-timeout=0s"}, {"--source-timeout=20s"}, {"--freshness-timeout=-1s"}, {"--max-sample-age=0s"}, {"unexpected"}} {
		if err := run(context.Background(), args, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
