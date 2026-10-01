package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	fieldProduct  = "product"
	fieldRecipeID = "recipe_id"
	fieldRevision = "revision"
	fieldState    = "state"
	fieldCounters = "counters"
)

// Decode reads one bounded JSON snapshot and rejects missing or unknown fields.
func Decode(reader io.Reader) (Snapshot, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxMessageBytes+1))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot: %w", err)
	}
	if len(data) > MaxMessageBytes {
		return Snapshot{}, fmt.Errorf("snapshot exceeds %d bytes", MaxMessageBytes)
	}
	var snapshot Snapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Snapshot{}, fmt.Errorf("snapshot must contain exactly one JSON object")
	}
	if err := requiredFields(data); err != nil {
		return Snapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func requiredFields(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("decode snapshot fields: %w", err)
	}
	for _, name := range []string{
		"schema_version", "namespace", "tech_process", "equipment_id", "workstation",
		"source_instance_id", "source_started_at", "sequence", "observed_at",
		fieldProduct, fieldRecipeID, fieldRevision, fieldState, fieldCounters,
	} {
		if missing(object[name]) {
			return fmt.Errorf("%s is required and cannot be null", name)
		}
	}
	for _, group := range []struct {
		name   string
		fields []string
	}{
		{fieldState, []string{"healthy", "calibrated", "setup_complete"}},
		{fieldCounters, []string{"operations_total", "operation_errors_total", "rejected_operations_total"}},
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(object[group.name], &fields); err != nil {
			return fmt.Errorf("decode %s fields: %w", group.name, err)
		}
		for _, name := range group.fields {
			if missing(fields[name]) {
				return fmt.Errorf("%s.%s is required and cannot be null", group.name, name)
			}
		}
	}
	// A null numeric map value would otherwise silently decode as zero.
	var readings map[string]json.RawMessage
	if err := json.Unmarshal(objectOrNull(object["readings"]), &readings); err != nil {
		return fmt.Errorf("decode readings: %w", err)
	}
	for name, value := range readings {
		if missing(value) {
			return fmt.Errorf("reading %q cannot be null; omit unavailable readings", name)
		}
	}
	return nil
}

func missing(value json.RawMessage) bool {
	return len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func objectOrNull(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return value
}
