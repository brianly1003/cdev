package cmd

import (
	"testing"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/pairing"
)

func TestPairPageURL(t *testing.T) {
	info := &pairing.PairingInfo{HTTP: "https://abc123x4-16180.asse.devtunnels.ms"}

	if got := pairPageURL(info, ""); got != "https://abc123x4-16180.asse.devtunnels.ms/pair" {
		t.Fatalf("pairPageURL without token = %q", got)
	}

	if got := pairPageURL(info, "abc123"); got != "https://abc123x4-16180.asse.devtunnels.ms/pair?token=abc123" {
		t.Fatalf("pairPageURL with token = %q", got)
	}
}

func TestResolveCdevAccessToken(t *testing.T) {
	t.Setenv("CDEV_ACCESS_TOKEN", "env-token")

	cfg := &config.Config{
		Security: config.SecurityConfig{
			CdevAccessToken: "config-token",
		},
	}

	if got := resolveCdevAccessToken(cfg); got != "config-token" {
		t.Fatalf("resolveCdevAccessToken config precedence = %q", got)
	}

	cfg.Security.CdevAccessToken = ""
	if got := resolveCdevAccessToken(cfg); got != "env-token" {
		t.Fatalf("resolveCdevAccessToken env fallback = %q", got)
	}
}
