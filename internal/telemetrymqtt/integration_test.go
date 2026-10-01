//go:build mqttintegration

package telemetrymqtt

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"factory.local/factory-platform/internal/telemetryreceiver"
)

func dockerCommand(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func broker(t *testing.T) (string, string) {
	t.Helper()
	image := os.Getenv("MQTT_BROKER_IMAGE")
	if image == "" {
		image = "eclipse-mosquitto:2.0.22" //nolint:misspell // Official image name.
	}
	configPath, err := filepath.Abs("../../infra/mqtt/mosquitto.conf")
	if err != nil {
		t.Fatal(err)
	}
	name := "factory-mqtt-test-" + strings.ToLower(rand.Text()[:12])
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(dockerCommand(t, "logs", name))
		}
		dockerCommand(t, "rm", "--force", name)
	})
	// Pin the chosen port: Docker may choose a new ephemeral mapping after restart.
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	dockerCommand(t, "run", "--detach", "--name", name, "--publish", address+":1883",
		"--mount", "type=bind,src="+configPath+",dst=/tmp/mosquitto.conf,readonly",
		image, "mosquitto", "-c", "/tmp/mosquitto.conf") //nolint:misspell // Official executable name.
	if mapped := dockerCommand(t, "port", name, "1883/tcp"); mapped != address {
		t.Fatalf("unexpected broker port: %s", mapped)
	}
	eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	return name, "tcp://" + address
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("MQTT integration condition timed out")
		case <-ticker.C:
		}
	}
}

func startWorker(t *testing.T, run func(context.Context) error) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("MQTT worker did not stop")
		}
	})
	return cancel
}

func TestMosquittoRetainedDeliveryAndReconnect(t *testing.T) {
	name, brokerURL := broker(t)
	config := quietConfig()
	config.BrokerURL, config.Timeout, config.RetryInterval = brokerURL, time.Second, 50*time.Millisecond
	var nanos atomic.Int64
	nanos.Store(time.Now().UTC().UnixNano())
	now := func() time.Time { return time.Unix(0, nanos.Load()).UTC() }
	snapshot := fixture(t)
	snapshot.SourceStartedAt, snapshot.ObservedAt = now().Add(-time.Second), now()
	newReceiver := func() *telemetryreceiver.Receiver {
		receiver, err := telemetryreceiver.New(telemetryreceiver.Config{
			Expected: []telemetryreceiver.StreamKey{keyOf(snapshot)}, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return receiver
	}
	// A retained snapshot must be received even when it was published before subscription.
	rawConfig, err := configure(config, "test")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rawConfig.connect(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.close()
	message, err := encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitToken(context.Background(), raw.client.Publish(message.topic, 1, true, message.payload), time.Second); err != nil {
		t.Fatal(err)
	}
	receiver := newReceiver()
	subscriber, err := NewSubscriber(config, receiver)
	if err != nil {
		t.Fatal(err)
	}
	stopSubscriber := startWorker(t, subscriber.Run)
	eventually(t, func() bool { return receiver.Observations()[0].Fresh })
	if receiver.Observations()[0].Snapshot.Sequence != snapshot.Sequence {
		t.Fatal("retained snapshot not restored")
	}
	// Duplicate delivery must not add to cumulative counts.
	if err := waitToken(context.Background(), raw.client.Publish(message.topic, 1, true, message.payload), time.Second); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher(config)
	if err != nil {
		t.Fatal(err)
	}
	startWorker(t, publisher.Run)
	snapshot.Sequence++
	snapshot.Counters.OperationsTotal++
	eventually(t, func() bool { return publisher.Publish(context.Background(), snapshot) == nil })
	eventually(t, func() bool { return receiver.Observations()[0].Snapshot.Sequence == snapshot.Sequence })
	if receiver.Observations()[0].Snapshot.Counters != snapshot.Counters {
		t.Fatal("duplicate changed counters")
	}
	// Shutdown clears broker RAM. The clients must reconnect with clean sessions and resubscribe.
	dockerCommand(t, "stop", "--time", "0", name)
	eventually(t, func() bool { publisher.mu.Lock(); defer publisher.mu.Unlock(); return !publisher.connected })
	nanos.Add(int64(21 * time.Second))
	if receiver.Observations()[0].Fresh {
		t.Fatal("broker outage did not expire the observation")
	}
	snapshot.Sequence++
	snapshot.ObservedAt = now()
	if err := publisher.Publish(context.Background(), snapshot); !errors.Is(err, ErrDisconnected) {
		t.Fatal("outage publication was not dropped")
	}
	dockerCommand(t, "start", name)
	snapshot.Sequence++
	snapshot.Counters.OperationsTotal += 5
	eventually(t, func() bool { return publisher.Publish(context.Background(), snapshot) == nil })
	eventually(t, func() bool {
		view := receiver.Observations()[0]
		return view.Fresh && view.Snapshot.Sequence == snapshot.Sequence
	})
	// A new receiver recovers the latest retained snapshot; it does not recover history.
	stopSubscriber()
	restarted := newReceiver()
	restartedSubscriber, err := NewSubscriber(config, restarted)
	if err != nil {
		t.Fatal(err)
	}
	startWorker(t, restartedSubscriber.Run)
	eventually(t, func() bool { return restarted.Observations()[0].Fresh })
	if restarted.Observations()[0].Snapshot.Sequence != snapshot.Sequence {
		t.Fatal("receiver restart restored wrong snapshot")
	}
	// Topic spoofing is rejected even when both identities are otherwise well-formed.
	wrong := snapshot
	wrong.Namespace = "wrong-namespace"
	wrong.Sequence += 100
	spoof, err := json.Marshal(wrong)
	if err != nil {
		t.Fatal(err)
	}
	check, err := rawConfig.connect(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer check.close()
	if err := waitToken(context.Background(), check.client.Publish(message.topic, 1, false, spoof), time.Second); err != nil {
		t.Fatal(err)
	}
	snapshot.Sequence++
	if err := publisher.Publish(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return restarted.Observations()[0].Snapshot.Sequence == snapshot.Sequence })
}
