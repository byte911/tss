package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// RunServerOnPort starts a NATS server on the specified port
func RunServerOnPort(port int) (*server.Server, error) {
	opts := &server.Options{
		Host:           "127.0.0.1",
		Port:           port,
		NoLog:          true,
		NoSigs:         true,
		MaxControlLine: 256,
	}

	return server.NewServer(opts)
}

// SetupJetStream sets up a NATS server with JetStream enabled for testing
func SetupJetStream(t *testing.T) (nats.JetStreamContext, func()) {
	t.Helper()

	// Start NATS server with JetStream
	_, js, cleanup := StartJetStream(t)

	return js, cleanup
}

// StartJetStream starts a NATS server with JetStream enabled
func StartJetStream(t *testing.T) (*server.Server, nats.JetStreamContext, func()) {
	t.Helper()

	// Start NATS server
	s, err := RunServerOnPort(14222)
	require.NoError(t, err)
	err = s.EnableJetStream(&server.JetStreamConfig{
		StoreDir: t.TempDir(),
	})
	require.NoError(t, err)

	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("Unable to start NATS server")
	}

	// Connect to server
	nc, err := nats.Connect(s.ClientURL(), nats.Timeout(5*time.Second))
	require.NoError(t, err)

	// Create JetStream context
	js, err := nc.JetStream(nats.MaxWait(5 * time.Second))
	require.NoError(t, err)

	cleanup := func() {
		nc.Close()
		s.Shutdown()
	}

	return s, js, cleanup
}

// WaitForStream waits for a stream to be created
func WaitForStream(t *testing.T, js nats.JetStreamContext, name string, timeout time.Duration) error {
	t.Helper()

	start := time.Now()
	for time.Since(start) < timeout {
		_, err := js.StreamInfo(name)
		if err == nil {
			return nil
		}
		if err != nats.ErrStreamNotFound {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for stream %s", name)
}

// WaitForConsumer waits for a consumer to be created
func WaitForConsumer(t *testing.T, js nats.JetStreamContext, stream, consumer string, timeout time.Duration) error {
	t.Helper()

	start := time.Now()
	for time.Since(start) < timeout {
		_, err := js.ConsumerInfo(stream, consumer)
		if err == nil {
			return nil
		}
		if err != nats.ErrConsumerNotFound {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for consumer %s on stream %s", consumer, stream)
}

// PublishWithRetry publishes a message with retries
func PublishWithRetry(js nats.JetStreamContext, subject string, data []byte, retries int, delay time.Duration) error {
	var err error
	for i := 0; i < retries; i++ {
		_, err = js.Publish(subject, data)
		if err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}

// ConsumeMessages consumes messages from a subject for a specified duration
func ConsumeMessages(js nats.JetStreamContext, subject string, duration time.Duration) ([][]byte, error) {
	var messages [][]byte
	msgChan := make(chan *nats.Msg, 100)
	sub, err := js.Subscribe(subject, func(msg *nats.Msg) {
		msgChan <- msg
	})
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()

	timer := time.NewTimer(duration)
	defer timer.Stop()

	for {
		select {
		case msg := <-msgChan:
			messages = append(messages, msg.Data)
		case <-timer.C:
			return messages, nil
		}
	}
}
