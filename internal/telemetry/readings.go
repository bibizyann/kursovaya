package telemetry

import "factory.local/factory-platform/internal/equipment"

const (
	unitSeconds = "seconds"
	unitCelsius = "celsius"
	unitVolts   = "volts"
	unitAmperes = "amperes"
	unitBoolean = "boolean"
)

// ReadingDefinition assigns a fixed unit to an allowed gauge name.
type ReadingDefinition struct {
	// Name is the wire key and future exporter metric suffix.
	Name string
	// Unit is seconds, celsius, volts, amperes, or boolean (zero or one).
	Unit string
}

// ReadingDefinitions returns an independent list of gauges supported by a device.
func ReadingDefinitions(equipmentID string) ([]ReadingDefinition, error) {
	device, err := equipment.Lookup(equipmentID)
	if err != nil {
		return nil, err
	}
	definitions := []ReadingDefinition{{Name: "last_operation_duration_seconds", Unit: unitSeconds}}
	switch device.Kind {
	case "reflow-oven":
		definitions = append(definitions,
			ReadingDefinition{Name: "temperature_celsius", Unit: unitCelsius},
			ReadingDefinition{Name: "target_temperature_celsius", Unit: unitCelsius},
			ReadingDefinition{Name: "temperature_ready", Unit: unitBoolean},
		)
	case "power-supply":
		definitions = append(definitions,
			ReadingDefinition{Name: "voltage_volts", Unit: unitVolts},
			ReadingDefinition{Name: "target_voltage_volts", Unit: unitVolts},
			ReadingDefinition{Name: "current_amperes", Unit: unitAmperes},
			ReadingDefinition{Name: "current_limit_amperes", Unit: unitAmperes},
			ReadingDefinition{Name: "output_enabled", Unit: unitBoolean},
		)
	case "multimeter":
		definitions = append(definitions,
			ReadingDefinition{Name: "voltage_volts", Unit: unitVolts},
			ReadingDefinition{Name: "min_voltage_volts", Unit: unitVolts},
			ReadingDefinition{Name: "max_voltage_volts", Unit: unitVolts},
		)
	}
	return definitions, nil
}
