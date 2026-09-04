package entity

import (
	"encoding/json"
	"testing"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

func TestWorkspace_Lifecycle(t *testing.T) {
	ws, err := NewWorkspace("T123", "Acme SRE", []byte("encrypted-token"))
	if err != nil {
		t.Fatalf("expected valid workspace creation, got: %v", err)
	}

	if ws.SlackTeamID() != "T123" {
		t.Errorf("expected team id 'T123', got '%s'", ws.SlackTeamID())
	}

	if err := ws.UpdateTeamName("Acme Global"); err != nil {
		t.Fatalf("failed updating team name: %v", err)
	}
	if ws.SlackTeamName() != "Acme Global" {
		t.Errorf("expected updated name 'Acme Global', got '%s'", ws.SlackTeamName())
	}

	if err := ws.UpdateTeamName(""); err == nil {
		t.Error("expected error updating to empty name, got nil")
	}
}

func TestPersona_EnableDisable(t *testing.T) {
	wsID := value.NewWorkspaceID()
	p, err := NewPersona(wsID, value.PersonaKeyDatabase, "Database Specialist", "Prompt text")
	if err != nil {
		t.Fatalf("failed creating persona: %v", err)
	}

	if !p.IsEnabled() {
		t.Error("expected persona to be enabled by default")
	}

	p.Disable()
	if p.IsEnabled() {
		t.Error("expected persona to be disabled")
	}

	p.Enable()
	if !p.IsEnabled() {
		t.Error("expected persona to be enabled")
	}

	if err := p.UpdatePrompt("New prompt"); err != nil || p.SystemPrompt() != "New prompt" {
		t.Errorf("failed updating prompt: %v", err)
	}
}

func TestDocument_LifecycleAndHashing(t *testing.T) {
	wsID := value.NewWorkspaceID()
	personaID := uuid.New()
	content := "# Network Runbook\nCheck ping and gateway."

	doc, err := NewDocument(wsID, personaID, "network.md", content)
	if err != nil {
		t.Fatalf("failed creating document: %v", err)
	}

	expectedHash := CalculateContentHash(content)
	if doc.ContentHash() != expectedHash {
		t.Errorf("expected hash '%s', got '%s'", expectedHash, doc.ContentHash())
	}

	if doc.Status() != value.DocumentStatusPending {
		t.Errorf("expected pending status, got '%s'", doc.Status())
	}
	if doc.ChunkCount() != 0 {
		t.Errorf("expected 0 chunks initially, got %d", doc.ChunkCount())
	}

	doc.MarkIndexed(12)
	if doc.Status() != value.DocumentStatusIndexed {
		t.Errorf("expected indexed status, got '%s'", doc.Status())
	}
	if doc.ChunkCount() != 12 {
		t.Errorf("expected 12 chunks, got %d", doc.ChunkCount())
	}

	doc.MarkFailed()
	if doc.Status() != value.DocumentStatusFailed {
		t.Errorf("expected failed status, got '%s'", doc.Status())
	}
}

func TestIncident_LifecycleWithSeverity(t *testing.T) {
	wsID := value.NewWorkspaceID()
	inc, err := NewIncident(
		wsID,
		"Payment Gateway 504",
		"Users reporting checkout errors",
		value.SlackChannelID("C12345"),
		value.SlackThreadTS("1709512345.000100"),
		value.SeveritySev1,
		value.SlackUserID("U999"),
		json.RawMessage(`{"tags":["checkout"]}`),
	)
	if err != nil {
		t.Fatalf("failed creating incident: %v", err)
	}

	if inc.Severity() != value.SeveritySev1 {
		t.Errorf("expected severity sev-1, got '%s'", inc.Severity())
	}

	if err := inc.UpdateSeverity(value.SeveritySev2); err != nil || inc.Severity() != value.SeveritySev2 {
		t.Errorf("failed updating severity: %v", err)
	}

	if inc.Status().IsResolved() {
		t.Error("new incident should not be resolved")
	}

	resolvedTime := time.Now().UTC()
	if err := inc.Resolve(resolvedTime); err != nil {
		t.Fatalf("failed to resolve incident: %v", err)
	}

	if !inc.Status().IsResolved() {
		t.Error("expected incident to be resolved")
	}
	if inc.ResolvedAt() == nil || *inc.ResolvedAt() != resolvedTime {
		t.Error("expected resolved timestamp to match")
	}

	// Invariant guard: cannot resolve an already resolved incident
	if err := inc.Resolve(resolvedTime); err != ErrIncidentAlreadyResolved {
		t.Errorf("expected ErrIncidentAlreadyResolved, got: %v", err)
	}
}

func TestDebateTurn_WithCitations(t *testing.T) {
	incID := uuid.New()
	wsID := value.NewWorkspaceID()
	pID := uuid.New()
	chunkID := uuid.New()

	metadataJSON := `{"citations":[{"chunk_id":"` + chunkID.String() + `","document_title":"database.md","snippet":"lock contention detected"}]}`

	turn, err := NewDebateTurn(
		incID,
		wsID,
		&pID,
		value.StageSpecialistBlast,
		value.TurnTypeSpecialistHypothesis,
		"Database locks detected on orders table.",
		[]uuid.UUID{chunkID},
		"1709512350.000200",
		json.RawMessage(metadataJSON),
	)
	if err != nil {
		t.Fatalf("failed creating debate turn: %v", err)
	}

	if len(turn.ReferencedChunkIDs()) != 1 || turn.ReferencedChunkIDs()[0] != chunkID {
		t.Errorf("referenced chunk ID mismatch")
	}

	citations, err := turn.Citations()
	if err != nil {
		t.Fatalf("failed extracting citations: %v", err)
	}
	if len(citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(citations))
	}
	if citations[0].ChunkID != chunkID || citations[0].DocumentTitle != "database.md" {
		t.Errorf("citation fields mismatch: %+v", citations[0])
	}
}

