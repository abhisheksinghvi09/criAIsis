package llm

import (
	"context"
	"testing"
)

// validateEmbeddingBaseURL is the only gate between a tenant's setup form and an
// outbound request to wherever they name, so it must reject the ways that
// request could land inside the platform's own network.
func TestValidateEmbeddingBaseURL_RejectsNonRoutableTargets(t *testing.T) {
	cases := []string{
		"http://api.openai.com/v1",   // not https
		"https://127.0.0.1/v1",       // loopback
		"https://169.254.169.254/v1", // cloud metadata (link-local)
		"https://10.0.0.5/v1",        // private
		"https://192.168.1.1/v1",     // private
		"https://0.0.0.0/v1",         // unspecified
		"not a url at all",
		"",
	}
	for _, raw := range cases {
		if err := validateEmbeddingBaseURL(context.Background(), raw); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}

func TestValidateEmbeddingBaseURL_AcceptsPublicAddress(t *testing.T) {
	// A public IP literal needs no DNS lookup, keeping this test hermetic.
	if err := validateEmbeddingBaseURL(context.Background(), "https://8.8.8.8/v1"); err != nil {
		t.Errorf("a public address was rejected: %v", err)
	}
}
