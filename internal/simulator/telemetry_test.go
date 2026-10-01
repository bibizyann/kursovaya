package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/telemetry"
	"factory.local/factory-platform/internal/telemetryreceiver"
)

const (
	telemetryOven           = "reflow-oven-01"
	telemetryMeter          = "test-multimeter-01"
	telemetryProduct        = "board-a"
	telemetryOtherProduct   = "board-b"
	telemetryLegacyKind     = "tester"
	telemetryModePath       = "/api/v1/mode"
	telemetryOperationsPath = "/api/v1/operations"
)

type simulationClock struct{ nanos atomic.Int64 }

func (c *simulationClock) now() time.Time          { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *simulationClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func generationConfig(id string, seed int64) (Config, *simulationClock) {
	clock := &simulationClock{}
	clock.nanos.Store(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).UnixNano())
	return Config{
		EquipmentID: id, Product: telemetryProduct, Revision: "revision-a", InitialMode: ModeNormal,
		Latency: 3 * time.Second, Now: clock.now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Telemetry: &TelemetryConfig{Namespace: "factory", TechProcess: "line-one", Interval: time.Second, Seed: &seed},
	}, clock
}

func generatedServer(t *testing.T, id string) (*Server, *simulationClock) {
	t.Helper()
	config, clock := generationConfig(id, 42)
	s, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return s, clock
}

func generate(t *testing.T, s *Server) telemetry.Snapshot {
	t.Helper()
	snapshot, err := s.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("invalid generated snapshot: %v", err)
	}
	return snapshot
}

func TestTelemetryConfiguration(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"legacy equipment":   func(c *Config) { c.EquipmentID, c.Kind = "", telemetryLegacyKind },
		"seed required":      func(c *Config) { c.Telemetry.Seed = nil },
		"zero interval":      func(c *Config) { c.Telemetry.Interval = 0 },
		"negative interval":  func(c *Config) { c.Telemetry.Interval = -time.Second },
		"namespace required": func(c *Config) { c.Telemetry.Namespace = "" },
		"invalid process":    func(c *Config) { c.Telemetry.TechProcess = "INVALID" },
		"invalid revision":   func(c *Config) { c.Revision = "invalid revision" },
	} {
		t.Run(name, func(t *testing.T) {
			config, _ := generationConfig(telemetryOven, 0)
			change(&config)
			if _, err := New(config); err == nil {
				t.Fatal("accepted invalid telemetry configuration")
			}
		})
	}
	config, _ := generationConfig(telemetryOven, 0)
	s, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	*config.Telemetry.Seed = 5
	config.Telemetry.Namespace = "mutated"
	first := generate(t, s)
	if *first.SimulationSeed != 0 || first.Namespace != "factory" || first.Sequence != 1 {
		t.Fatal("configuration was aliased")
	}
	first.Readings["temperature_celsius"] = 999
	*first.SimulationSeed = 99
	if *generate(t, s).SimulationSeed != 0 {
		t.Fatal("snapshot changed generator seed")
	}
	config.Telemetry = nil
	disabled, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.Generate(); err == nil {
		t.Fatal("disabled generator produced telemetry")
	}
	if err := disabled.RunGenerator(context.Background(), nil); err == nil {
		t.Fatal("disabled runner started")
	}
}

