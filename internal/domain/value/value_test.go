package value

import (
	"testing"

	"github.com/google/uuid"
)

func TestWorkspaceID_Valid(t *testing.T) {
	ws := NewWorkspaceID()
	if ws.IsZero() {
		t.Error("expected new workspace id to not be zero")
	}

	parsed, err := ParseWorkspaceID(ws.String())
	if err != nil {
		t.Fatalf("failed to parse valid workspace id: %v", err)
	}
	if parsed.UUID() != ws.UUID() {
		t.Errorf("uuid mismatch: %s vs %s", parsed.UUID(), ws.UUID())
	}
}

func TestWorkspaceID_Invalid(t *testing.T) {
	_, err := ParseWorkspaceID("invalid-uuid")
	if err == nil {
		t.Error("expected error parsing invalid uuid, got nil")
	}

	_, err = ParseWorkspaceID(uuid.Nil.String())
	if err == nil {
		t.Error("expected error parsing nil uuid, got nil")
	}
}

func TestPersonaKey_Validation(t *testing.T) {
	cases := []struct {
		input       string
		expectedKey PersonaKey
		shouldErr   bool
	}{
		{"network", PersonaKeyNetwork, false},
		{"DATABASE", PersonaKeyDatabase, false},
		{" Application ", PersonaKeyApplication, false},
		{"security", PersonaKeySecurity, false},
		{"frontend", "", true},
		{"", "", true},
	}

	for _, c := range cases {
		k, err := ParsePersonaKey(c.input)
		if c.shouldErr && err == nil {
			t.Errorf("expected error for '%s', got nil", c.input)
		}
		if !c.shouldErr && err != nil {
			t.Errorf("unexpected error for '%s': %v", c.input, err)
		}
		if !c.shouldErr && k != c.expectedKey {
			t.Errorf("expected '%s', got '%s'", c.expectedKey, k)
		}
	}
}

func TestIncidentStatus_Lifecycle(t *testing.T) {
	inv, err := ParseIncidentStatus("investigating")
	if err != nil || inv.IsResolved() {
		t.Errorf("expected investigating status, got error: %v", err)
	}

	res, err := ParseIncidentStatus("resolved")
	if err != nil || !res.IsResolved() {
		t.Errorf("expected resolved status, got error: %v", err)
	}

	_, err = ParseIncidentStatus("closed")
	if err == nil {
		t.Error("expected error for unknown status 'closed', got nil")
	}
}

func TestSeverity_ParsingAndValidation(t *testing.T) {
	cases := []struct {
		input    string
		expected Severity
		valid    bool
	}{
		{"sev-1", SeveritySev1, true},
		{"SEV-1", SeveritySev1, true},
		{"sev1", SeveritySev1, true},
		{"critical", SeveritySev1, true},
		{"sev-2", SeveritySev2, true},
		{"high", SeveritySev2, true},
		{"sev-3", SeveritySev3, true},
		{"minor", SeveritySev3, true},
		{"sev-4", SeveritySev4, true},
		{"low", SeveritySev4, true},
		{"sev-99", "", false},
		{"", "", false},
	}

	for _, c := range cases {
		sev, err := ParseSeverity(c.input)
		if c.valid {
			if err != nil {
				t.Errorf("expected valid severity for '%s': %v", c.input, err)
			}
			if sev != c.expected {
				t.Errorf("expected '%s', got '%s'", c.expected, sev)
			}
			if !sev.IsValid() {
				t.Errorf("expected severity '%s' to be valid", sev)
			}
		} else {
			if err == nil {
				t.Errorf("expected error for '%s', got nil", c.input)
			}
		}
	}
}

func TestDocumentStatus_Parsing(t *testing.T) {
	p, err := ParseDocumentStatus("pending")
	if err != nil || p.IsIndexed() {
		t.Errorf("expected pending status, got: %v", err)
	}

	idx, err := ParseDocumentStatus("indexed")
	if err != nil || !idx.IsIndexed() {
		t.Errorf("expected indexed status, got: %v", err)
	}

	failed, err := ParseDocumentStatus("failed")
	if err != nil || failed.IsIndexed() {
		t.Errorf("expected failed status, got: %v", err)
	}

	_, err = ParseDocumentStatus("unknown")
	if err == nil {
		t.Error("expected error for unknown status, got nil")
	}
}

