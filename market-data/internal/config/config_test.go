package config

import (
	"os"
	"testing"
)

func TestLoad_UsesDefaultAssetsWhenUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	os.Unsetenv("ASSETS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Assets) == 0 {
		t.Fatal("expected non-empty default asset list")
	}
	if cfg.DatabaseURL != "postgres://x/y" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoad_ParsesAssetsFromEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("ASSETS", "BTC, ETH ,SOL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"BTC", "ETH", "SOL"}
	if len(cfg.Assets) != len(want) {
		t.Fatalf("Assets = %v, want %v", cfg.Assets, want)
	}
	for i := range want {
		if cfg.Assets[i] != want[i] {
			t.Errorf("Assets[%d] = %q, want %q", i, cfg.Assets[i], want[i])
		}
	}
}

func TestLoad_ErrorsWithoutDatabaseURL(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is unset")
	}
}
