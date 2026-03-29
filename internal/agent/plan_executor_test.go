package agent

import (
	"strings"
	"testing"
)

func TestValidateCallbackURLRejectsLocalhostByDefault(t *testing.T) {
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACK", "")
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACKS", "")

	err := validateCallbackURL("http://localhost:5299/api/ai-cases/4/status")
	if err == nil {
		t.Fatal("expected localhost callback to be rejected by default")
	}
	if !strings.Contains(err.Error(), "localhost") {
		t.Fatalf("expected localhost rejection error, got: %v", err)
	}
}

func TestValidateCallbackURLAllowsLocalhostWithSingularEnvVar(t *testing.T) {
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACK", "1")
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACKS", "")

	err := validateCallbackURL("http://localhost:5299/api/ai-cases/4/status")
	if err != nil {
		t.Fatalf("expected localhost callback to be allowed, got error: %v", err)
	}
}

func TestValidateCallbackURLAllowsLocalhostWithPluralEnvVar(t *testing.T) {
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACK", "")
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACKS", "1")

	err := validateCallbackURL("http://localhost:5299/api/ai-cases/4/status")
	if err != nil {
		t.Fatalf("expected localhost callback to be allowed, got error: %v", err)
	}
}

func TestValidateCallbackURLAllowsLazyAdminLoopbackWithoutEnvVar(t *testing.T) {
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACK", "")
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACKS", "")

	err := validateCallbackURLForOrigin("lazyadmin", "http://localhost:5299/api/ai-cases/4/status")
	if err != nil {
		t.Fatalf("expected localhost callback for lazyadmin to be allowed, got error: %v", err)
	}
}

func TestValidateCallbackURLRejectsLazyAdminPrivateIPWithoutEnvVar(t *testing.T) {
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACK", "")
	t.Setenv("CDEV_ALLOW_LOCAL_CALLBACKS", "")

	err := validateCallbackURLForOrigin("lazyadmin", "http://192.168.1.10:5299/api/ai-cases/4/status")
	if err == nil {
		t.Fatal("expected private IP callback for lazyadmin to still be rejected")
	}
	if !strings.Contains(err.Error(), "192.168.1.10") {
		t.Fatalf("expected private IP rejection error, got: %v", err)
	}
}
