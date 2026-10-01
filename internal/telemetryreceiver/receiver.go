// Package telemetryreceiver stores current equipment observations for monitoring.
package telemetryreceiver

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
	"time"

	"factory.local/factory-platform/internal/equipment"
	"factory.local/factory-platform/internal/telemetry"
)

// DefaultFreshnessTimeout is the maximum observation age on the local stand.
const DefaultFreshnessTimeout = 20 * time.Second

// DefaultMaxFutureSkew is the permitted source clock lead on the local stand.
const DefaultMaxFutureSkew = 5 * time.Second

// StreamKey identifies one expected device independently of its publisher lifetime.
type StreamKey struct {
	// Namespace scopes the technical process.
	Namespace string
	// TechProcess identifies the technical process.
	TechProcess string
	// EquipmentID identifies the catalog device.
	EquipmentID string
}

// Config defines the fixed inventory and observation time policy.
type Config struct {
	// Expected lists unique streams; at least one stream is required.
	Expected []StreamKey
	// FreshnessTimeout defaults to DefaultFreshnessTimeout when zero.
	FreshnessTimeout time.Duration
	// MaxFutureSkew defaults to DefaultMaxFutureSkew when zero.
	MaxFutureSkew time.Duration
	// Now defaults to time.Now and must be safe for concurrent calls.
	Now func() time.Time
}

// Observation is an isolated view of the last accepted snapshot for an expected device.
type Observation struct {
	// Key identifies the configured stream.
	Key StreamKey
	// Workstation is the logical workstation from the catalog.
	Workstation string
	// Fresh reports whether Snapshot is usable at the time of this view.
	Fresh bool
	// Snapshot is nil until the first accepted observation arrives.
	Snapshot *telemetry.Snapshot
}

// Receiver retains at most one snapshot for each configured stream.
type Receiver struct {
	mu        sync.RWMutex
	order     []StreamKey
	entries   map[StreamKey]Observation
	freshness time.Duration
	future    time.Duration
	now       func() time.Time
}

var labelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// New creates an empty receiver; missing devices are immediately observable.
func New(config Config) (*Receiver, error) {
	if len(config.Expected) == 0 {
		return nil, fmt.Errorf("expected inventory must not be empty")
	}
	if config.FreshnessTimeout < 0 || config.MaxFutureSkew < 0 {
		return nil, fmt.Errorf("time limits must not be negative")
	}
	if config.FreshnessTimeout == 0 {
		config.FreshnessTimeout = DefaultFreshnessTimeout
	}
	if config.MaxFutureSkew == 0 {
		config.MaxFutureSkew = DefaultMaxFutureSkew
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	r := &Receiver{
		order:     append([]StreamKey(nil), config.Expected...),
		entries:   make(map[StreamKey]Observation, len(config.Expected)),
		freshness: config.FreshnessTimeout, future: config.MaxFutureSkew, now: config.Now,
	}
	for _, key := range r.order {
		if err := validateScope(key); err != nil {
			return nil, err
		}
		device, err := equipment.Lookup(key.EquipmentID)
		if err != nil {
			return nil, err
		}
		if _, exists := r.entries[key]; exists {
			return nil, fmt.Errorf("duplicate expected stream: %+v", key)
		}
		r.entries[key] = Observation{Key: key, Workstation: device.Workstation}
	}
	return r, nil
}

func validateScope(key StreamKey) error {
	if len(key.Namespace) > 63 || !labelPattern.MatchString(key.Namespace) {
		return fmt.Errorf("invalid expected namespace %q", key.Namespace)
	}
	if len(key.TechProcess) == 0 || len(key.TechProcess) > 253 {
		return fmt.Errorf("invalid expected tech_process %q", key.TechProcess)
	}
	for label := range strings.SplitSeq(key.TechProcess, ".") {
		if len(label) > 63 || !labelPattern.MatchString(label) {
			return fmt.Errorf("invalid expected tech_process %q", key.TechProcess)
		}
	}
	return nil
}

// Accept validates and copies a candidate snapshot. False with no error means an
// older sequence or source was ignored. No rejected message changes stored state.
// Callers must decode untrusted JSON through telemetry.Decode before calling Accept.
func (r *Receiver) Accept(snapshot telemetry.Snapshot) (bool, error) {
	if err := snapshot.Validate(); err != nil {
		return false, fmt.Errorf("invalid snapshot: %w", err)
	}
	key := StreamKey{Namespace: snapshot.Namespace, TechProcess: snapshot.TechProcess, EquipmentID: snapshot.EquipmentID}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.entries[key]
	if !exists {
		return false, fmt.Errorf("unexpected stream: %+v", key)
	}
	if entry.Snapshot != nil {
		accept, err := advances(*entry.Snapshot, snapshot)
		if !accept || err != nil {
			return false, err
		}
	}
	if err := r.validateTime(snapshot.ObservedAt, r.now()); err != nil {
		return false, err
	}
	entry.Snapshot = cloneSnapshot(&snapshot)
	r.entries[key] = entry
	return true, nil
}

func advances(previous, next telemetry.Snapshot) (bool, error) {
	if previous.SourceInstanceID != next.SourceInstanceID {
		switch {
		case next.SourceStartedAt.Before(previous.SourceStartedAt):
			return false, nil
		case next.SourceStartedAt.Equal(previous.SourceStartedAt):
			return false, fmt.Errorf("different source instances have the same start time")
		default:
			return true, nil
		}
	}
	if !previous.SourceStartedAt.Equal(next.SourceStartedAt) || previous.Product != next.Product ||
		previous.RecipeID != next.RecipeID || previous.Revision != next.Revision ||
		!sameSeed(previous.SimulationSeed, next.SimulationSeed) {
		return false, fmt.Errorf("configuration changed within a source instance")
	}
	if next.Sequence <= previous.Sequence {
		return false, nil
	}
	if next.ObservedAt.Before(previous.ObservedAt) {
		return false, fmt.Errorf("observed_at decreased within a source instance")
	}
	if next.Counters.OperationsTotal < previous.Counters.OperationsTotal ||
		next.Counters.OperationErrorsTotal < previous.Counters.OperationErrorsTotal ||
		next.Counters.RejectedOperationsTotal < previous.Counters.RejectedOperationsTotal {
		return false, fmt.Errorf("counters decreased within a source instance")
	}
	return true, nil
}

func sameSeed(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (r *Receiver) validateTime(observedAt, now time.Time) error {
	if observedAt.After(now.Add(r.future)) {
		return fmt.Errorf("observed_at exceeds allowed future clock skew")
	}
	if now.Sub(observedAt) >= r.freshness {
		return fmt.Errorf("observation is stale")
	}
	return nil
}

// Observations returns independent snapshots in inventory order with current freshness.
func (r *Receiver) Observations() []Observation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now()
	observations := make([]Observation, 0, len(r.order))
	for _, key := range r.order {
		entry := r.entries[key]
		entry.Snapshot = cloneSnapshot(entry.Snapshot)
		entry.Fresh = entry.Snapshot != nil && r.validateTime(entry.Snapshot.ObservedAt, now) == nil
		observations = append(observations, entry)
	}
	return observations
}

func cloneSnapshot(snapshot *telemetry.Snapshot) *telemetry.Snapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.Readings = maps.Clone(snapshot.Readings)
	if snapshot.SimulationSeed != nil {
		seed := *snapshot.SimulationSeed
		copy.SimulationSeed = &seed
	}
	return &copy
}
