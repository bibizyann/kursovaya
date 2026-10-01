package telemetrymqtt

import (
	"bytes"
	"encoding/json"
	"errors"

	"factory.local/factory-platform/internal/telemetry"
	"factory.local/factory-platform/internal/telemetryreceiver"
)

func topic(key telemetryreceiver.StreamKey) string {
	return "factory/v1/" + key.Namespace + "/" + key.TechProcess + "/" + key.EquipmentID + "/telemetry"
}

func keyOf(snapshot telemetry.Snapshot) telemetryreceiver.StreamKey {
	return telemetryreceiver.StreamKey{Namespace: snapshot.Namespace, TechProcess: snapshot.TechProcess, EquipmentID: snapshot.EquipmentID}
}

type publication struct {
	topic   string
	payload []byte
}

func encode(snapshot telemetry.Snapshot) (publication, error) {
	if err := snapshot.Validate(); err != nil {
		return publication{}, err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return publication{}, err
	}
	if len(payload) > telemetry.MaxMessageBytes {
		return publication{}, errors.New("MQTT snapshot exceeds size limit")
	}
	return publication{topic: topic(keyOf(snapshot)), payload: payload}, nil
}

// Handle validates untrusted JSON and topic identity before passing a snapshot to the receiver.
// False with no error means the receiver ignored an old or duplicate message.
func (s *Subscriber) Handle(messageTopic string, payload []byte) (bool, error) {
	snapshot, err := telemetry.Decode(bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	if messageTopic != topic(keyOf(snapshot)) {
		return false, errors.New("MQTT topic does not match snapshot identity")
	}
	return s.receiver.Accept(snapshot)
}
