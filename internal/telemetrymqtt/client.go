// Package telemetrymqtt transports complete equipment snapshots over MQTT.
package telemetrymqtt

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Config defines a local telemetry broker connection and bounded retry policy.
type Config struct {
	BrokerURL string
	Username  string
	Password  string
	// Timeout defaults to three seconds for each network operation.
	Timeout time.Duration
	// RetryInterval defaults to one second between failed sessions.
	RetryInterval time.Duration
	Logger        *slog.Logger
}

type connectionConfig struct {
	Config
	endpoint *url.URL
	clientID string
}

func configure(config Config, role string) (connectionConfig, error) {
	u, err := url.Parse(config.BrokerURL)
	if err != nil || u == nil {
		return connectionConfig{}, errors.New("invalid MQTT broker URL")
	}
	if (u.Scheme != "tcp" && u.Scheme != "ssl") || u.Hostname() == "" || u.Port() == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return connectionConfig{}, errors.New("MQTT broker must be tcp://host:port or ssl://host:port without credentials or path")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return connectionConfig{}, errors.New("MQTT broker port must be between 1 and 65535")
	}
	if config.Timeout < 0 || config.RetryInterval < 0 {
		return connectionConfig{}, errors.New("MQTT time limits must not be negative")
	}
	if config.Timeout == 0 {
		config.Timeout = 3 * time.Second
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return connectionConfig{Config: config, endpoint: u, clientID: role + "-" + rand.Text()[:16]}, nil
}

type session struct {
	client mqtt.Client
	conn   net.Conn
	lost   chan error
}

func (s *session) close() {
	s.client.Disconnect(0)
	_ = s.conn.Close()
}

func (c connectionConfig) connect(ctx context.Context, handler mqtt.MessageHandler) (*session, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	dialer := &net.Dialer{Timeout: c.Timeout}
	var conn net.Conn
	var err error
	if c.endpoint.Scheme == "ssl" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{MinVersion: tls.VersionTLS12}}
		conn, err = tlsDialer.DialContext(dialCtx, "tcp", c.endpoint.Host)
	} else {
		conn, err = dialer.DialContext(dialCtx, "tcp", c.endpoint.Host)
	}
	if err != nil {
		return nil, fmt.Errorf("MQTT dial: %w", err)
	}
	s := &session{conn: conn, lost: make(chan error, 1)}
	options := mqtt.NewClientOptions().AddBroker(c.BrokerURL).SetClientID(c.clientID).
		SetProtocolVersion(4).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).
		SetConnectTimeout(c.Timeout).SetWriteTimeout(c.Timeout).
		SetKeepAlive(5 * time.Second).SetPingTimeout(time.Second).
		SetUsername(c.Username).SetPassword(c.Password).SetOrderMatters(true).
		SetStore(mqtt.NewMemoryStore()).SetDefaultPublishHandler(handler)
	// Owning the socket lets cancellation interrupt the MQTT handshake as well as active I/O.
	options.SetCustomOpenConnectionFn(func(*url.URL, mqtt.ClientOptions) (net.Conn, error) { return conn, nil })
	options.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		select {
		case s.lost <- err:
		default:
		}
	})
	s.client = mqtt.NewClient(options)
	if err := waitToken(ctx, s.client.Connect(), c.Timeout); err != nil {
		s.close()
		return nil, fmt.Errorf("MQTT connect: %w", err)
	}
	return s, nil
}

func waitToken(ctx context.Context, token mqtt.Token, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("MQTT operation timed out")
	case <-token.Done():
		return token.Error()
	}
}

func retry(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