func TestCitationSnapshot_Validation(t *testing.T) {
	chunkID := uuid.New()
	cit, err := NewCitationSnapshot(chunkID, "db_runbook.md", "SELECT * FROM locks", "pg_stat_activity")
	if err != nil {
		t.Fatalf("failed creating citation snapshot: %v", err)
	}
	if cit.ChunkID != chunkID || cit.DocumentTitle != "db_runbook.md" {
		t.Error("citation snapshot field mismatch")
	}

	// Nil chunk ID error
	if _, err := NewCitationSnapshot(uuid.Nil, "title", "snippet", ""); err == nil {
		t.Error("expected error for nil chunk id, got nil")
	}

	// Empty snippet error
	if _, err := NewCitationSnapshot(chunkID, "title", "", ""); err == nil {
		t.Error("expected error for empty snippet, got nil")
	}
}

func TestStage_Validation(t *testing.T) {
	for i := 1; i <= 3; i++ {
		s, err := ParseStage(i)
		if err != nil || s.Int() != i {
			t.Errorf("expected valid stage %d, got err: %v", i, err)
		}
	}

	for _, invalid := range []int{0, 4, -1} {
		if _, err := ParseStage(invalid); err == nil {
			t.Errorf("expected error for stage %d, got nil", invalid)
		}
	}
}

func TestEmbeddingVector_Dimensions(t *testing.T) {
	valid := make([]float32, ExpectedEmbeddingDimensions)
	valid[0] = 1.0

	vec, err := NewEmbeddingVector(valid)
	if err != nil {
		t.Fatalf("expected valid embedding vector: %v", err)
	}
	if len(vec.Slice()) != ExpectedEmbeddingDimensions {
		t.Errorf("expected %d elements, got %d", ExpectedEmbeddingDimensions, len(vec.Slice()))
	}

	// Under-dimensioned
	invalid := make([]float32, 100)
	if _, err := NewEmbeddingVector(invalid); err == nil {
		t.Error("expected error for 100-dim vector, got nil")
	}

	// Self-similarity
	sim, err := vec.CosineSimilarity(vec)
	if err != nil {
		t.Fatalf("failed cosine similarity: %v", err)
	}
	if sim < 0.999 {
		t.Errorf("expected self-similarity ~1.0, got %f", sim)
	}
}

