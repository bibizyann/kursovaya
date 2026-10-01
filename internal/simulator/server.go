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

// Package simulator provides a deterministic HTTP simulator for factory equipment.
package simulator

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"factory.local/factory-platform/internal/equipment"
)

// Mode controls simulated equipment behavior.
type Mode string

const (
	errorResponseKey = "error"

	// ModeNormal reports healthy and calibrated equipment.
	ModeNormal Mode = "normal"
	// ModeFault reports a hardware fault while keeping the process alive.
	ModeFault Mode = "fault"
	// ModeUncalibrated reports healthy equipment that requires calibration.
	ModeUncalibrated Mode = "uncalibrated"
	// ModeUnprepared waits for simulated operator setup confirmation.
	ModeUnprepared Mode = "unprepared"
	// ModeSlow delays measurements without failing readiness.
	ModeSlow Mode = "slow"
)

// Config defines one equipment simulator instance.
type Config struct {
	Kind         string
	EquipmentID  string
	Revision     string
	RequireSetup bool
	Product      string
	InitialMode  Mode
	Latency      time.Duration
	Logger       *slog.Logger
}

// Status is the public equipment state returned by the simulator.
type Status struct {
	Equipment     *equipment.Device `json:"equipment,omitempty"`
	Recipe        *equipment.Recipe `json:"recipe,omitempty"`
	Revision      string            `json:"revision,omitempty"`
	SetupComplete bool              `json:"setupComplete"`
	Kind          string            `json:"kind"`
	Product       string            `json:"product"`
	Mode          Mode              `json:"mode"`
	Healthy       bool              `json:"healthy"`
	Calibrated    bool              `json:"calibrated"`
	Sequence      uint64            `json:"sequence"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// Measurement represents one deterministic simulated production observation.
type Measurement struct {
	EquipmentKind string    `json:"equipmentKind"`
	Product       string    `json:"product"`
	Sequence      uint64    `json:"sequence"`
	Accepted      bool      `json:"accepted"`
	Value         float64   `json:"value"`
	ObservedAt    time.Time `json:"observedAt"`
}

// Server is a concurrency-safe equipment simulator and HTTP handler.
type Server struct {
	mu      sync.RWMutex
	epoch   uint64
	state   Status
	latency time.Duration
	logger  *slog.Logger
	mux     *http.ServeMux
}

// New creates a simulator in the configured mode.
func New(config Config) (*Server, error) {
	var device *equipment.Device
	var recipe *equipment.Recipe
	if config.EquipmentID != "" {
		if config.Kind != "" {
			return nil, errors.New("equipment and legacy kind are mutually exclusive")
		}
		d, err := equipment.Lookup(config.EquipmentID)
		if err != nil {
			return nil, err
		}
		r, err := equipment.Profile(config.EquipmentID, config.Product)
		if err != nil {
			return nil, err
		}
		device, recipe, config.Kind = &d, &r, d.Kind
	} else if config.Kind != "camera" && config.Kind != "tester" {
		return nil, errors.New("select a catalog equipment ID or legacy kind camera/tester")
	}
	if strings.TrimSpace(config.Revision) == "" {
		config.Revision = "standalone"
	}
	if strings.TrimSpace(config.Product) == "" {
		return nil, errors.New("product must not be empty")
	}
	if !validMode(config.InitialMode) {
		return nil, fmt.Errorf("unsupported initial mode %q", config.InitialMode)
	}
	if config.Latency < 0 {
		return nil, errors.New("latency must not be negative")
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	server := &Server{
		state: Status{
			Equipment:     device,
			Recipe:        recipe,
			Revision:      config.Revision,
			SetupComplete: !config.RequireSetup && config.InitialMode != ModeUnprepared,
			Kind:          config.Kind,
			Product:       config.Product,
			Mode:          config.InitialMode,
			Healthy:       config.InitialMode != ModeFault,
			Calibrated:    config.InitialMode != ModeFault && config.InitialMode != ModeUncalibrated,
			UpdatedAt:     time.Now().UTC(),
		},
		latency: config.Latency,
		logger:  config.Logger,
		mux:     http.NewServeMux(),
	}
	server.routes()
	return server, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mux.ServeHTTP(writer, request)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	s.mux.HandleFunc("PUT /api/v1/mode", s.handleMode)
	s.mux.HandleFunc("POST /api/v1/calibrate", s.handleCalibrate)
	s.mux.HandleFunc("POST /api/v1/measurements", s.handleMeasurement)
	s.mux.HandleFunc("POST /api/v1/operations", s.handleOperation)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) handleReady(writer http.ResponseWriter, _ *http.Request) {
	status := s.snapshot()
	if !status.Healthy || !status.Calibrated || !status.SetupComplete {
		writeJSON(writer, http.StatusServiceUnavailable, status)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (s *Server) handleStatus(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.snapshot())
}

func (s *Server) handleMode(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Mode Mode `json:"mode"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{errorResponseKey: "invalid JSON body"})
		return
	}
	if !validMode(input.Mode) {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{errorResponseKey: "unsupported mode"})
		return
	}

	s.mu.Lock()
	s.state.Mode = input.Mode
	s.state.Healthy = input.Mode != ModeFault
	s.state.Calibrated = input.Mode != ModeFault && input.Mode != ModeUncalibrated
	if input.Mode == ModeUnprepared {
		s.state.SetupComplete = false
	}
	s.epoch++
	s.state.UpdatedAt = time.Now().UTC()
	status := s.state
	s.mu.Unlock()

	s.logger.Info("Simulator mode changed", "kind", status.Kind, "product", status.Product, "mode", status.Mode)
	writeJSON(writer, http.StatusOK, status)
}

