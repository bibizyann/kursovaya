package telemetryreceiver

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"factory.local/factory-platform/internal/telemetry"
)

const (
	ovenID             = "reflow-oven-01"
	inspectorID        = "optical-inspector-01"
	otherProcess       = "line-two"
	newInstance        = "new-instance"
	temperatureReading = "temperature_celsius"
)

type testClock struct{ nanos atomic.Int64 }

func newClock() *testClock {
	c := &testClock{}
	c.nanos.Store(time.Date(2026, 10, 1, 12, 0, 10, 0, time.UTC).UnixNano())
	return c
}

func (c *testClock) now() time.Time          { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *testClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func sample(t *testing.T, id string) telemetry.Snapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "telemetry", "testdata", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := telemetry.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func keyOf(s telemetry.Snapshot) StreamKey {
	return StreamKey{Namespace: s.Namespace, TechProcess: s.TechProcess, EquipmentID: s.EquipmentID}
}

func receiverFor(t *testing.T, ids ...string) (*Receiver, *testClock) {
	t.Helper()
	c := newClock()
	keys := make([]StreamKey, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, keyOf(sample(t, id)))
	}
	r, err := New(Config{Expected: keys, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}

func accept(t *testing.T, r *Receiver, s telemetry.Snapshot) {
	t.Helper()
	accepted, err := r.Accept(s)
	if !accepted || err != nil {
		t.Fatalf("Accept = %t, %v", accepted, err)
	}
}

func TestConfiguration(t *testing.T) {
	key := keyOf(sample(t, ovenID))
	for name, change := range map[string]func(*Config){
		"empty inventory":    func(c *Config) { c.Expected = nil },
		"duplicate":          func(c *Config) { c.Expected = append(c.Expected, key) },
		"unknown equipment":  func(c *Config) { c.Expected[0].EquipmentID = "missing" },
		"invalid namespace":  func(c *Config) { c.Expected[0].Namespace = "/" },
		"long namespace":     func(c *Config) { c.Expected[0].Namespace = strings.Repeat("a", 64) },
		"empty process":      func(c *Config) { c.Expected[0].TechProcess = "" },
		"long process":       func(c *Config) { c.Expected[0].TechProcess = strings.Repeat("a", 254) },
		"invalid process":    func(c *Config) { c.Expected[0].TechProcess = "a..b" },
		"long process label": func(c *Config) { c.Expected[0].TechProcess = strings.Repeat("a", 64) },
		"negative expiry":    func(c *Config) { c.FreshnessTimeout = -1 },
		"negative skew":      func(c *Config) { c.MaxFutureSkew = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			config := Config{Expected: []StreamKey{key}}
			change(&config)
			if _, err := New(config); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	config := Config{Expected: []StreamKey{key}}
	r, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.Expected[0].EquipmentID = "changed"
	if r.Observations()[0].Key != key || r.freshness != DefaultFreshnessTimeout || r.future != DefaultMaxFutureSkew {
		t.Fatal("configuration was aliased or defaults were not applied")
	}
}

func TestExpectedDevicesAndScopedIdentity(t *testing.T) {
	s := sample(t, ovenID)
	first, second := keyOf(s), keyOf(s)
	second.TechProcess = otherProcess
	r, err := New(Config{Expected: []StreamKey{first, second}, Now: newClock().now})
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range r.Observations() {
		if observation.Fresh || observation.Snapshot != nil || observation.Workstation != s.Workstation {
			t.Fatal("expected devices must start unknown")
		}
	}
	accept(t, r, s)
	if r.Observations()[1].Snapshot != nil {
		t.Fatal("different processes shared a snapshot")
	}
	s.TechProcess = otherProcess
	accept(t, r, s)
	if !r.Observations()[1].Fresh {
		t.Fatal("second process did not receive its own snapshot")
	}
	s.Namespace = "another-namespace"
	if ok, err := r.Accept(s); ok || err == nil {
		t.Fatal("accepted unexpected namespace")
	}
}

func TestCopiesAndCompleteReplacement(t *testing.T) {
	r, _ := receiverFor(t, ovenID)
	s := sample(t, ovenID)
	accept(t, r, s)
	s.Readings[temperatureReading] = 999
	*s.SimulationSeed = 1
	view := r.Observations()[0]
	if view.Snapshot.Readings[temperatureReading] != 239.7 || *view.Snapshot.SimulationSeed != 42 {
		t.Fatal("caller changed stored snapshot")
	}
	view.Snapshot.Readings[temperatureReading] = 555
	*view.Snapshot.SimulationSeed = 2
	if r.Observations()[0].Snapshot.Readings[temperatureReading] != 239.7 || *r.Observations()[0].Snapshot.SimulationSeed != 42 {
		t.Fatal("view changed stored snapshot")
	}
	next := sample(t, ovenID)
	next.Sequence++
	next.Readings = nil
	accept(t, r, next)
	if len(r.Observations()[0].Snapshot.Readings) != 0 {
		t.Fatal("new snapshot inherited omitted readings")
	}
}

func TestIgnoredMessagesDoNotRefreshOrIncrement(t *testing.T) {
	r, clock := receiverFor(t, ovenID)
	s := sample(t, ovenID)
	accept(t, r, s)
	clock.advance(15 * time.Second) // The original observation is now exactly 20 seconds old.
	for _, sequence := range []uint64{s.Sequence, s.Sequence - 1} {
		next := s
		next.Sequence = sequence
		next.ObservedAt = clock.now()
		next.Counters.OperationsTotal++
		if ok, err := r.Accept(next); ok || err != nil {
			t.Fatalf("old sequence was not ignored: %t, %v", ok, err)
		}
	}
	view := r.Observations()[0]
	if view.Fresh || !reflect.DeepEqual(view.Snapshot, &s) {
		t.Fatal("duplicate refreshed or changed the snapshot")
	}
	next := s
	next.Sequence++
	next.ObservedAt = clock.now()
	accept(t, r, next)
	if !r.Observations()[0].Fresh {
		t.Fatal("new observation did not restore freshness")
	}
}

func TestConflictsLeaveSnapshotUnchanged(t *testing.T) {
	for name, change := range map[string]func(*telemetry.Snapshot){
		"invalid schema":        func(s *telemetry.Snapshot) { s.SchemaVersion = 0 },
		"product changed":       func(s *telemetry.Snapshot) { s.Product = "board-b" },
		"recipe changed":        func(s *telemetry.Snapshot) { s.RecipeID = "another-recipe" },
		"revision changed":      func(s *telemetry.Snapshot) { s.Revision = "revision-b" },
		"seed changed":          func(s *telemetry.Snapshot) { *s.SimulationSeed = 5 },
		"seed removed":          func(s *telemetry.Snapshot) { s.SimulationSeed = nil },
		"start changed":         func(s *telemetry.Snapshot) { s.SourceStartedAt = s.SourceStartedAt.Add(-time.Second) },
		"observation regressed": func(s *telemetry.Snapshot) { s.ObservedAt = s.ObservedAt.Add(-time.Second) },
		"operations decreased":  func(s *telemetry.Snapshot) { s.Counters.OperationsTotal-- },
		"errors decreased":      func(s *telemetry.Snapshot) { s.Counters.OperationErrorsTotal-- },
		"rejections decreased":  func(s *telemetry.Snapshot) { s.Counters.RejectedOperationsTotal-- },
		"equal start conflict":  func(s *telemetry.Snapshot) { s.SourceInstanceID = "different-instance" },
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := receiverFor(t, inspectorID)
			previous := sample(t, inspectorID)
			previous.Counters.OperationErrorsTotal = 2
			accept(t, r, previous)
			next := *cloneSnapshot(&previous)
			next.Sequence++
			change(&next)
			if ok, err := r.Accept(next); ok || err == nil {
				t.Fatalf("conflict accepted: %t, %v", ok, err)
			}
			if !reflect.DeepEqual(r.Observations()[0].Snapshot, &previous) {
				t.Fatal("rejected snapshot changed receiver state")
			}
		})
	}
}

func TestSourceRestartAndLateOldInstance(t *testing.T) {
	r, clock := receiverFor(t, ovenID)
	old := sample(t, ovenID)
	accept(t, r, old)
	next := sample(t, ovenID)
	next.SourceInstanceID = newInstance
	next.SourceStartedAt = clock.now()
	next.ObservedAt = clock.now()
	next.Sequence = 1
	next.Counters = telemetry.Counters{}
	next.Readings = nil
	next.Product, next.RecipeID, next.Revision = "board-b", "recipe-b", "revision-b"
	next.SimulationSeed = nil
	accept(t, r, next)
	old.Sequence = 999
	old.ObservedAt = clock.now()
	if ok, err := r.Accept(old); ok || err != nil {
		t.Fatalf("old source was not ignored: %t, %v", ok, err)
	}
	if !reflect.DeepEqual(r.Observations()[0].Snapshot, &next) {
		t.Fatal("old source replaced the new revision")
	}
	next.Sequence++
	accept(t, r, next) // Nil seeds are equal within this lifetime.
	// A new receiver has no durable watermark, but reconstructs state from fresh snapshots.
	restarted, _ := receiverFor(t, ovenID)
	if restarted.Observations()[0].Snapshot != nil {
		t.Fatal("receiver restart retained history")
	}
	accept(t, restarted, next)
}

func TestObservationTimeBoundaries(t *testing.T) {
	for _, test := range []struct {
		name     string
		offset   time.Duration
		accepted bool
	}{
		{"just before expiry", -20*time.Second + time.Nanosecond, true},
		{"at expiry", -20 * time.Second, false},
		{"old retained snapshot", -time.Hour, false},
		{"at future limit", 5 * time.Second, true},
		{"past future limit", 5*time.Second + time.Nanosecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, clock := receiverFor(t, ovenID)
			s := sample(t, ovenID)
			s.ObservedAt = clock.now().Add(test.offset)
			s.SourceStartedAt = s.ObservedAt.Add(-time.Second)
			ok, err := r.Accept(s)
			if ok != test.accepted || (err == nil) != test.accepted {
				t.Fatalf("Accept = %t, %v", ok, err)
			}
			if !test.accepted && r.Observations()[0].Snapshot != nil {
				t.Fatal("rejected first message was stored")
			}
		})
	}
}

func TestRejectedRestartDoesNotReplaceWatermark(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "future restart"
		if stale {
			name = "stale restart"
		}
		t.Run(name, func(t *testing.T) {
			r, clock := receiverFor(t, ovenID)
			previous := sample(t, ovenID)
			accept(t, r, previous)
			next := sample(t, ovenID)
			next.SourceInstanceID = newInstance
			next.SourceStartedAt = previous.SourceStartedAt.Add(time.Second)
			next.Sequence = 1
			if stale {
				clock.advance(DefaultFreshnessTimeout)
			} else {
				next.ObservedAt = clock.now().Add(DefaultMaxFutureSkew + time.Second)
			}
			if ok, err := r.Accept(next); ok || err == nil {
				t.Fatalf("invalid restart accepted: %t, %v", ok, err)
			}
			if !reflect.DeepEqual(r.Observations()[0].Snapshot, &previous) {
				t.Fatal("rejected restart changed the watermark")
			}
			next.ObservedAt = clock.now()
			accept(t, r, next)
			if !r.Observations()[0].Fresh {
				t.Fatal("valid retry did not restore current state")
			}
		})
	}
}