func TestIssueClassification_ParsingAndValidation(t *testing.T) {
	cases := []struct {
		input    string
		expected IssueClassification
		valid    bool
	}{
		{"code", IssueClassificationCode, true},
		{"CODE", IssueClassificationCode, true},
		{"software", IssueClassificationCode, true},
		{"infra", IssueClassificationInfra, true},
		{"infrastructure", IssueClassificationInfra, true},
		{"hybrid", IssueClassificationHybrid, true},
		{"both", IssueClassificationHybrid, true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, c := range cases {
		classification, err := ParseIssueClassification(c.input)
		if c.valid {
			if err != nil {
				t.Errorf("expected valid classification for '%s': %v", c.input, err)
			}
			if classification != c.expected {
				t.Errorf("expected '%s', got '%s'", c.expected, classification)
			}
			if !classification.IsValid() {
				t.Errorf("expected classification '%s' to be valid", classification)
			}
		} else {
			if err == nil {
				t.Errorf("expected error for '%s', got nil", c.input)
			}
		}
	}
}

func TestTriggerType_Validation(t *testing.T) {
	cases := []struct {
		input     string
		expected  TriggerType
		valid     bool
		isWebhook bool
	}{
		{"slash_command", TriggerTypeSlashCommand, true, false},
		{"SLASH_COMMAND", TriggerTypeSlashCommand, true, false},
		{"webhook", TriggerTypeWebhook, true, true},
		{" WEBHOOK ", TriggerTypeWebhook, true, true},
		{"manual_api", "", false, false},
		{"", "", false, false},
	}

	for _, c := range cases {
		tt, err := ParseTriggerType(c.input)
		if c.valid {
			if err != nil {
				t.Errorf("unexpected error for '%s': %v", c.input, err)
			}
			if tt != c.expected {
				t.Errorf("expected '%s', got '%s'", c.expected, tt)
			}
			if tt.IsWebhook() != c.isWebhook {
				t.Errorf("expected isWebhook=%v, got %v", c.isWebhook, tt.IsWebhook())
			}
			if !tt.IsValid() {
				t.Errorf("expected trigger type '%s' to be valid", tt)
			}
			if tt.String() != string(c.expected) {
				t.Errorf("expected string '%s', got '%s'", c.expected, tt.String())
			}
		} else {
			if err == nil {
				t.Errorf("expected error for '%s', got nil", c.input)
			}
		}
	}
}

func TestIncidentContext_Lifecycle(t *testing.T) {
	empty := NewIncidentContext("", "", nil, nil, nil)
	if !empty.IsEmpty() {
		t.Error("expected context to be empty")
	}
	if empty.Provider() != "manual" {
		t.Errorf("expected default provider 'manual', got '%s'", empty.Provider())
	}

	metrics := map[string]float64{"error_rate": 0.15, "latency_p99": 450.0}
	ctx := NewIncidentContext(
		"Grafana",
		"High5xxErrors",
		[]string{"HTTP 504 Gateway Timeout", "connection refused"},
		[]string{"panic: runtime error at handler.go:42"},
		metrics,
	)

	if ctx.IsEmpty() {
		t.Error("expected context to not be empty")
	}
	if ctx.Provider() != "grafana" {
		t.Errorf("expected provider 'grafana', got '%s'", ctx.Provider())
	}
	if ctx.AlertName() != "High5xxErrors" {
		t.Errorf("expected alert name 'High5xxErrors', got '%s'", ctx.AlertName())
	}
	if len(ctx.ErrorLogs()) != 2 {
		t.Errorf("expected 2 error logs, got %d", len(ctx.ErrorLogs()))
	}
	if len(ctx.StackTraces()) != 1 {
		t.Errorf("expected 1 stack trace, got %d", len(ctx.StackTraces()))
	}
	if ctx.Metrics()["error_rate"] != 0.15 {
		t.Errorf("expected metric error_rate=0.15, got %f", ctx.Metrics()["error_rate"])
	}

	// Test immutability via defensive copies
	logs := ctx.ErrorLogs()
	logs[0] = "mutated"
	if ctx.ErrorLogs()[0] == "mutated" {
		t.Error("defensive copy violated for error logs")
	}

	m := ctx.Metrics()
	m["error_rate"] = 999.0
	if ctx.Metrics()["error_rate"] == 999.0 {
		t.Error("defensive copy violated for metrics")
	}

	// JSON serialization round-trip
	bytes, err := ctx.MarshalJSON()
	if err != nil {
		t.Fatalf("failed marshaling IncidentContext: %v", err)
	}

	var restored IncidentContext
	if err := restored.UnmarshalJSON(bytes); err != nil {
		t.Fatalf("failed unmarshaling IncidentContext: %v", err)
	}

	if restored.Provider() != ctx.Provider() {
		t.Errorf("provider mismatch after unmarshal: %s vs %s", restored.Provider(), ctx.Provider())
	}
	if restored.AlertName() != ctx.AlertName() {
		t.Errorf("alert name mismatch after unmarshal: %s vs %s", restored.AlertName(), ctx.AlertName())
	}
	if len(restored.ErrorLogs()) != 2 || restored.ErrorLogs()[0] != "HTTP 504 Gateway Timeout" {
		t.Errorf("error logs mismatch after unmarshal: %v", restored.ErrorLogs())
	}
}
