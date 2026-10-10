package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// defaultMaxTokens is generous enough that a specialist hypothesis with citations
// is never truncated mid-sentence, which would break JSON parsing.
const defaultMaxTokens int64 = 4096

// AnthropicProvider serves the clash engine from the Claude Messages API.
type AnthropicProvider struct {
	client          anthropic.Client
	specialistModel string
	synthesisModel  string
}

var _ ChatProvider = (*AnthropicProvider)(nil)

// AnthropicOptions are one tenant's debate credentials and model choices.
type AnthropicOptions struct {
	APIKey          string
	SpecialistModel string
	SynthesisModel  string
}

// NewAnthropic constructs a Claude-backed chat provider for a single tenant.
func NewAnthropic(opts AnthropicOptions) *AnthropicProvider {
	return &AnthropicProvider{
		client:          anthropic.NewClient(option.WithAPIKey(opts.APIKey)),
		specialistModel: opts.SpecialistModel,
		synthesisModel:  opts.SynthesisModel,
	}
}

// Complete issues a single grounded completion, returning the assistant text.
func (p *AnthropicProvider) Complete(ctx context.Context, req ChatRequest) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:        p.modelFor(req.Role),
		MaxTokens:    maxTokens,
		OutputConfig: p.outputConfig(req),
		System:       []anthropic.TextBlockParam{{Text: req.System}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.User)),
		},
	}

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("claude completion (%s): %w", req.Role, err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("claude refused the %s request: %s", req.Role, resp.StopDetails.Explanation)
	}

	return collectText(resp), nil
}

// modelFor maps the operational role onto its configured model.
func (p *AnthropicProvider) modelFor(role Role) string {
	if role == RoleSynthesis {
		return p.synthesisModel
	}
	return p.specialistModel
}

// outputConfig trades reasoning depth against the stage latency budget, and pins
// the response to a JSON schema when the caller supplied one.
func (p *AnthropicProvider) outputConfig(req ChatRequest) anthropic.OutputConfigParam {
	cfg := anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortHigh}
	if req.Role == RoleSpecialist {
		// Stage 1 runs four-wide against a <10s budget; depth belongs to Stage 2.
		cfg.Effort = anthropic.OutputConfigEffortLow
	}
	if req.Schema != nil {
		cfg.Format = anthropic.JSONOutputFormatParam{Schema: req.Schema}
	}
	return cfg
}

// collectText concatenates the text blocks of a response, skipping thinking blocks.
func collectText(resp *anthropic.Message) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String()
}
