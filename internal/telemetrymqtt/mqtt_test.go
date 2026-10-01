package telemetrymqtt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"factory.local/factory-platform/internal/telemetry"
	"factory.local/factory-platform/internal/telemetryreceiver"
)

const testBroker = "tcp://127.0.0.1:1883"

func quietConfig() Config {
	return Config{BrokerURL: testBroker, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func fixture(t *testing.T) telemetry.Snapshot {
	t.Helper()
	payload, err := os.ReadFile("../telemetry/testdata/reflow-oven-01.json")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := telemetry.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestConnectionConfiguration(t *testing.T) {
	for _, broker := range []string{"", "mqtt://host:1883", "tcp://host", "tcp://host:bad", "tcp://host:0", "tcp://host:65536", "tcp://user:secret@host:1883", "tcp://host:1883/path", "tcp://host:1883?password=secret"} {
		if _, err := NewPublisher(Config{BrokerURL: broker}); err == nil {
			t.Fatalf("accepted broker %q", broker)
		}
	}
	for _, config := range []Config{{BrokerURL: testBroker, Timeout: -1}, {BrokerURL: testBroker, RetryInterval: -1}} {
		if _, err := NewPublisher(config); err == nil {
			t.Fatal("accepted negative duration")
		}
	}
	config, err := configure(Config{BrokerURL: "ssl://localhost:8883"}, "test")
	if err != nil || config.Timeout != 3*time.Second || config.RetryInterval != time.Second || config.Logger == nil {
		t.Fatalf("defaults: %+v, %v", config, err)
	}
	if _, err := NewSubscriber(quietConfig(), nil); err == nil {
		t.Fatal("accepted nil receiver")
	}
}

func TestTopicValidationAndDuplicateHandling(t *testing.T) {
	snapshot := fixture(t)
	now := snapshot.ObservedAt
	key := keyOf(snapshot)
	other := key
	other.Namespace = "other"
	receiver, err := telemetryreceiver.New(telemetryreceiver.Config{Expected: []telemetryreceiver.StreamKey{key, other}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := NewSubscriber(quietConfig(), receiver)
	if err != nil {
		t.Fatal(err)
	}
	message, err := encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalidTopic := range []string{topic(other), message.topic + "/extra", strings.Replace(message.topic, "/v1/", "/v2/", 1), "factory/v1/+/+/+/telemetry"} {
		if ok, err := subscriber.Handle(invalidTopic, message.payload); ok || err == nil {
			t.Fatal("accepted mismatched topic")
		}
	}
	for _, payload := range [][]byte{nil, []byte("{"), bytes.Repeat([]byte("x"), telemetry.MaxMessageBytes+1)} {
		if ok, err := subscriber.Handle(message.topic, payload); ok || err == nil {
			t.Fatal("accepted invalid payload")
		}
	}
	if ok, err := subscriber.Handle(message.topic, message.payload); !ok || err != nil {
		t.Fatalf("valid message: %v", err)
	}
	if ok, err := subscriber.Handle(message.topic, message.payload); ok || err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if receiver.Observations()[1].Snapshot != nil {
		t.Fatal("topic spoof changed another stream")
	}
	// Retained payloads use the same age policy as live messages, including after receiver restart.
	now = now.Add(telemetryreceiver.DefaultFreshnessTimeout)
	newReceiver, err := telemetryreceiver.New(telemetryreceiver.Config{Expected: []telemetryreceiver.StreamKey{key}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	newSubscriber, err := NewSubscriber(quietConfig(), newReceiver)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := newSubscriber.Handle(message.topic, message.payload); ok || err == nil {
		t.Fatal("accepted stale retained snapshot")
	}
	if newReceiver.Observations()[0].Snapshot != nil {
		t.Fatal("stale first message stored")
	}
}

func TestPublisherBoundsAndOwnsPendingPayload(t *testing.T) {
	publisher, err := NewPublisher(quietConfig())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := fixture(t)
	if err := publisher.Publish(context.Background(), snapshot); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("disconnected publish: %v", err)
	}
	if len(publisher.pending) != 0 {
		t.Fatal("queued disconnected snapshot")
	}
	publisher.setConnected(true)
	for i := uint64(1); i <= 100; i++ {
		snapshot.Sequence = i
		if err := publisher.Publish(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	*snapshot.SimulationSeed = 999
	message := <-publisher.pending
	stored, err := telemetry.Decode(bytes.NewReader(message.payload))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Sequence != 100 || *stored.SimulationSeed == 999 {
		t.Fatal("pending snapshot was not replaced or was aliased")
	}
	if err := publisher.Publish(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	publisher.setConnected(false)
	publisher.setConnected(true)
	if len(publisher.pending) != 0 {
		t.Fatal("pending snapshot survived reconnect")
	}
	snapshot.Namespace = "another"
	if err := publisher.Publish(context.Background(), snapshot); err == nil {
		t.Fatal("publisher switched streams")
	}
	snapshot.SchemaVersion = 0
	if err := publisher.Publish(context.Background(), snapshot); err == nil {
		t.Fatal("invalid snapshot queued")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, fixture(t)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publish was not rejected")
	}
	if err := publisher.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentPublishRetainsOneMessage(t *testing.T) {
	publisher, err := NewPublisher(quietConfig())
	if err != nil {
		t.Fatal(err)
	}
	publisher.setConnected(true)
	snapshot := fixture(t)
	var workers sync.WaitGroup
	for range 50 {
		workers.Go(func() {
			if err := publisher.Publish(context.Background(), snapshot); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if len(publisher.pending) != 1 {
		t.Fatal("publisher queue is not bounded")
	}
}

func TestCancellationClosesUnresponsiveHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	accepted := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		close(accepted)
		_, _ = io.Copy(io.Discard, conn)
	}()
	config := quietConfig()
	config.BrokerURL = "tcp://" + listener.Addr().String()
	c, err := configure(config, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.connect(ctx, nil); done <- err }()
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not opened")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("connect error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connect ignored cancellation")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancelled socket leaked")
	}
}

func TestEncodeMatchesWireContract(t *testing.T) {
	snapshot := fixture(t)
	message, err := encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded telemetry.Snapshot
	if err := json.Unmarshal(message.payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if message.topic != "factory/v1/"+snapshot.Namespace+"/"+snapshot.TechProcess+"/"+snapshot.EquipmentID+"/telemetry" || decoded.Sequence != snapshot.Sequence {
		t.Fatal("wrong wire identity")
	}
}
