package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"criaisis/internal/domain/value"
)

// embeddingTimeout bounds a single embeddings round trip. Ingestion is a background
// job, so this is tuned for reliability over latency.
const embeddingTimeout = 60 * time.Second

// OpenAIEmbedder calls any OpenAI-compatible /embeddings endpoint. Only one endpoint
// shape is involved, so a raw HTTP call is cheaper than a vendor SDK dependency.
type OpenAIEmbedder struct {
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
}

var _ EmbeddingProvider = (*OpenAIEmbedder)(nil)

// EmbeddingOptions are one tenant's retrieval credentials and endpoint.
type EmbeddingOptions struct {
	APIKey  string
	Model   string
	BaseURL string
}

// NewOpenAIEmbedder constructs an embeddings client for a single tenant.
func NewOpenAIEmbedder(opts EmbeddingOptions) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		client:  &http.Client{Timeout: embeddingTimeout},
		baseURL: strings.TrimSuffix(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		model:   opts.Model,
	}
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed vectorizes a batch of texts, preserving input order.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([]value.EmbeddingVector, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := e.post(ctx, embeddingRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: expected %d vectors, got %d", len(texts), len(resp.Data))
	}

	// The API documents index-ordered data but does not guarantee it; place by index.
	vectors := make([]value.EmbeddingVector, len(texts))
	for _, item := range resp.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			return nil, fmt.Errorf("embeddings: response index %d out of range", item.Index)
		}
		vec, err := value.NewEmbeddingVector(item.Embedding)
		if err != nil {
			return nil, fmt.Errorf("embeddings: %w", err)
		}
		vectors[item.Index] = vec
	}
	return vectors, nil
}

// post executes the embeddings call and decodes the payload.
func (e *OpenAIEmbedder) post(ctx context.Context, body embeddingRequest) (*embeddingResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embeddings: marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("embeddings: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	httpResp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request failed: %w", err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("embeddings: reading response: %w", err)
	}

	var decoded embeddingResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("embeddings: decoding response (status %d): %w", httpResp.StatusCode, err)
	}
	if httpResp.StatusCode != http.StatusOK {
		if decoded.Error != nil {
			return nil, fmt.Errorf("embeddings: status %d: %s", httpResp.StatusCode, decoded.Error.Message)
		}
		return nil, fmt.Errorf("embeddings: status %d", httpResp.StatusCode)
	}
	return &decoded, nil
}
