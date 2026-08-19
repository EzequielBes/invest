// analysis/internal/llm/select_test.go
package llm

import (
	"context"
	"errors"
	"testing"
)

type fakeClient struct {
	narrative string
	err       error
	calls     int
}

func (f *fakeClient) Summarize(context.Context, string, string) (string, error) {
	f.calls++
	return f.narrative, f.err
}

func TestFallbackClient_PrimarySucceedsNeverCallsSecondary(t *testing.T) {
	primary := &fakeClient{narrative: "from primary"}
	secondary := &fakeClient{narrative: "from secondary"}
	client := &FallbackClient{primary: primary, secondary: secondary}

	got, err := client.Summarize(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got != "from primary" {
		t.Fatalf("narrative = %q, want from primary", got)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary.calls = %d, want 0 (primary succeeded)", secondary.calls)
	}
}

func TestFallbackClient_PrimaryFailsCallsSecondary(t *testing.T) {
	primary := &fakeClient{err: errors.New("primary down")}
	secondary := &fakeClient{narrative: "from secondary"}
	client := &FallbackClient{primary: primary, secondary: secondary}

	got, err := client.Summarize(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got != "from secondary" {
		t.Fatalf("narrative = %q, want from secondary", got)
	}
}

func TestFallbackClient_BothFailReturnsSecondaryError(t *testing.T) {
	primary := &fakeClient{err: errors.New("primary down")}
	secondary := &fakeClient{err: errors.New("secondary down")}
	client := &FallbackClient{primary: primary, secondary: secondary}

	_, err := client.Summarize(context.Background(), "sys", "user")
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
