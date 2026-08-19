package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	model     = "claude-sonnet-5"
	maxTokens = 512
	toolName  = "record_decision"
)

// Decision is the strategist's structured output for one asset. Side is
// "buy", "sell", or "hold"; SizingPct is only meaningful when Side isn't
// "hold".
type Decision struct {
	Side       string  `json:"side"`
	Confidence float64 `json:"confidence"`
	SizingPct  float64 `json:"sizing_pct"`
	Rationale  string  `json:"rationale"`
}

// Client asks the LLM to decide what to do about one asset, given a
// prompt describing its current indicators/narratives.
type Client interface {
	Decide(ctx context.Context, systemPrompt, userPrompt string) (Decision, error)
}

// AnthropicClient is the production implementation. It uses tool use
// (forced via ToolChoice) to get a structured response instead of
// parsing free text.
type AnthropicClient struct {
	client anthropic.Client
}

// NewAnthropicClient reads its API key from ANTHROPIC_API_KEY, per the
// SDK's default credential resolution.
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{client: anthropic.NewClient()}
}

var decisionTool = anthropic.ToolUnionParamOfTool(
	anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"side": map[string]any{
				"type":        "string",
				"enum":        []string{"buy", "sell", "hold"},
				"description": "The proposed action for this asset.",
			},
			"confidence": map[string]any{
				"type":        "number",
				"minimum":     0,
				"maximum":     1,
				"description": "How confident the decision is, from 0 (a guess) to 1 (very confident).",
			},
			"sizing_pct": map[string]any{
				"type":        "number",
				"minimum":     0,
				"maximum":     1,
				"description": "Fraction of total portfolio value to allocate to this trade. Ignored when side is hold.",
			},
			"rationale": map[string]any{
				"type":        "string",
				"description": "A short (2-4 sentence) explanation for the decision.",
			},
		},
		Required: []string{"side", "confidence", "sizing_pct", "rationale"},
	},
	toolName,
)

func (c *AnthropicClient) Decide(ctx context.Context, systemPrompt, userPrompt string) (Decision, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools:      []anthropic.ToolUnionParam{decisionTool},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolName),
	})
	if err != nil {
		return Decision{}, fmt.Errorf("llm: decide: %w", err)
	}
	return decisionFromResponse(resp)
}

func decisionFromResponse(resp *anthropic.Message) (Decision, error) {
	if resp == nil {
		return Decision{}, fmt.Errorf("llm: decide: empty response")
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Decision{}, fmt.Errorf("llm: decide: model refused the request")
	}
	for _, block := range resp.Content {
		toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || toolUse.Name != toolName {
			continue
		}
		var d Decision
		if err := json.Unmarshal(toolUse.Input, &d); err != nil {
			return Decision{}, fmt.Errorf("llm: decide: unmarshal tool input: %w", err)
		}
		if d.Side != "buy" && d.Side != "sell" && d.Side != "hold" {
			return Decision{}, fmt.Errorf("llm: decide: unexpected side %q", d.Side)
		}
		return d, nil
	}
	return Decision{}, fmt.Errorf("llm: decide: no %s tool call in response", toolName)
}
