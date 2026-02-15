package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// FileConfig holds configuration values loaded from a YAML file.
// All fields are pointers so we can distinguish "not set" from zero values.
type FileConfig struct {
	// Server
	Port    *int    `yaml:"port"`
	Host    *string `yaml:"host"`
	SiteURL *string `yaml:"base_url"`
	DevMode *bool   `yaml:"dev_mode"`

	// Storage
	Repository  *string `yaml:"repository_path"`
	DatabaseURI *string `yaml:"database_path"`

	// Auth
	AuthMethod          *string `yaml:"auth_method"`
	DisableRegistration *bool   `yaml:"registration_enabled"`
	SecretKey           *string `yaml:"session_secret"`
	AutoApproval        *bool   `yaml:"auto_approval"`

	// Permissions
	ReadAccess       *string `yaml:"read_access"`
	WriteAccess      *string `yaml:"write_access"`
	AttachmentAccess *string `yaml:"attachment_access"`

	// Wiki
	SiteName *string `yaml:"site_name"`
	HomePage *string `yaml:"landing_page"`
	SiteLang *string `yaml:"site_lang"`

	// Logging
	LogLevel  *string `yaml:"log_level"`
	LogFormat *string `yaml:"log_format"`
}

// LoadFromFile reads and parses a YAML configuration file.
func LoadFromFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var fc FileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &fc, nil
}

// applyTo applies non-nil file config values onto a Config.
func (fc *FileConfig) applyTo(cfg *Config) {
	if fc.Port != nil {
		cfg.Port = *fc.Port
	}
	if fc.Host != nil {
		cfg.Host = *fc.Host
	}
	if fc.SiteURL != nil {
		cfg.SiteURL = *fc.SiteURL
	}
	if fc.DevMode != nil {
		cfg.DevMode = *fc.DevMode
	}
	if fc.Repository != nil {
		cfg.Repository = *fc.Repository
	}
	if fc.DatabaseURI != nil {
		cfg.DatabaseURI = *fc.DatabaseURI
	}
	if fc.AuthMethod != nil {
		cfg.AuthMethod = *fc.AuthMethod
	}
	if fc.DisableRegistration != nil {
		// YAML field is "registration_enabled", so invert
		cfg.DisableRegistration = !*fc.DisableRegistration
	}
	if fc.SecretKey != nil {
		cfg.SecretKey = *fc.SecretKey
	}
	if fc.AutoApproval != nil {
		cfg.AutoApproval = *fc.AutoApproval
	}
	if fc.ReadAccess != nil {
		cfg.ReadAccess = *fc.ReadAccess
	}
	if fc.WriteAccess != nil {
		cfg.WriteAccess = *fc.WriteAccess
	}
	if fc.AttachmentAccess != nil {
		cfg.AttachmentAccess = *fc.AttachmentAccess
	}
	if fc.SiteName != nil {
		cfg.SiteName = *fc.SiteName
	}
	if fc.HomePage != nil {
		cfg.HomePage = *fc.HomePage
	}
	if fc.SiteLang != nil {
		cfg.SiteLang = *fc.SiteLang
	}
	if fc.LogLevel != nil {
		cfg.LogLevel = *fc.LogLevel
	}
	if fc.LogFormat != nil {
		cfg.LogFormat = *fc.LogFormat
	}
}

// LoadWithFile creates a Config by applying defaults, then file config, then env vars.
// Precedence: defaults -> config file -> environment variables.
func LoadWithFile(filePath string) (*Config, error) {
	cfg := Default()

	fc, err := LoadFromFile(filePath)
	if err != nil {
		return nil, err
	}
	fc.applyTo(cfg)

	cfg.LoadFromEnv()
	return cfg, nil
}