func TestSeedReproducibilityAndCatalogReceiverCompatibility(t *testing.T) {
	for _, device := range equipment.Catalog() {
		for _, product := range []string{telemetryProduct, telemetryOtherProduct} {
			t.Run(device.ID+"/"+product, func(t *testing.T) {
				config, clock := generationConfig(device.ID, 42)
				config.Product = product
				left, err := New(config)
				if err != nil {
					t.Fatal(err)
				}
				right, err := New(config)
				if err != nil {
					t.Fatal(err)
				}
				receiver, err := telemetryreceiver.New(telemetryreceiver.Config{
					Expected: []telemetryreceiver.StreamKey{{Namespace: config.Telemetry.Namespace, TechProcess: config.Telemetry.TechProcess, EquipmentID: device.ID}}, Now: clock.now,
				})
				if err != nil {
					t.Fatal(err)
				}
				for i := uint64(1); i <= 20; i++ {
					clock.advance(time.Second)
					a, b := generate(t, left), generate(t, right)
					if a.SourceInstanceID == b.SourceInstanceID {
						t.Fatal("source IDs reused across simulator lifetimes")
					}
					b.SourceInstanceID = a.SourceInstanceID
					if !reflect.DeepEqual(a, b) {
						t.Fatalf("seed did not reproduce sample %d", i)
					}
					if a.Sequence != i || a.Counters.OperationsTotal != i || left.snapshot().Sequence != i {
						t.Fatal("snapshot and operation sequences diverged")
					}
					if accepted, err := receiver.Accept(a); !accepted || err != nil {
						t.Fatalf("receiver rejected generated snapshot: %v", err)
					}
					wantRejected := uint64(0)
					if device.Kind == "optical-inspector" || device.Kind == "multimeter" {
						wantRejected = i / 10
					}
					if a.Counters.RejectedOperationsTotal != wantRejected {
						t.Fatal("inspection rejection counters disagree with operations")
					}
				}
			})
		}
	}
	first, _ := generatedServer(t, telemetryOven)
	config, _ := generationConfig(telemetryOven, 43)
	second, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(generate(t, first).Readings, generate(t, second).Readings) {
		t.Fatal("different seeds produced identical readings")
	}
}

func TestHeartbeatSharesHTTPReadinessAndCounters(t *testing.T) {
	s, clock := generatedServer(t, telemetryMeter)
	// HTTP and automatic operations share the deterministic every-tenth rejection.
	for range 9 {
		assertStatusCode(t, s, "POST", telemetryOperationsPath, nil, 200)
	}
	snapshot := generate(t, s)
	if snapshot.Counters.OperationsTotal != 10 || snapshot.Counters.RejectedOperationsTotal != 1 || snapshot.Readings["voltage_volts"] <= snapshot.Readings["max_voltage_volts"] {
		t.Fatal("automatic operation did not continue the HTTP operation sequence")
	}
	for _, mode := range []Mode{ModeFault, ModeUncalibrated, ModeUnprepared} {
		body, err := json.Marshal(map[string]Mode{"mode": mode})
		if err != nil {
			t.Fatal(err)
		}
		assertStatusCode(t, s, "PUT", telemetryModePath, body, 200)
		updatedAt := s.snapshot().UpdatedAt
		for range 2 {
			clock.advance(time.Second)
			next := generate(t, s)
			status := s.snapshot()
			if next.State.Healthy != status.Healthy || next.State.Calibrated != status.Calibrated || next.State.SetupComplete != status.SetupComplete {
				t.Fatal("HTTP and telemetry readiness disagree")
			}
			if next.Counters != snapshot.Counters || !next.ObservedAt.After(snapshot.ObservedAt) || next.Sequence <= snapshot.Sequence {
				t.Fatal("unready heartbeat changed operations or stopped advancing")
			}
			if _, present := next.Readings["voltage_volts"]; present {
				t.Fatal("unready measurement presented as current")
			}
			if !status.UpdatedAt.Equal(updatedAt) {
				t.Fatal("heartbeat rewrote HTTP state update time")
			}
			assertStatusCode(t, s, "GET", "/healthz", nil, 200)
			assertStatusCode(t, s, "GET", "/readyz", nil, 503)
			snapshot = next
		}
	}
	assertStatusCode(t, s, "POST", telemetryOperationsPath, nil, 409)
	if generate(t, s).Counters.OperationErrorsTotal != 1 {
		t.Fatal("failed HTTP attempt was not counted")
	}
	assertStatusCode(t, s, "POST", "/api/v1/calibrate", nil, 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 503)
	assertStatusCode(t, s, "POST", "/api/v1/setup", nil, 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 200)
	if generate(t, s).Counters.OperationsTotal != 11 {
		t.Fatal("operations did not resume after preparation")
	}
}

