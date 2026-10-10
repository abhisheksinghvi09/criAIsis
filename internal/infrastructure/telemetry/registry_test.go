package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"criaisis/internal/domain/repository"
	"criaisis/internal/infrastructure/telemetry"
)

// stubTool is a controllable adapter for exercising registry behaviour.
type stubTool struct {
	name  string
	delay time.Duration
	err   error
}

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return "stub probe" }

func (s stubTool) Execute(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
	if s.err != nil {
		return nil, s.err
	}
	select {
	case <-time.After(s.delay):
		return json.RawMessage(`{"value":1}`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

var _ repository.DiagnosticTool = stubTool{}

func TestRegistry_ExecutesRegisteredTool(t *testing.T) {
	reg := telemetry.NewRegistry(stubTool{name: "fast"})

	got, err := reg.Execute(context.Background(), "fast", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if string(got) != `{"value":1}` {
		t.Errorf("unexpected result: %s", got)
	}
}

func TestRegistry_UnknownToolIsRejected(t *testing.T) {
	reg := telemetry.NewRegistry(stubTool{name: "fast"})

	if _, err := reg.Execute(context.Background(), "nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an error for an unregistered tool")
	}
}

// A telemetry backend that hangs must not be able to consume the stage budget.
func TestRegistry_EnforcesTimeoutOnSlowTool(t *testing.T) {
	reg := telemetry.NewRegistry(stubTool{name: "slow", delay: telemetry.ToolTimeout + 2*time.Second})

	start := time.Now()
	_, err := reg.Execute(context.Background(), "slow", json.RawMessage(`{}`))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the slow tool to be cancelled")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected a deadline error, got %v", err)
	}
	if elapsed > telemetry.ToolTimeout+time.Second {
		t.Errorf("registry waited %v, beyond the %v ceiling", elapsed, telemetry.ToolTimeout)
	}
}

// Graceful degradation: a failing probe must yield a prompt note, never an error
// that would sink the whole specialist turn.
func TestRegistry_ProbeDegradesGracefully(t *testing.T) {
	reg := telemetry.NewRegistry(stubTool{name: "broken", err: errors.New("connection refused")})

	note := reg.Probe(context.Background(), "broken", json.RawMessage(`{}`))
	if !strings.Contains(note, "unavailable") {
		t.Errorf("expected an availability note, got %q", note)
	}
}

func TestRegistry_DescribeListsToolsInStableOrder(t *testing.T) {
	reg := telemetry.NewRegistry(stubTool{name: "zulu"}, stubTool{name: "alpha"})

	names := reg.Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zulu" {
		t.Fatalf("expected sorted names, got %v", names)
	}
	if !strings.Contains(reg.Describe(), "alpha") {
		t.Error("catalogue should describe every registered tool")
	}
}
