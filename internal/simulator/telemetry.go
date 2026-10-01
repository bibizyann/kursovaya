package simulator

import (
	"crypto/rand"
	"errors"
	"fmt"
	mathrand "math/rand"
	"time"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/telemetry"
)

// TelemetryConfig enables periodic simulation for one catalog device.
type TelemetryConfig struct {
	// Namespace scopes the technical process.
	Namespace string
	// TechProcess identifies the technical process.
	TechProcess string
	// Interval is the positive heartbeat and automatic operation scheduling interval.
	Interval time.Duration
	// Seed must be explicitly provided; zero is a valid seed.
	Seed *int64
}

type telemetryState struct {
	identity telemetry.Snapshot
	interval time.Duration
	random   *mathrand.Rand
	sequence uint64
	running  bool
	pending  *pendingOperation
}

type pendingOperation struct {
	startedAt time.Time
	finishAt  time.Time
}

func (s *Server) configureTelemetry(config *TelemetryConfig) error {
	if config == nil {
		return nil
	}
	if s.state.Equipment == nil {
		return errors.New("telemetry requires catalog equipment")
	}
	if config.Interval <= 0 || config.Seed == nil {
		return errors.New("telemetry requires a positive interval and an explicit simulation seed")
	}
	seed := *config.Seed
	identity := telemetry.Snapshot{
		SchemaVersion: telemetry.SchemaVersion,
		Namespace:     config.Namespace, TechProcess: config.TechProcess,
		EquipmentID: s.state.Equipment.ID, Workstation: s.state.Equipment.Workstation,
		SourceInstanceID: rand.Text(), SourceStartedAt: s.state.UpdatedAt,
		ObservedAt: s.state.UpdatedAt, Sequence: 1,
		Product: s.state.Product, RecipeID: s.state.Recipe.ID, Revision: s.state.Revision,
		SimulationSeed: &seed,
	}
	if err := identity.Validate(); err != nil {
		return fmt.Errorf("invalid telemetry configuration: %w", err)
	}
	s.telemetry = &telemetryState{
		identity: identity, interval: config.Interval,
		random: mathrand.New(mathrand.NewSource(seed)),
	}
	return nil
}

// Generate advances one automatic simulation cycle and returns a complete snapshot.
// Calls must follow the desired sampling schedule; it does not sleep or perform I/O.
// Reproducible values require the same seed, clock steps, and HTTP control/operation order.
func (s *Server) Generate() (telemetry.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.telemetry == nil {
		return telemetry.Snapshot{}, errors.New("telemetry is disabled")
	}
	now := s.now().UTC()
	if now.Before(s.telemetry.identity.ObservedAt) {
		return telemetry.Snapshot{}, errors.New("simulation clock moved backwards")
	}
	s.generateOperationLocked(now)
	s.telemetry.sequence++
	s.telemetry.identity.ObservedAt = now
	snapshot := s.telemetry.identity
	snapshot.Sequence = s.telemetry.sequence
	seed := *snapshot.SimulationSeed
	snapshot.SimulationSeed = &seed
	snapshot.State = telemetry.EquipmentState{
		Healthy: s.state.Healthy, Calibrated: s.state.Calibrated, SetupComplete: s.state.SetupComplete,
	}
	snapshot.Counters = s.counters
	snapshot.Readings = s.readingsLocked()
	return snapshot, nil
}

func ready(status Status) bool {
	return status.Healthy && status.Calibrated && status.SetupComplete
}

// completeOperationLocked is shared by automatic cycles and HTTP operations.
func (s *Server) completeOperationLocked(now time.Time, duration time.Duration) equipment.Result {
	s.state.Sequence++
	s.state.UpdatedAt = now
	if s.state.Recipe == nil {
		return equipment.Result{}
	}
	result := equipment.Execute(*s.state.Recipe, s.state.Sequence)
	s.counters.OperationsTotal++
	if !result.Accepted {
		s.counters.RejectedOperationsTotal++
	}
	s.lastDuration = max(0, duration.Seconds())
	if voltage, available := result.Readings["voltage_v"]; available {
		s.lastVoltage = voltage
	}
	return result
}

func (s *Server) invalidateOperationsLocked() {
	s.epoch++
	if s.telemetry != nil && s.telemetry.pending != nil {
		s.telemetry.pending = nil
		s.counters.OperationErrorsTotal++
	}
}

func (s *Server) readingsLocked() map[string]float64 {
	readings := make(map[string]float64)
	if s.counters.OperationsTotal > 0 {
		readings["last_operation_duration_seconds"] = s.lastDuration
	}
	profile := s.state.Recipe
	isReady := ready(s.state)
	switch s.state.Kind {
	case "reflow-oven":
		readings["target_temperature_celsius"] = profile.PeakTemperatureC
		readings["temperature_ready"] = 0
		if isReady {
			readings["temperature_ready"] = 1
			readings["temperature_celsius"] = profile.PeakTemperatureC + s.telemetry.random.Float64() - 0.5
		}
	case "power-supply":
		readings["target_voltage_volts"] = profile.VoltageV
		readings["current_limit_amperes"] = profile.CurrentLimitA
		readings["output_enabled"] = 0
		if isReady {
			readings["output_enabled"] = 1
			readings["voltage_volts"] = profile.VoltageV * (0.995 + 0.01*s.telemetry.random.Float64())
			readings["current_amperes"] = profile.CurrentLimitA * (0.3 + 0.1*s.telemetry.random.Float64())
		}
	case "multimeter":
		readings["min_voltage_volts"], readings["max_voltage_volts"] = profile.MinVoltageV, profile.MaxVoltageV
		if isReady && s.counters.OperationsTotal > 0 {
			readings["voltage_volts"] = s.lastVoltage
		}
	}
	return readings
}