func TestIncident_TriggerTypeAndContext(t *testing.T) {
	wsID := value.NewWorkspaceID()
	inc, err := NewIncident(
		wsID,
		"Checkout Latency Spike",
		"504s observed at payment gateway",
		value.SlackChannelID("C999"),
		value.SlackThreadTS("1709512345.000100"),
		value.SeveritySev2,
		value.SlackUserID("U123"),
		nil,
	)
	if err != nil {
		t.Fatalf("failed creating incident: %v", err)
	}

	// Default trigger type is slash_command
	if inc.TriggerType() != value.TriggerTypeSlashCommand {
		t.Errorf("expected default trigger type 'slash_command', got '%s'", inc.TriggerType())
	}

	// Update to webhook
	if err := inc.SetTriggerType(value.TriggerTypeWebhook); err != nil {
		t.Fatalf("failed setting trigger type: %v", err)
	}
	if inc.TriggerType() != value.TriggerTypeWebhook {
		t.Errorf("expected trigger type 'webhook', got '%s'", inc.TriggerType())
	}

	// Initial context should be nil
	ctx, err := inc.IncidentContext()
	if err != nil {
		t.Fatalf("unexpected error getting nil incident context: %v", err)
	}
	if ctx != nil {
		t.Error("expected nil incident context initially")
	}

	// Set IncidentContext
	inContext := value.NewIncidentContext(
		"cloudwatch",
		"HighErrorRateAlarm",
		[]string{"Task timed out after 15.00 seconds"},
		[]string{"main.go:88"},
		map[string]float64{"Errors": 42.0},
	)
	if err := inc.SetIncidentContext(inContext); err != nil {
		t.Fatalf("failed setting incident context: %v", err)
	}

	retrievedCtx, err := inc.IncidentContext()
	if err != nil {
		t.Fatalf("failed retrieving incident context: %v", err)
	}
	if retrievedCtx == nil {
		t.Fatal("expected non-nil retrieved context")
	}
	if retrievedCtx.Provider() != "cloudwatch" {
		t.Errorf("expected provider 'cloudwatch', got '%s'", retrievedCtx.Provider())
	}
	if retrievedCtx.AlertName() != "HighErrorRateAlarm" {
		t.Errorf("expected alert name 'HighErrorRateAlarm', got '%s'", retrievedCtx.AlertName())
	}
	if len(retrievedCtx.ErrorLogs()) != 1 {
		t.Errorf("expected 1 error log, got %d", len(retrievedCtx.ErrorLogs()))
	}
}
