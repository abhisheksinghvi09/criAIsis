package handler_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/value"
	"criaisis/internal/handler"
	"criaisis/internal/infrastructure/crypto"
	"criaisis/internal/service/incident"
	"criaisis/internal/service/orchestrator"
	"criaisis/internal/service/sandbox"
	"criaisis/internal/service/tenant"

	"github.com/rs/zerolog"
)

// TestSecurityRegression_US7_1 validates that multi-tenant isolation, signature verification,
// and rate limiting are strictly enforced across all operational paths.
func TestSecurityRegression_US7_1(t *testing.T) {
	logger := zerolog.Nop()
	cipher, _ := crypto.New("01234567890123456789012345678901")
	wsRepo := newMockWorkspaceRepo()
	incRepo := newMockIncidentRepo()
	reproRepo := newMockReproRepo()
	prov := &mockProvisioner{}
	queue := &mockQueue{}

	resolver := tenant.NewResolver(nil, cipher)
	incSvc := incident.New(incRepo, resolver, func(rt *tenant.Runtime) *orchestrator.Orchestrator { return nil }, queue, &logger)
	sandboxSvc := sandbox.NewService(reproRepo, incRepo, prov, &logger)

	slClient := &mockSlackClient{}
	inboundH := handler.NewSlackInboundHandler(
		wsRepo, incRepo, incSvc, resolver, nil, cipher, slClient,
		"client-id", "client-secret", &logger,
	)

	// Create Tenant A and Tenant B
	wsA, _ := entity.NewWorkspace("TEAM_A", "Tenant Alpha", []byte("enc-a"))
	wsB, _ := entity.NewWorkspace("TEAM_B", "Tenant Beta", []byte("enc-b"))
	_ = wsRepo.Create(context.Background(), wsA)
	_ = wsRepo.Create(context.Background(), wsB)

	chanID, _ := value.ParseSlackChannelID("C123")
	threadTS, _ := value.ParseSlackThreadTS("123.456")
	userID, _ := value.ParseSlackUserID("U123")

	incA, _ := entity.NewIncident(wsA.ID(), "Incident Alpha", "desc", chanID, threadTS, value.SeveritySev3, userID, nil)
	_ = incA.Resolve(time.Now().UTC())
	_ = incRepo.Create(context.Background(), incA)

	t.Run("BR7.1: Tenant B cannot access Tenant A's sandbox reproduction", func(t *testing.T) {
		// Tenant A triggers reproduction
		srA, err := sandboxSvc.Trigger(context.Background(), wsA.ID(), incA.ID())
		if err != nil {
			t.Fatalf("trigger failed: %v", err)
		}

		// Tenant B attempts to read Tenant A's reproduction. The repository now
		// enforces this in the query itself (WHERE workspace_id = $1 AND id = $2),
		// so the row is simply not found rather than found-then-rejected.
		_, err = sandboxSvc.Get(context.Background(), wsB.ID(), srA.ID())
		if err == nil {
			t.Error("expected Tenant B to be denied Tenant A's reproduction, got no error")
		}

		// Tenant B attempts to select scenario on Tenant A's reproduction
		_, err = sandboxSvc.SelectScenario(context.Background(), wsB.ID(), srA.ID(), "oom_crashloop")
		if err == nil {
			t.Error("expected Tenant B to be denied Tenant A's reproduction, got no error")
		}
	})

	t.Run("NFR1 & BR0.1: Signature tampering rejected", func(t *testing.T) {
		secret := "my-secret-signing"
		mw := handler.NewSlackSignatureMiddleware(secret, &logger)

		body := "command=%2Fcriaisis&text=investigate+leak"
		ts := fmt.Sprintf("%d", time.Now().UTC().Unix())
		baseString := fmt.Sprintf("v0:%s:%s", ts, body)

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(baseString))
		validSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

		// Tampered body with valid signature for original body
		tamperedBody := "command=%2Fcriaisis&text=investigate+leak+MODIFIED"
		req := httptest.NewRequest("POST", "/slack/commands", strings.NewReader(tamperedBody))
		req.Header.Set("X-Slack-Request-Timestamp", ts)
		req.Header.Set("X-Slack-Signature", validSig)
		rec := httptest.NewRecorder()

		mw.Handler(http.HandlerFunc(inboundH.HandleCommand)).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for tampered payload, got %d", rec.Code)
		}
	})

	t.Run("NFR2 & BR0.3: Rate limit boundary per workspace", func(t *testing.T) {
		limit := 30
		rl := handler.NewWorkspaceRateLimitMiddleware(limit, time.Minute, &logger)

		dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// 30 requests from Team A
		for i := 0; i < limit; i++ {
			req := httptest.NewRequest("POST", "/slack/commands?team_id=TEAM_A", strings.NewReader(""))
			rec := httptest.NewRecorder()
			rl.Handler(dummyHandler).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("req %d failed unexpectedly: %d", i+1, rec.Code)
			}
		}

		// 31st request from Team A is rejected
		reqA := httptest.NewRequest("POST", "/slack/commands?team_id=TEAM_A", strings.NewReader(""))
		recA := httptest.NewRecorder()
		rl.Handler(dummyHandler).ServeHTTP(recA, reqA)
		if recA.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429 for Team A, got %d", recA.Code)
		}

		// Team B is not affected by Team A's rate limit exhaustion
		reqB := httptest.NewRequest("POST", "/slack/commands?team_id=TEAM_B", strings.NewReader(""))
		recB := httptest.NewRecorder()
		rl.Handler(dummyHandler).ServeHTTP(recB, reqB)
		if recB.Code != http.StatusOK {
			t.Errorf("expected 200 for Team B, got %d", recB.Code)
		}
	})
}
