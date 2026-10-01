package telemetrymqtt

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"factory.local/factory-platform/internal/telemetryreceiver"
)

// Subscriber receives only explicitly configured device streams into an existing receiver.
type Subscriber struct {
	config   connectionConfig
	receiver *telemetryreceiver.Receiver
	filters  map[string]byte
	running  atomic.Bool
}

// NewSubscriber validates configuration and derives exact topic subscriptions from receiver inventory.
func NewSubscriber(config Config, receiver *telemetryreceiver.Receiver) (*Subscriber, error) {
	if receiver == nil {
		return nil, errors.New("MQTT subscriber requires a receiver")
	}
	c, err := configure(config, "sub")
	if err != nil {
		return nil, err
	}
	filters := make(map[string]byte)
	for _, observation := range receiver.Observations() {
		filters[topic(observation.Key)] = 1
	}
	return &Subscriber{config: c, receiver: receiver, filters: filters}, nil
}

// Run reconnects and resubscribes until cancellation, independently of equipment readiness.
func (s *Subscriber) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("MQTT subscriber is already running")
	}
	defer s.running.Store(false)
	for ctx.Err() == nil {
		err := s.receive(ctx)
		if ctx.Err() != nil {
			return nil
		}
		s.config.Logger.Warn("MQTT subscriber session ended; retrying", "error", err)
		if !retry(ctx, s.config.RetryInterval) {
			return nil
		}
	}
	return nil
}

func (s *Subscriber) receive(ctx context.Context) error {
	connection, err := s.config.connect(ctx, func(_ mqtt.Client, message mqtt.Message) {
		if _, err := s.Handle(message.Topic(), message.Payload()); err != nil {
			s.config.Logger.Warn("Rejected MQTT telemetry", "topic", message.Topic(), "error", err)
		}
	})
	if err != nil {
		return err
	}
	defer connection.close()
	token := connection.client.SubscribeMultiple(s.filters, nil)
	if err := waitToken(ctx, token, s.config.Timeout); err != nil {
		return fmt.Errorf("MQTT subscribe: %w", err)
	}
	if result, ok := token.(*mqtt.SubscribeToken); ok {
		for name, qos := range result.Result() {
			if qos == 0x80 {
				return fmt.Errorf("MQTT subscription denied for %s", name)
			}
		}
	}
	s.config.Logger.Info("MQTT subscriber connected", "streams", len(s.filters))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-connection.lost:
		return err
	}
}
