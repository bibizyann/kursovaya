package telemetrymqtt

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"factory.local/factory-platform/internal/telemetry"
)

// ErrDisconnected reports that a snapshot was dropped while the broker was unavailable.
var ErrDisconnected = errors.New("MQTT publisher is disconnected; snapshot dropped")

// Publisher keeps at most one waiting snapshot and one publication in flight.
// One Publisher belongs to one equipment stream.
type Publisher struct {
	config    connectionConfig
	mu        sync.Mutex
	connected bool
	stream    string
	pending   chan publication
	running   atomic.Bool
}

// NewPublisher validates broker configuration without connecting to the network.
func NewPublisher(config Config) (*Publisher, error) {
	c, err := configure(config, "pub")
	if err != nil {
		return nil, err
	}
	return &Publisher{config: c, pending: make(chan publication, 1)}, nil
}

// Publish offers a snapshot without waiting for network I/O. Nil means queued, not acknowledged.
// A newer snapshot replaces the single waiting snapshot. Disconnected snapshots are dropped.
func (p *Publisher) Publish(ctx context.Context, snapshot telemetry.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	message, err := encode(snapshot)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != "" && p.stream != message.topic {
		return errors.New("publisher cannot change equipment stream")
	}
	p.stream = message.topic
	if !p.connected {
		return ErrDisconnected
	}
	select {
	case <-p.pending:
	default:
	}
	p.pending <- message
	return nil
}

func (p *Publisher) setConnected(connected bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = connected
	select {
	case <-p.pending:
	default:
	}
}

// Run connects and reconnects until cancellation, discarding pending data between sessions.
func (p *Publisher) Run(ctx context.Context) error {
	if !p.running.CompareAndSwap(false, true) {
		return errors.New("MQTT publisher is already running")
	}
	defer p.running.Store(false)
	defer p.setConnected(false)
	for ctx.Err() == nil {
		s, err := p.config.connect(ctx, nil)
		if err == nil {
			p.setConnected(true)
			p.config.Logger.Info("MQTT publisher connected")
			err = p.send(ctx, s)
			p.setConnected(false)
			s.close()
		}
		if ctx.Err() != nil {
			return nil
		}
		p.config.Logger.Warn("MQTT publisher session ended; retrying", "error", err)
		if !retry(ctx, p.config.RetryInterval) {
			return nil
		}
	}
	return nil
}

func (p *Publisher) send(ctx context.Context, s *session) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.lost:
			return err
		case message := <-p.pending:
			if err := waitToken(ctx, s.client.Publish(message.topic, 1, true, message.payload), p.config.Timeout); err != nil {
				return err
			}
		}
	}
}
