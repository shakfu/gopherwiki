package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	content := `
site_name: "My Wiki"
port: 9090
host: "127.0.0.1"
repository_path: "/data/wiki"
log_level: "DEBUG"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error: %v", err)
	}

	if fc.SiteName == nil || *fc.SiteName != "My Wiki" {
		t.Errorf("SiteName = %v, want 'My Wiki'", fc.SiteName)
	}
	if fc.Port == nil || *fc.Port != 9090 {
		t.Errorf("Port = %v, want 9090", fc.Port)
	}
	if fc.Host == nil || *fc.Host != "127.0.0.1" {
		t.Errorf("Host = %v, want '127.0.0.1'", fc.Host)
	}
	if fc.Repository == nil || *fc.Repository != "/data/wiki" {
		t.Errorf("Repository = %v, want '/data/wiki'", fc.Repository)
	}
	if fc.LogLevel == nil || *fc.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %v, want 'DEBUG'", fc.LogLevel)
	}
	// Unset fields should be nil
	if fc.DevMode != nil {
		t.Errorf("DevMode = %v, want nil", fc.DevMode)
	}
	if fc.SecretKey != nil {
		t.Errorf("SecretKey = %v, want nil", fc.SecretKey)
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/config.yml")
	if err == nil {
		t.Fatal("LoadFromFile() should error for nonexistent file")
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte("{{bad yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("LoadFromFile() should error for invalid YAML")
	}
}

func TestLoadFromFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error: %v", err)
	}
	// All fields should be nil
	if fc.Port != nil {
		t.Errorf("Port = %v, want nil", fc.Port)
	}
	if fc.SiteName != nil {
		t.Errorf("SiteName = %v, want nil", fc.SiteName)
	}
}

func TestApplyTo(t *testing.T) {
	cfg := Default()

	siteName := "Test Wiki"
	port := 3000
	logLevel := "WARN"
	regEnabled := false

	fc := &FileConfig{
		SiteName:            &siteName,
		Port:                &port,
		LogLevel:            &logLevel,
		DisableRegistration: &regEnabled,
	}

	fc.applyTo(cfg)

	if cfg.SiteName != "Test Wiki" {
		t.Errorf("SiteName = %q, want 'Test Wiki'", cfg.SiteName)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.LogLevel != "WARN" {
		t.Errorf("LogLevel = %q, want 'WARN'", cfg.LogLevel)
	}
	// registration_enabled: false -> DisableRegistration: true
	if !cfg.DisableRegistration {
		t.Error("DisableRegistration should be true when registration_enabled is false")
	}
	// Unset fields should retain defaults
	if cfg.ReadAccess != "ANONYMOUS" {
		t.Errorf("ReadAccess = %q, want 'ANONYMOUS' (default)", cfg.ReadAccess)
	}
}

func TestApplyTo_RegistrationEnabled(t *testing.T) {
	cfg := Default()
	cfg.DisableRegistration = true

	regEnabled := true
	fc := &FileConfig{DisableRegistration: &regEnabled}
	fc.applyTo(cfg)

	if cfg.DisableRegistration {
		t.Error("DisableRegistration should be false when registration_enabled is true")
	}
}

func TestApplyTo_AllFields(t *testing.T) {
	cfg := Default()

	port := 4000
	host := "0.0.0.0"
	siteURL := "https://wiki.example.com"
	devMode := true
	repo := "/opt/wiki"
	dbURI := "sqlite:///custom.db"
	authMethod := "header"
	regEnabled := true
	secret := "super-secret-key-1234"
	autoApproval := false
	readAccess := "REGISTERED"
	writeAccess := "APPROVED"
	attachAccess := "APPROVED"
	siteName := "Corp Wiki"
	homePage := "Dashboard"
	siteLang := "de"
	logLevel := "ERROR"
	logFormat := "json"

	fc := &FileConfig{
		Port:                &port,
		Host:                &host,
		SiteURL:             &siteURL,
		DevMode:             &devMode,
		Repository:          &repo,
		DatabaseURI:         &dbURI,
		AuthMethod:          &authMethod,
		DisableRegistration: &regEnabled,
		SecretKey:           &secret,
		AutoApproval:        &autoApproval,
		ReadAccess:          &readAccess,
		WriteAccess:         &writeAccess,
		AttachmentAccess:    &attachAccess,
		SiteName:            &siteName,
		HomePage:            &homePage,
		SiteLang:            &siteLang,
		LogLevel:            &logLevel,
		LogFormat:           &logFormat,
	}

	fc.applyTo(cfg)

	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want 4000", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want '0.0.0.0'", cfg.Host)
	}
	if cfg.SiteURL != "https://wiki.example.com" {
		t.Errorf("SiteURL = %q, want 'https://wiki.example.com'", cfg.SiteURL)
	}
	if !cfg.DevMode {
		t.Error("DevMode should be true")
	}
	if cfg.Repository != "/opt/wiki" {
		t.Errorf("Repository = %q, want '/opt/wiki'", cfg.Repository)
	}
	if cfg.DatabaseURI != "sqlite:///custom.db" {
		t.Errorf("DatabaseURI = %q, want 'sqlite:///custom.db'", cfg.DatabaseURI)
	}
	if cfg.AuthMethod != "header" {
		t.Errorf("AuthMethod = %q, want 'header'", cfg.AuthMethod)
	}
	if cfg.DisableRegistration {
		t.Error("DisableRegistration should be false when registration_enabled is true")
	}
	if cfg.SecretKey != "super-secret-key-1234" {
		t.Errorf("SecretKey = %q, want 'super-secret-key-1234'", cfg.SecretKey)
	}
	if cfg.AutoApproval {
		t.Error("AutoApproval should be false")
	}
	if cfg.ReadAccess != "REGISTERED" {
		t.Errorf("ReadAccess = %q, want 'REGISTERED'", cfg.ReadAccess)
	}
	if cfg.WriteAccess != "APPROVED" {
		t.Errorf("WriteAccess = %q, want 'APPROVED'", cfg.WriteAccess)
	}
	if cfg.AttachmentAccess != "APPROVED" {
		t.Errorf("AttachmentAccess = %q, want 'APPROVED'", cfg.AttachmentAccess)
	}
	if cfg.SiteName != "Corp Wiki" {
		t.Errorf("SiteName = %q, want 'Corp Wiki'", cfg.SiteName)
	}
	if cfg.HomePage != "Dashboard" {
		t.Errorf("HomePage = %q, want 'Dashboard'", cfg.HomePage)
	}
	if cfg.SiteLang != "de" {
		t.Errorf("SiteLang = %q, want 'de'", cfg.SiteLang)
	}
	if cfg.LogLevel != "ERROR" {
		t.Errorf("LogLevel = %q, want 'ERROR'", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want 'json'", cfg.LogFormat)
	}
}

func TestLoadWithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	content := `
site_name: "File Wiki"
log_level: "WARN"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithFile(path)
	if err != nil {
		t.Fatalf("LoadWithFile() error: %v", err)
	}

	if cfg.SiteName != "File Wiki" {
		t.Errorf("SiteName = %q, want 'File Wiki'", cfg.SiteName)
	}
	if cfg.LogLevel != "WARN" {
		t.Errorf("LogLevel = %q, want 'WARN'", cfg.LogLevel)
	}
	// Defaults should still be present
	if cfg.ReadAccess != "ANONYMOUS" {
		t.Errorf("ReadAccess = %q, want 'ANONYMOUS' (default)", cfg.ReadAccess)
	}
}

func TestLoadWithFile_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	content := `
site_name: "File Wiki"
log_level: "WARN"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Env var should override file value
	t.Setenv("SITE_NAME", "Env Wiki")

	cfg, err := LoadWithFile(path)
	if err != nil {
		t.Fatalf("LoadWithFile() error: %v", err)
	}

	if cfg.SiteName != "Env Wiki" {
		t.Errorf("SiteName = %q, want 'Env Wiki' (env should override file)", cfg.SiteName)
	}
	// File value should still apply where no env var is set
	if cfg.LogLevel != "WARN" {
		t.Errorf("LogLevel = %q, want 'WARN' (from file)", cfg.LogLevel)
	}
}

func TestLoadWithFile_NotFound(t *testing.T) {
	_, err := LoadWithFile("/nonexistent/config.yml")
	if err == nil {
		t.Fatal("LoadWithFile() should error for nonexistent file")
	}
}
