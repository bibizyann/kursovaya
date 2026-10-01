package simulator

import (
	"context"
	"errors"
	"time"

	"factory.local/factory-platform/internal/telemetry"
)

func (s *Server) generateOperationLocked(now time.Time) {
	if !ready(s.state) {
		return
	}
	if pending := s.telemetry.pending; pending != nil {
		if !now.Before(pending.finishAt) {
			s.completeOperationLocked(now, pending.finishAt.Sub(pending.startedAt))
			s.telemetry.pending = nil
		}
		return
	}
	if s.state.Mode == ModeSlow && s.latency > 0 {
		s.telemetry.pending = &pendingOperation{startedAt: now, finishAt: now.Add(s.latency)}
		return
	}
	// This duration is synthetic, not the elapsed time of a physical machine cycle.
	duration := time.Duration(float64(s.telemetry.interval) * (0.05 + 0.15*s.telemetry.random.Float64()))
	s.completeOperationLocked(now, duration)
}

// RunGenerator emits an immediate snapshot, then samples at the configured interval.
// Only one runner is allowed. The sink must return promptly and honor cancellation.
// Sink failures are logged and discarded; they never change equipment readiness.
func (s *Server) RunGenerator(ctx context.Context, emit func(context.Context, telemetry.Snapshot) error) error {
	s.mu.Lock()
	if s.telemetry == nil || s.telemetry.running || emit == nil {
		s.mu.Unlock()
		return errors.New("generator requires enabled telemetry, a sink, and no active runner")
	}
	s.telemetry.running = true
	interval := s.telemetry.interval
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.telemetry.running = false
		s.mu.Unlock()
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	return s.runGenerator(ctx, ticker.C, emit)
}

func (s *Server) runGenerator(ctx context.Context, ticks <-chan time.Time, emit func(context.Context, telemetry.Snapshot) error) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		snapshot, err := s.Generate()
		if err != nil {
			return err
		}
		// Generate releases the state lock before any potentially slow external I/O.
		if err := emit(ctx, snapshot); err != nil && ctx.Err() == nil {
			s.logger.Warn("Telemetry snapshot delivery failed", "sequence", snapshot.Sequence, "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case _, open := <-ticks:
			if !open {
				return nil
			}
		}
	}
}
