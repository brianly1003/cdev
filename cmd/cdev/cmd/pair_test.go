package cmd

import (
	"testing"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/pairing"
)

func TestPairPageURL(t *testing.T) {
	info := &pairing.PairingInfo{HTTP: "https://abc123x4-8766.asse.devtunnels.ms"}

	if got := pairPageURL(info, ""); got != "https://abc123x4-8766.asse.devtunnels.ms/pair" {
		t.Fatalf("pairPageURL without token = %q", got)
	}

	if got := pairPageURL(info, "abc123"); got != "https://abc123x4-8766.asse.devtunnels.ms/pair?token=abc123" {
		t.Fatalf("pairPageURL with token = %q", got)
	}
}

func TestResolvePairAccessToken(t *testing.T) {
	t.Setenv("CDEV_TOKEN", "env-token")

	cfg := &config.Config{
		Security: config.SecurityConfig{
			PairAccessToken: "config-token",
		},
	}

	if got := resolvePairAccessToken(cfg); got != "config-token" {
		t.Fatalf("resolvePairAccessToken config precedence = %q", got)
	}

	cfg.Security.PairAccessToken = ""
	if got := resolvePairAccessToken(cfg); got != "env-token" {
		t.Fatalf("resolvePairAccessToken env fallback = %q", got)
	}
}
