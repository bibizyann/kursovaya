package telemetry

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"factory.local/factory-platform/internal/equipment"
)

var (
	dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	sourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
)

// Validate checks one snapshot without making ordering or freshness decisions.
func (s Snapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", s.SchemaVersion)
	}
	if err := s.validateIdentity(); err != nil {
		return err
	}
	if s.Sequence == 0 {
		return fmt.Errorf("sequence must be greater than zero")
	}
	if s.SourceStartedAt.IsZero() || s.ObservedAt.IsZero() {
		return fmt.Errorf("source_started_at and observed_at are required")
	}
	if s.SourceStartedAt.Year() < 1 || s.SourceStartedAt.Year() > 9999 ||
		s.ObservedAt.Year() < 1 || s.ObservedAt.Year() > 9999 {
		return fmt.Errorf("timestamps must be representable as RFC3339")
	}
	if s.ObservedAt.Before(s.SourceStartedAt) {
		return fmt.Errorf("observed_at precedes source_started_at")
	}
	if s.Counters.RejectedOperationsTotal > s.Counters.OperationsTotal {
		return fmt.Errorf("rejected_operations_total exceeds operations_total")
	}
	device, err := equipment.Lookup(s.EquipmentID)
	if err != nil {
		return err
	}
	if s.Workstation != device.Workstation {
		return fmt.Errorf("workstation does not match equipment %q", s.EquipmentID)
	}
	if s.Counters.RejectedOperationsTotal != 0 && device.Kind != "optical-inspector" && device.Kind != "multimeter" {
		return fmt.Errorf("equipment %q does not report rejected inspections", s.EquipmentID)
	}
	return s.validateReadings()
}

func (s Snapshot) validateIdentity() error {
	if !validDNSName(s.Namespace, 63, false) {
		return fmt.Errorf("namespace must be a DNS label of at most 63 characters")
	}
	if !validDNSName(s.TechProcess, 253, true) {
		return fmt.Errorf("tech_process must be a DNS subdomain of at most 253 characters")
	}
	if len(s.SourceInstanceID) > 128 || !sourceID.MatchString(s.SourceInstanceID) {
		return fmt.Errorf("source_instance_id must be a token of at most 128 characters")
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{fieldProduct, s.Product, 128},
		{fieldRecipeID, s.RecipeID, 256},
		{fieldRevision, s.Revision, 128},
	} {
		if len(field.value) == 0 || len(field.value) > field.limit ||
			strings.ContainsFunc(field.value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return fmt.Errorf("%s must be nonempty, contain no whitespace or controls, and fit in %d bytes", field.name, field.limit)
		}
	}
	return nil
}

func validDNSName(value string, maxLength int, allowDots bool) bool {
	if len(value) == 0 || len(value) > maxLength {
		return false
	}
	if !allowDots {
		return dnsLabel.MatchString(value)
	}
	for label := range strings.SplitSeq(value, ".") {
		if len(label) > 63 || !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (s Snapshot) validateReadings() error {
	if _, ok := s.Readings["last_operation_duration_seconds"]; ok && s.Counters.OperationsTotal == 0 {
		return fmt.Errorf("last_operation_duration_seconds requires a completed operation")
	}
	definitions, err := ReadingDefinitions(s.EquipmentID)
	if err != nil {
		return err
	}
	allowed := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		allowed[definition.Name] = definition.Unit
	}
	for name, value := range s.Readings {
		unit, ok := allowed[name]
		if !ok {
			return fmt.Errorf("unsupported reading %q for equipment %q", name, s.EquipmentID)
		}
		if !validReading(unit, value) {
			return fmt.Errorf("invalid %s value for reading %q", unit, name)
		}
	}
	minimum, hasMinimum := s.Readings["min_voltage_volts"]
	maximum, hasMaximum := s.Readings["max_voltage_volts"]
	if hasMinimum != hasMaximum || (hasMinimum && minimum > maximum) {
		return fmt.Errorf("voltage limits must be an ordered pair")
	}
	return nil
}

func validReading(unit string, value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	switch unit {
	case unitSeconds:
		return value >= 0
	case unitCelsius:
		return value >= -273.15
	case unitBoolean:
		return value == 0 || value == 1
	default:
		return true
	}
}