func (s *Server) handleCalibrate(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Healthy {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "equipment fault prevents calibration"})
		return
	}
	s.state.Calibrated = true
	s.epoch++
	s.state.UpdatedAt = time.Now().UTC()
	writeJSON(writer, http.StatusOK, s.state)
}

// Operation is an observation tied to a device, recipe, and deployed revision.
type Operation struct {
	equipment.Result
	EquipmentID string    `json:"equipmentId"`
	Workstation string    `json:"workstation"`
	Product     string    `json:"product"`
	RecipeID    string    `json:"recipeId"`
	Revision    string    `json:"revision"`
	Sequence    uint64    `json:"sequence"`
	ObservedAt  time.Time `json:"observedAt"`
}

func (s *Server) handleSetup(writer http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Healthy {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "equipment fault prevents setup"})
		return
	}
	s.state.SetupComplete = true
	s.state.UpdatedAt = time.Now().UTC()
	s.epoch++
	writeJSON(writer, http.StatusOK, s.state)
}

func (s *Server) handleOperation(writer http.ResponseWriter, request *http.Request) {
	if s.snapshot().Equipment == nil {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "operations require catalog equipment"})
		return
	}
	status, ok := s.observe(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, Operation{
		Result:      equipment.Execute(*status.Recipe, status.Sequence),
		EquipmentID: status.Equipment.ID, Workstation: status.Equipment.Workstation,
		Product: status.Product, RecipeID: status.Recipe.ID, Revision: status.Revision,
		Sequence: status.Sequence, ObservedAt: status.UpdatedAt,
	})
}

func (s *Server) handleMeasurement(writer http.ResponseWriter, request *http.Request) {
	if s.snapshot().Equipment != nil {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "use /api/v1/operations for catalog equipment"})
		return
	}
	status, ok := s.observe(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, Measurement{
		EquipmentKind: status.Kind, Product: status.Product, Sequence: status.Sequence,
		Accepted: status.Sequence%10 != 0, Value: 0.90 + float64(status.Sequence%10)/100,
		ObservedAt: status.UpdatedAt,
	})
}

func (s *Server) observe(writer http.ResponseWriter, request *http.Request) (Status, bool) {
	s.mu.RLock()
	status, epoch := s.state, s.epoch
	s.mu.RUnlock()
	if !status.Healthy || !status.Calibrated || !status.SetupComplete {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "equipment is not ready"})
		return Status{}, false
	}
	if status.Mode == ModeSlow && s.latency > 0 {
		timer := time.NewTimer(s.latency)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return Status{}, false
		case <-timer.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.Context().Err() != nil {
		return Status{}, false
	}
	if s.epoch != epoch {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "equipment state changed during operation"})
		return Status{}, false
	}
	s.state.Sequence++
	s.state.UpdatedAt = time.Now().UTC()
	return s.state, true
}

func (s *Server) snapshot() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeNormal, ModeFault, ModeUncalibrated, ModeSlow, ModeUnprepared:
		return true
	default:
		return false
	}
}

func writeJSON(writer http.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}
