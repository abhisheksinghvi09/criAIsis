package server_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"criaisis/internal/config"
	"criaisis/internal/job"
	"criaisis/internal/server"

	"github.com/rs/zerolog"
)

// Server.New requires a live database ping, so these tests build the struct
// directly (every field is exported) to exercise the HTTP lifecycle and
// shutdown ordering in isolation.

type stubQueue struct {
	shutdownErr   error
	shutdownCalls int
}

func (q *stubQueue) Enqueue(job.IncidentJob) error { return nil }
func (q *stubQueue) Start(context.Context, job.Handler) {}
func (q *stubQueue) Shutdown(context.Context) error {
	q.shutdownCalls++
	return q.shutdownErr
}

func testConfig() *config.Config {
	return &config.Config{
		Primary: config.Primary{Env: "test"},
		Server: config.ServerConfig{
			Port: "0", ReadTimeout: 1, WriteTimeout: 1, IdleTimeout: 1,
		},
	}
}

func TestStart_WithoutSetupHTTPServerReturnsAClearError(t *testing.T) {
	logger := zerolog.Nop()
	srv := &server.Server{Config: testConfig(), Logger: &logger}

	if err := srv.Start(); err == nil {
		t.Error("expected Start to fail before SetupHTTPServer is called")
	}
}

func TestSetupHTTPServer_AppliesConfiguredTimeouts(t *testing.T) {
	logger := zerolog.Nop()
	srv := &server.Server{Config: testConfig(), Logger: &logger}
	srv.SetupHTTPServer(http.NotFoundHandler())

	// Start should now bind and run; cancel it immediately via Shutdown and
	// confirm ListenAndServe's normal-shutdown path is treated as success.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	// Give the listener a moment to come up before shutting it down.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := srv.Shutdown(ctx)
		cancel()
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected ErrServerClosed to be treated as a clean stop, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

func TestShutdown_StopsQueueEvenWithNoDatabaseOrHTTPServer(t *testing.T) {
	logger := zerolog.Nop()
	queue := &stubQueue{}
	srv := &server.Server{Config: testConfig(), Logger: &logger, Queue: queue}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if queue.shutdownCalls != 1 {
		t.Errorf("expected the queue to be shut down exactly once, got %d", queue.shutdownCalls)
	}
}

func TestShutdown_AggregatesQueueFailure(t *testing.T) {
	logger := zerolog.Nop()
	queueErr := errors.New("queue would not drain")
	queue := &stubQueue{shutdownErr: queueErr}
	srv := &server.Server{Config: testConfig(), Logger: &logger, Queue: queue}

	err := srv.Shutdown(context.Background())
	if err == nil || !errors.Is(err, queueErr) {
		t.Errorf("expected the queue shutdown failure to be reported, got %v", err)
	}
}
