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
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/simulator"
	"factory.local/factory-platform/internal/telemetry"
)

func main() {
	var listenAddress string
	var equipmentKind string
	var product string
	var equipmentID, revision string
	var listEquipment, requireSetup bool
	var initialMode string
	var latency time.Duration

	flag.StringVar(&listenAddress, "listen", ":8080", "HTTP listen address")
	flag.StringVar(&equipmentKind, "kind", "", "legacy simulator kind: camera or tester")
	flag.StringVar(&equipmentID, "equipment", "", "catalog equipment ID (default optical-inspector-01)")
	flag.StringVar(&revision, "revision", os.Getenv("FACTORY_REVISION"), "deployed process revision")
	flag.BoolVar(&listEquipment, "list-equipment", false, "print equipment catalog and exit")
	flag.BoolVar(&requireSetup, "require-setup", false, "wait for simulated operator setup confirmation")
	flag.StringVar(&product, "product", "board-a", "product profile")
	flag.StringVar(&initialMode, "mode", string(simulator.ModeNormal),
		"initial mode: normal, fault, uncalibrated, unprepared, or slow")
	flag.DurationVar(&latency, "latency", 2*time.Second, "measurement latency in slow mode")
	telemetryOptions := bindTelemetryFlags(flag.CommandLine)
	flag.Parse()
	if listEquipment {
		if err := json.NewEncoder(os.Stdout).Encode(equipment.Catalog()); err != nil {
			slog.Error("Catalog output failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if equipmentID == "" && equipmentKind == "" {
		equipmentID = "optical-inspector-01"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	telemetryConfig, err := telemetryOptions.config(flag.CommandLine)
	if err != nil {
		logger.Error("Invalid telemetry configuration", "error", err)
		os.Exit(2)
	}
	handler, err := simulator.New(simulator.Config{
		Kind:         equipmentKind,
		EquipmentID:  equipmentID,
		Revision:     revision,
		RequireSetup: requireSetup,
		Product:      product,
		InitialMode:  simulator.Mode(initialMode),
		Latency:      latency,
		Logger:       logger,
		Telemetry:    telemetryConfig,
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
	generatorDone := make(chan struct{})
	if telemetryConfig == nil {
		close(generatorDone)
	} else {
		go func() {
			defer close(generatorDone)
			err := handler.RunGenerator(ctx, func(_ context.Context, snapshot telemetry.Snapshot) error {
				logger.Info("Generated telemetry snapshot", "snapshot", snapshot)
				return nil
			})
			if err != nil {
				logger.Error("Telemetry generator stopped", "error", err)
			}
		}()
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Simulator shutdown failed", "error", err)
		}
	}()

	logger.Info("Starting equipment simulator", "address", listenAddress, "kind", equipmentKind,
		"equipment", equipmentID, "product", product, "mode", initialMode)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("Simulator server failed", "error", err)
		os.Exit(1)
	}
	stop()
	<-generatorDone
	<-shutdownDone
}
