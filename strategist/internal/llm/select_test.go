// strategist/internal/llm/select_test.go
package llm

import (
	"context"
	"errors"
	"testing"
)

type fakeClient struct {
	decision Decision
	err      error
	calls    int
}

func (f *fakeClient) Decide(context.Context, string, string) (Decision, error) {
	f.calls++
	return f.decision, f.err
}

func TestFallbackClient_PrimarySucceedsNeverCallsSecondary(t *testing.T) {
	primary := &fakeClient{decision: Decision{Side: "hold", Rationale: "from primary"}}
	secondary := &fakeClient{decision: Decision{Side: "hold", Rationale: "from secondary"}}
	client := &FallbackClient{primary: primary, secondary: secondary}

	got, err := client.Decide(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Rationale != "from primary" {
		t.Fatalf("rationale = %q, want from primary", got.Rationale)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary.calls = %d, want 0 (primary succeeded)", secondary.calls)
	}
}

func TestFallbackClient_PrimaryFailsCallsSecondary(t *testing.T) {
	primary := &fakeClient{err: errors.New("primary down")}
	secondary := &fakeClient{decision: Decision{Side: "hold", Rationale: "from secondary"}}
	client := &FallbackClient{primary: primary, secondary: secondary}

	got, err := client.Decide(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Rationale != "from secondary" {
		t.Fatalf("rationale = %q, want from secondary", got.Rationale)
	}
}

func TestFallbackClient_BothFailReturnsSecondaryError(t *testing.T) {
	primary := &fakeClient{err: errors.New("primary down")}
	secondary := &fakeClient{err: errors.New("secondary down")}
	client := &FallbackClient{primary: primary, secondary: secondary}

	_, err := client.Decide(context.Background(), "sys", "user")
	if err == nil || err.Error() != "secondary down" {
		t.Fatalf("error = %v, want secondary down", err)
	}
}

func TestNewClient_NoProviderIsError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := NewClient(); err == nil {
		t.Fatal("expected an error with no provider configured, got nil")
	}
}

func TestNewClient_SingleProviderNoFallbackWrapper(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("OPENAI_API_KEY", "")
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := client.(*AnthropicClient); !ok {
		t.Fatalf("client = %T, want *AnthropicClient", client)
	}
}

func TestResolvePrimary_DefaultsToAnthropic(t *testing.T) {
	t.Setenv("LLM_PRIMARY_PROVIDER", "")
	primary, _ := resolvePrimary()
	if _, ok := primary.(*AnthropicClient); !ok {
		t.Fatalf("primary = %T, want *AnthropicClient (default)", primary)
	}
}

func TestResolvePrimary_OpenAIWhenConfigured(t *testing.T) {
	t.Setenv("LLM_PRIMARY_PROVIDER", "openai")
	primary, _ := resolvePrimary()
	if _, ok := primary.(*OpenAIClient); !ok {
		t.Fatalf("primary = %T, want *OpenAIClient", primary)
	}
}
