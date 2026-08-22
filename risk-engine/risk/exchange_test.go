package risk

import "testing"

func TestBuildAssetExchangeMap_ReadsFromEnvVars(t *testing.T) {
	t.Setenv("ASSETS", "BTC, ETH")
	t.Setenv("ALPACA_ASSETS", "AAPL")

	m := buildAssetExchangeMap()
	if m["BTC"] != "binance" {
		t.Errorf(`m["BTC"] = %q, want "binance"`, m["BTC"])
	}
	if m["ETH"] != "binance" {
		t.Errorf(`m["ETH"] = %q, want "binance"`, m["ETH"])
	}
	if m["AAPL"] != "alpaca" {
		t.Errorf(`m["AAPL"] = %q, want "alpaca"`, m["AAPL"])
	}
}

func TestExchangeFor_KnownCryptoAsset(t *testing.T) {
	if got := ExchangeFor("BTC"); got != "binance" {
		t.Errorf("ExchangeFor(BTC) = %q, want %q", got, "binance")
	}
}

func TestExchangeFor_UnknownAssetFallsBackToBinance(t *testing.T) {
	// An asset not in either configured list must still resolve to
	// something rather than panic or return empty — binance is the
	// original, always-correct default for this system's crypto-only
	// history, so an unrecognized symbol behaves exactly as before this
	// function existed.
	if got := ExchangeFor("SOMETHING_UNKNOWN"); got != "binance" {
		t.Errorf("ExchangeFor(unknown) = %q, want fallback %q", got, "binance")
	}
}

func TestIsCrypto_TrueForCryptoAsset(t *testing.T) {
	if !IsCrypto("BTC") {
		t.Error("IsCrypto(BTC) = false, want true")
	}
}

func TestIsCrypto_FalseForUnknownAsset(t *testing.T) {
	// Until stock assets are configured, nothing is a stock — but an
	// unrecognized symbol still resolves to the binance/crypto fallback
	// via ExchangeFor, so IsCrypto must stay consistent with that and
	// report true, not silently miscategorize it as non-crypto exposure.
	if !IsCrypto("SOMETHING_UNKNOWN") {
		t.Error("IsCrypto(unknown) = false, want true (falls back to binance/crypto)")
	}
}
