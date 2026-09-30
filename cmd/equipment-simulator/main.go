/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"factory.local/factory-platform/internal/simulator"
)

func main() {
	var listenAddress string
	var equipmentKind string
	var product string
	var initialMode string
	var latency time.Duration

	flag.StringVar(&listenAddress, "listen", ":8080", "HTTP listen address")
	flag.StringVar(&equipmentKind, "kind", "camera", "simulated equipment kind")
	flag.StringVar(&product, "product", "board-a", "product profile")
	flag.StringVar(&initialMode, "mode", string(simulator.ModeNormal),
		"initial mode: normal, fault, uncalibrated, or slow")
	flag.DurationVar(&latency, "latency", 2*time.Second, "measurement latency in slow mode")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler, err := simulator.New(simulator.Config{
		Kind:        equipmentKind,
		Product:     product,
		InitialMode: simulator.Mode(initialMode),
		Latency:     latency,
		Logger:      logger,
	})
	if err != nil {
		logger.Error("Invalid simulator configuration", "error", err)
		os.Exit(2)
	}

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Simulator shutdown failed", "error", err)
		}
	}()

	logger.Info("Starting equipment simulator", "address", listenAddress, "kind", equipmentKind,
		"product", product, "mode", initialMode)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("Simulator server failed", "error", err)
		os.Exit(1)
	}
}
