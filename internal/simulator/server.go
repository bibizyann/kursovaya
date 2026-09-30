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
	// ModeSlow delays measurements without failing readiness.
	ModeSlow Mode = "slow"
)

// Config defines one equipment simulator instance.
type Config struct {
	Kind        string
	Product     string
	InitialMode Mode
	Latency     time.Duration
	Logger      *slog.Logger
}

// Status is the public equipment state returned by the simulator.
type Status struct {
	Kind       string    `json:"kind"`
	Product    string    `json:"product"`
	Mode       Mode      `json:"mode"`
	Healthy    bool      `json:"healthy"`
	Calibrated bool      `json:"calibrated"`
	Sequence   uint64    `json:"sequence"`
	UpdatedAt  time.Time `json:"updatedAt"`
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
	state   Status
	latency time.Duration
	logger  *slog.Logger
	mux     *http.ServeMux
}

// New creates a simulator in the configured mode.
func New(config Config) (*Server, error) {
	if strings.TrimSpace(config.Kind) == "" {
		return nil, errors.New("equipment kind must not be empty")
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
			Kind:       config.Kind,
			Product:    config.Product,
			Mode:       config.InitialMode,
			Healthy:    config.InitialMode != ModeFault,
			Calibrated: config.InitialMode == ModeNormal || config.InitialMode == ModeSlow,
			UpdatedAt:  time.Now().UTC(),
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
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) handleReady(writer http.ResponseWriter, _ *http.Request) {
	status := s.snapshot()
	if !status.Healthy || !status.Calibrated {
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
	s.state.Calibrated = input.Mode == ModeNormal || input.Mode == ModeSlow
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
	s.state.UpdatedAt = time.Now().UTC()
	writeJSON(writer, http.StatusOK, s.state)
}

func (s *Server) handleMeasurement(writer http.ResponseWriter, request *http.Request) {
	status := s.snapshot()
	if !status.Healthy || !status.Calibrated {
		writeJSON(writer, http.StatusConflict, map[string]string{errorResponseKey: "equipment is not ready"})
		return
	}
	if status.Mode == ModeSlow && s.latency > 0 {
		timer := time.NewTimer(s.latency)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return
		case <-timer.C:
		}
	}

	s.mu.Lock()
	s.state.Sequence++
	s.state.UpdatedAt = time.Now().UTC()
	status = s.state
	s.mu.Unlock()

	measurement := Measurement{
		EquipmentKind: status.Kind,
		Product:       status.Product,
		Sequence:      status.Sequence,
		Accepted:      status.Sequence%10 != 0,
		Value:         0.90 + float64(status.Sequence%10)/100,
		ObservedAt:    status.UpdatedAt,
	}
	writeJSON(writer, http.StatusOK, measurement)
}

func (s *Server) snapshot() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeNormal, ModeFault, ModeUncalibrated, ModeSlow:
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