func TestConfiguredLimitsAndReceiverClockRegression(t *testing.T) {
	clock := newClock()
	s := sample(t, ovenID)
	s.ObservedAt = clock.now()
	r, err := New(Config{
		Expected: []StreamKey{keyOf(s)}, Now: clock.now,
		FreshnessTimeout: time.Second, MaxFutureSkew: time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	accept(t, r, s)
	clock.advance(-time.Second)
	if r.Observations()[0].Fresh {
		t.Fatal("clock regression hid future observation")
	}
	clock.advance(2 * time.Second)
	if r.Observations()[0].Fresh {
		t.Fatal("configured expiry was ignored")
	}
}

func TestConcurrentAcceptAndScrape(t *testing.T) {
	r, clock := receiverFor(t, ovenID)
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(r.Collector())
	s := sample(t, ovenID)
	s.ObservedAt = clock.now()
	var workers sync.WaitGroup
	for i := 1; i <= 50; i++ {
		workers.Go(func() {
			next := s
			next.Sequence = uint64(i)
			next.Counters.OperationsTotal = uint64(i)
			if _, err := r.Accept(next); err != nil {
				t.Error(err)
			}
			if _, err := registry.Gather(); err != nil {
				t.Error(err)
			}
			_ = r.Observations()
		})
	}
	workers.Wait()
	if r.Observations()[0].Snapshot.Sequence != 50 {
		t.Fatal("concurrent updates lost the latest snapshot")
	}
}
