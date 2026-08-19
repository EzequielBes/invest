// execution/internal/binanceclient/client_test.go
package binanceclient

import "testing"

func TestSign_DeterministicAndSecretSensitive(t *testing.T) {
	c1 := &Client{secret: "secret-a"}
	c2 := &Client{secret: "secret-b"}

	sigA1 := c1.sign("symbol=BTCUSDT&timestamp=123")
	sigA2 := c1.sign("symbol=BTCUSDT&timestamp=123")
	sigB := c2.sign("symbol=BTCUSDT&timestamp=123")

	if sigA1 != sigA2 {
		t.Errorf("sign is not deterministic: %q != %q", sigA1, sigA2)
	}
	if sigA1 == sigB {
		t.Error("sign did not change with a different secret")
	}
	if len(sigA1) != 64 {
		t.Errorf("signature length = %d, want 64 (hex-encoded SHA-256)", len(sigA1))
	}
}