func TestSlowGenerationDoesNotBlockHeartbeatAndAbortsOnStateChange(t *testing.T) {
	s, clock := generatedServer(t, telemetryOven)
	assertStatusCode(t, s, "PUT", telemetryModePath, []byte(`{"mode":"slow"}`), 200)
	first := generate(t, s)
	if first.Counters.OperationsTotal != 0 {
		t.Fatal("slow operation completed immediately")
	}
	if _, exists := first.Readings["last_operation_duration_seconds"]; exists {
		t.Fatal("duration exists before first completion")
	}
	clock.advance(time.Second)
	if generate(t, s).Counters.OperationsTotal != 0 {
		t.Fatal("slow operation ignored its deadline")
	}
	assertStatusCode(t, s, "PUT", telemetryModePath, []byte(`{"mode":"fault"}`), 200)
	clock.advance(5 * time.Second)
	failed := generate(t, s)
	if failed.Counters.OperationsTotal != 0 || failed.Counters.OperationErrorsTotal != 1 {
		t.Fatal("interrupted automatic operation was completed or counted twice")
	}
	assertStatusCode(t, s, "PUT", telemetryModePath, []byte(`{"mode":"slow"}`), 200)
	generate(t, s)
	clock.advance(3 * time.Second)
	completed := generate(t, s)
	if completed.Counters.OperationsTotal != 1 || completed.Readings["last_operation_duration_seconds"] != 3 || completed.Counters.OperationErrorsTotal != 1 {
		t.Fatal("slow operation did not complete at its deadline")
	}
}

func TestCancelledHTTPOperationIsAnError(t *testing.T) {
	s, _ := generatedServer(t, telemetryOven)
	assertStatusCode(t, s, "PUT", telemetryModePath, []byte(`{"mode":"slow"}`), 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifying := &notifyingContext{Context: ctx, started: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", telemetryOperationsPath, nil).WithContext(notifying))
	}()
	<-notifying.started
	cancel()
	<-done
	snapshot := generate(t, s)
	if snapshot.Counters.OperationErrorsTotal != 1 || snapshot.Counters.OperationsTotal != 0 {
		t.Fatal("cancelled HTTP operation changed completed count")
	}
}

func TestClockRegressionDoesNotAdvanceGeneration(t *testing.T) {
	s, clock := generatedServer(t, telemetryOven)
	first := generate(t, s)
	clock.advance(-time.Second)
	if _, err := s.Generate(); err == nil {
		t.Fatal("accepted a regressed simulation clock")
	}
	clock.advance(2 * time.Second)
	next := generate(t, s)
	if next.Sequence != first.Sequence+1 || next.Counters.OperationsTotal != first.Counters.OperationsTotal+1 {
		t.Fatal("rejected sample changed counters or sequence")
	}
}

func TestGeneratorSinkFailureDoesNotChangeReadinessOrReplay(t *testing.T) {
	s, clock := generatedServer(t, telemetryOven)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	samples := make(chan telemetry.Snapshot)
	done := make(chan error, 1)
	go func() {
		done <- s.runGenerator(ctx, ticks, func(_ context.Context, snapshot telemetry.Snapshot) error {
			assertStatusCode(t, s, "GET", "/readyz", nil, 200)
			samples <- snapshot
			return errors.New("sink unavailable")
		})
	}()
	first := <-samples
	clock.advance(time.Second)
	ticks <- clock.now()
	second := <-samples
	if second.Sequence != first.Sequence+1 || second.Counters.OperationsTotal != first.Counters.OperationsTotal+1 {
		t.Fatal("failed delivery was replayed or stopped generation")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunGeneratorCancellationAndSingleRunner(t *testing.T) {
	s, _ := generatedServer(t, telemetryOven)
	if err := s.RunGenerator(context.Background(), nil); err == nil {
		t.Fatal("nil sink accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.RunGenerator(ctx, func(ctx context.Context, _ telemetry.Snapshot) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-started
	if err := s.RunGenerator(ctx, func(context.Context, telemetry.Snapshot) error { return nil }); err == nil {
		t.Fatal("second runner accepted")
	}
	assertStatusCode(t, s, "GET", "/readyz", nil, 200)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.RunGenerator(ctx, func(context.Context, telemetry.Snapshot) error { t.Error("emitted after cancellation"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentGenerationAndHTTP(t *testing.T) {
	s, _ := generatedServer(t, telemetryOven)
	var workers sync.WaitGroup
	for range 40 {
		workers.Go(func() { generate(t, s) })
		workers.Go(func() { assertStatusCode(t, s, "POST", telemetryOperationsPath, nil, 200) })
		workers.Go(func() { assertStatusCode(t, s, "GET", "/api/v1/status", nil, 200) })
	}
	workers.Wait()
	if next := generate(t, s); next.Counters.OperationsTotal != 81 || next.Sequence != 41 {
		t.Fatal("concurrent calls lost operations or snapshot sequence")
	}
}
