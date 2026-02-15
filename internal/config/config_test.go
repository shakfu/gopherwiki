package config

import (
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.SiteName != "GopherWiki" {
		t.Errorf("SiteName = %q, want %q", cfg.SiteName, "GopherWiki")
	}
	if cfg.ReadAccess != "ANONYMOUS" {
		t.Errorf("ReadAccess = %q, want %q", cfg.ReadAccess, "ANONYMOUS")
	}
	if cfg.WriteAccess != "ANONYMOUS" {
		t.Errorf("WriteAccess = %q, want %q", cfg.WriteAccess, "ANONYMOUS")
	}
	if cfg.AttachmentAccess != "ANONYMOUS" {
		t.Errorf("AttachmentAccess = %q, want %q", cfg.AttachmentAccess, "ANONYMOUS")
	}
	if cfg.SecretKey != "CHANGE ME" {
		t.Errorf("SecretKey = %q, want %q", cfg.SecretKey, "CHANGE ME")
	}
	if cfg.DatabaseURI != "sqlite:///:memory:" {
		t.Errorf("DatabaseURI = %q, want %q", cfg.DatabaseURI, "sqlite:///:memory:")
	}
	if cfg.SiteLang != "en" {
		t.Errorf("SiteLang = %q, want %q", cfg.SiteLang, "en")
	}
	if !cfg.AutoApproval {
		t.Error("AutoApproval should default to true")
	}
	if !cfg.MinifyHTML {
		t.Error("MinifyHTML should default to true")
	}
	if cfg.MaxFormMemorySize != 1_000_000 {
		t.Errorf("MaxFormMemorySize = %d, want %d", cfg.MaxFormMemorySize, 1_000_000)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "INFO")
	}
}

func TestLoadFromEnv(t *testing.T) {
	cfg := Default()

	t.Setenv("SECRET_KEY", "my-super-secret-key-1234")
	t.Setenv("READ_ACCESS", "REGISTERED")
	t.Setenv("SITE_NAME", "TestWiki")
	t.Setenv("REPOSITORY", "/tmp/testrepo")
	t.Setenv("DATABASE_URI", "sqlite:///test.db")

	cfg.LoadFromEnv()

	if cfg.SecretKey != "my-super-secret-key-1234" {
		t.Errorf("SecretKey = %q, want %q", cfg.SecretKey, "my-super-secret-key-1234")
	}
	if cfg.ReadAccess != "REGISTERED" {
		t.Errorf("ReadAccess = %q, want %q", cfg.ReadAccess, "REGISTERED")
	}
	if cfg.SiteName != "TestWiki" {
		t.Errorf("SiteName = %q, want %q", cfg.SiteName, "TestWiki")
	}
	if cfg.Repository != "/tmp/testrepo" {
		t.Errorf("Repository = %q, want %q", cfg.Repository, "/tmp/testrepo")
	}
	if cfg.DatabaseURI != "sqlite:///test.db" {
		t.Errorf("DatabaseURI = %q, want %q", cfg.DatabaseURI, "sqlite:///test.db")
	}
}

func TestLoadFromEnvBool(t *testing.T) {
	tests := []struct {
		envVal string
		want   bool
	}{
		{"true", true},
		{"yes", true},
		{"on", true},
		{"1", true},
		{"TRUE", true},
		{"Yes", true},
		{"false", false},
		{"no", false},
		{"0", false},
		{"", false}, // empty falls back to default (false for Debug)
	}

	for _, tt := range tests {
		t.Run("DEBUG="+tt.envVal, func(t *testing.T) {
			cfg := Default()
			cfg.Debug = false // ensure default is false
			if tt.envVal != "" {
				t.Setenv("DEBUG", tt.envVal)
			}
			cfg.LoadFromEnv()
			if cfg.Debug != tt.want {
				t.Errorf("Debug with DEBUG=%q: got %v, want %v", tt.envVal, cfg.Debug, tt.want)
			}
		})
	}
}

func TestLoadFromEnvInt(t *testing.T) {
	cfg := Default()

	t.Setenv("MAIL_PORT", "587")
	t.Setenv("MAX_FORM_MEMORY_SIZE", "5000000")

	cfg.LoadFromEnv()

	if cfg.MailPort != 587 {
		t.Errorf("MailPort = %d, want %d", cfg.MailPort, 587)
	}
	if cfg.MaxFormMemorySize != 5_000_000 {
		t.Errorf("MaxFormMemorySize = %d, want %d", cfg.MaxFormMemorySize, 5_000_000)
	}
}

func TestLoadFromEnvInt_Invalid(t *testing.T) {
	cfg := Default()
	t.Setenv("MAIL_PORT", "notanumber")
	cfg.LoadFromEnv()

	// Should fall back to default
	if cfg.MailPort != 0 {
		t.Errorf("MailPort = %d, want %d (default)", cfg.MailPort, 0)
	}
}

func TestValidate_Valid(t *testing.T) {
	cfg := Default()
	cfg.SecretKey = "a-long-secret-key-here"
	cfg.Repository = t.TempDir()

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() returned error for valid config: %v", err)
	}
}

func TestValidate_ShortSecretKey(t *testing.T) {
	cfg := Default()
	cfg.SecretKey = "short"
	cfg.Repository = t.TempDir()

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should return error for short SecretKey")
	}
}

func TestValidate_DefaultSecretKey(t *testing.T) {
	cfg := Default()
	cfg.Repository = t.TempDir()
	// SecretKey defaults to "CHANGE ME"

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should return error for default SecretKey")
	}
}

func TestValidate_MissingRepository(t *testing.T) {
	cfg := Default()
	cfg.SecretKey = "a-long-secret-key-here"
	cfg.Repository = ""

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should return error for empty Repository")
	}
}

func TestValidate_NonexistentRepository(t *testing.T) {
	cfg := Default()
	cfg.SecretKey = "a-long-secret-key-here"
	cfg.Repository = "/nonexistent/path/that/does/not/exist"

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should return error for nonexistent Repository")
	}
}

func TestLoad(t *testing.T) {
	// Load() returns defaults + env. Just verify it doesn't panic and returns non-nil.
	cfg := Load()
	if cfg == nil {
		t.Fatal("Load() returned nil")
	}
	if cfg.SiteName == "" {
		t.Error("Load() should set default SiteName")
	}
}

func TestDevMode_Default(t *testing.T) {
	cfg := Default()
	if cfg.DevMode {
		t.Error("DevMode should default to false")
	}
}

func TestDevMode_FromEnv(t *testing.T) {
	cfg := Default()
	t.Setenv("DEV_MODE", "true")
	cfg.LoadFromEnv()
	if !cfg.DevMode {
		t.Error("DevMode should be true when DEV_MODE=true")
	}
}

func TestValidate_DevMode_SkipsSecretKey(t *testing.T) {
	cfg := Default()
	cfg.DevMode = true
	cfg.Repository = t.TempDir()
	// SecretKey is still "CHANGE ME" (default), which normally fails validation

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should pass with DevMode=true and weak SecretKey, got: %v", err)
	}
}

func TestValidate_ProductionMode_RejectsWeakSecretKey(t *testing.T) {
	cfg := Default()
	cfg.DevMode = false
	cfg.Repository = t.TempDir()
	// SecretKey is "CHANGE ME"

	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject weak SecretKey when DevMode=false")
	}
}
