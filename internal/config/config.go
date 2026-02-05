// Package config provides configuration management for GopherWiki.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration settings for the wiki.
type Config struct {
	// Core settings
	Debug      bool
	Testing    bool
	LogLevel   string
	Repository string
	SecretKey  string

	// Site settings
	SiteName        string
	SiteDescription string
	SiteURL         string
	SiteLogo        string
	SiteIcon        string
	SiteLang        string
	HideLogo        bool
	HomePage        string

	// Auth settings
	AuthMethod             string
	AuthHeadersUsername    string
	AuthHeadersEmail       string
	AuthHeadersPermissions string
	ReadAccess             string
	WriteAccess            string
	AttachmentAccess       string
	AutoApproval           bool
	DisableRegistration    bool
	EmailNeedsConfirmation bool
	NotifyAdminsOnRegister bool
	NotifyUserOnApproval   bool

	// Database
	DatabaseURI string

	// Mail settings
	MailDefaultSender string
	MailServer        string
	MailPort          int
	MailUsername      string
	MailPassword      string
	MailUseTLS        bool
	MailUseSSL        bool

	// Content settings
	RetainPageNameCase            bool
	TreatUnderscoreAsSpaceForTitles bool
	MinifyHTML                    bool
	CommitMessage                 string
	WikilinkStyle                 string

	// Sidebar settings
	SidebarMenutreeMode       string
	SidebarMenutreeIgnoreCase bool
	SidebarMenutreeMaxdepth   string
	SidebarMenutreeFocus      string
	SidebarCustomMenu         string
	SidebarShortcuts          string

	// Git settings
	GitWebServer        bool
	GitRemotePushEnabled bool
	GitRemotePullEnabled bool

	// Misc settings
	RobotsTxt          string
	MaxFormMemorySize  int64
	HTMLExtraHead      string
	HTMLExtraBody      string
	LogLevelWerkzeug   string
}

// Default returns a Config with default values matching the Python implementation.
func Default() *Config {
	return &Config{
		Debug:                  false,
		Testing:                false,
		LogLevel:               "INFO",
		Repository:             "",
		SecretKey:              "CHANGE ME",
		SiteName:               "An Otter Wiki",
		SiteDescription:        "",
		SiteURL:                "http://localhost:8080",
		SiteLogo:               "",
		SiteIcon:               "",
		SiteLang:               "en",
		HideLogo:               false,
		HomePage:               "",
		AuthMethod:             "",
		AuthHeadersUsername:    "x-otterwiki-name",
		AuthHeadersEmail:       "x-otterwiki-email",
		AuthHeadersPermissions: "x-otterwiki-permissions",
		ReadAccess:             "ANONYMOUS",
		WriteAccess:            "ANONYMOUS",
		AttachmentAccess:       "ANONYMOUS",
		AutoApproval:           true,
		DisableRegistration:    false,
		EmailNeedsConfirmation: true,
		NotifyAdminsOnRegister: false,
		NotifyUserOnApproval:   false,
		DatabaseURI:            "sqlite:///:memory:",
		MailDefaultSender:      "otterwiki@YOUR.ORGANIZATION.TLD",
		MailServer:             "",
		MailPort:               0,
		MailUsername:           "",
		MailPassword:           "",
		MailUseTLS:             false,
		MailUseSSL:             false,
		RetainPageNameCase:            false,
		TreatUnderscoreAsSpaceForTitles: false,
		MinifyHTML:                    true,
		CommitMessage:                 "REQUIRED",
		WikilinkStyle:                 "",
		SidebarMenutreeMode:       "SORTED",
		SidebarMenutreeIgnoreCase: false,
		SidebarMenutreeMaxdepth:   "",
		SidebarMenutreeFocus:      "SUBTREE",
		SidebarCustomMenu:         "",
		SidebarShortcuts:          "home pageindex createpage",
		GitWebServer:        false,
		GitRemotePushEnabled: false,
		GitRemotePullEnabled: false,
		RobotsTxt:          "allow",
		MaxFormMemorySize:  1_000_000,
		HTMLExtraHead:      "",
		HTMLExtraBody:      "",
		LogLevelWerkzeug:   "INFO",
	}
}

// LoadFromEnv loads configuration from environment variables.
func (c *Config) LoadFromEnv() {
	// Helper functions
	getEnv := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}

	getEnvBool := func(key string, fallback bool) bool {
		v := os.Getenv(key)
		if v == "" {
			return fallback
		}
		v = strings.ToLower(v)
		return v == "true" || v == "yes" || v == "on" || v == "1"
	}

	getEnvInt := func(key string, fallback int) int {
		v := os.Getenv(key)
		if v == "" {
			return fallback
		}
		i, err := strconv.Atoi(v)
		if err != nil {
			return fallback
		}
		return i
	}

	getEnvInt64 := func(key string, fallback int64) int64 {
		v := os.Getenv(key)
		if v == "" {
			return fallback
		}
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fallback
		}
		return i
	}

	// Core settings
	c.Debug = getEnvBool("DEBUG", c.Debug)
	c.Testing = getEnvBool("TESTING", c.Testing)
	c.LogLevel = getEnv("LOG_LEVEL", c.LogLevel)
	c.Repository = getEnv("REPOSITORY", c.Repository)
	c.SecretKey = getEnv("SECRET_KEY", c.SecretKey)

	// Site settings
	c.SiteName = getEnv("SITE_NAME", c.SiteName)
	c.SiteDescription = getEnv("SITE_DESCRIPTION", c.SiteDescription)
	c.SiteURL = getEnv("SITE_URL", c.SiteURL)
	c.SiteLogo = getEnv("SITE_LOGO", c.SiteLogo)
	c.SiteIcon = getEnv("SITE_ICON", c.SiteIcon)
	c.SiteLang = getEnv("SITE_LANG", c.SiteLang)
	c.HideLogo = getEnvBool("HIDE_LOGO", c.HideLogo)
	c.HomePage = getEnv("HOME_PAGE", c.HomePage)

	// Auth settings
	c.AuthMethod = getEnv("AUTH_METHOD", c.AuthMethod)
	c.AuthHeadersUsername = getEnv("AUTH_HEADERS_USERNAME", c.AuthHeadersUsername)
	c.AuthHeadersEmail = getEnv("AUTH_HEADERS_EMAIL", c.AuthHeadersEmail)
	c.AuthHeadersPermissions = getEnv("AUTH_HEADERS_PERMISSIONS", c.AuthHeadersPermissions)
	c.ReadAccess = getEnv("READ_ACCESS", c.ReadAccess)
	c.WriteAccess = getEnv("WRITE_ACCESS", c.WriteAccess)
	c.AttachmentAccess = getEnv("ATTACHMENT_ACCESS", c.AttachmentAccess)
	c.AutoApproval = getEnvBool("AUTO_APPROVAL", c.AutoApproval)
	c.DisableRegistration = getEnvBool("DISABLE_REGISTRATION", c.DisableRegistration)
	c.EmailNeedsConfirmation = getEnvBool("EMAIL_NEEDS_CONFIRMATION", c.EmailNeedsConfirmation)
	c.NotifyAdminsOnRegister = getEnvBool("NOTIFY_ADMINS_ON_REGISTER", c.NotifyAdminsOnRegister)
	c.NotifyUserOnApproval = getEnvBool("NOTIFY_USER_ON_APPROVAL", c.NotifyUserOnApproval)

	// Database
	c.DatabaseURI = getEnv("SQLALCHEMY_DATABASE_URI", c.DatabaseURI)

	// Mail settings
	c.MailDefaultSender = getEnv("MAIL_DEFAULT_SENDER", c.MailDefaultSender)
	c.MailServer = getEnv("MAIL_SERVER", c.MailServer)
	c.MailPort = getEnvInt("MAIL_PORT", c.MailPort)
	c.MailUsername = getEnv("MAIL_USERNAME", c.MailUsername)
	c.MailPassword = getEnv("MAIL_PASSWORD", c.MailPassword)
	c.MailUseTLS = getEnvBool("MAIL_USE_TLS", c.MailUseTLS)
	c.MailUseSSL = getEnvBool("MAIL_USE_SSL", c.MailUseSSL)

	// Content settings
	c.RetainPageNameCase = getEnvBool("RETAIN_PAGE_NAME_CASE", c.RetainPageNameCase)
	c.TreatUnderscoreAsSpaceForTitles = getEnvBool("TREAT_UNDERSCORE_AS_SPACE_FOR_TITLES", c.TreatUnderscoreAsSpaceForTitles)
	c.MinifyHTML = getEnvBool("MINIFY_HTML", c.MinifyHTML)
	c.CommitMessage = getEnv("COMMIT_MESSAGE", c.CommitMessage)
	c.WikilinkStyle = getEnv("WIKILINK_STYLE", c.WikilinkStyle)

	// Sidebar settings
	c.SidebarMenutreeMode = getEnv("SIDEBAR_MENUTREE_MODE", c.SidebarMenutreeMode)
	c.SidebarMenutreeIgnoreCase = getEnvBool("SIDEBAR_MENUTREE_IGNORE_CASE", c.SidebarMenutreeIgnoreCase)
	c.SidebarMenutreeMaxdepth = getEnv("SIDEBAR_MENUTREE_MAXDEPTH", c.SidebarMenutreeMaxdepth)
	c.SidebarMenutreeFocus = getEnv("SIDEBAR_MENUTREE_FOCUS", c.SidebarMenutreeFocus)
	c.SidebarCustomMenu = getEnv("SIDEBAR_CUSTOM_MENU", c.SidebarCustomMenu)
	c.SidebarShortcuts = getEnv("SIDEBAR_SHORTCUTS", c.SidebarShortcuts)

	// Git settings
	c.GitWebServer = getEnvBool("GIT_WEB_SERVER", c.GitWebServer)
	c.GitRemotePushEnabled = getEnvBool("GIT_REMOTE_PUSH_ENABLED", c.GitRemotePushEnabled)
	c.GitRemotePullEnabled = getEnvBool("GIT_REMOTE_PULL_ENABLED", c.GitRemotePullEnabled)

	// Misc settings
	c.RobotsTxt = getEnv("ROBOTS_TXT", c.RobotsTxt)
	c.MaxFormMemorySize = getEnvInt64("MAX_FORM_MEMORY_SIZE", c.MaxFormMemorySize)
	c.HTMLExtraHead = getEnv("HTML_EXTRA_HEAD", c.HTMLExtraHead)
	c.HTMLExtraBody = getEnv("HTML_EXTRA_BODY", c.HTMLExtraBody)
	c.LogLevelWerkzeug = getEnv("LOG_LEVEL_WERKZEUG", c.LogLevelWerkzeug)
}

// Validate checks that required configuration is set.
func (c *Config) Validate() error {
	if len(c.SecretKey) < 16 || c.SecretKey == "CHANGE ME" {
		return fmt.Errorf("please configure a random SECRET_KEY with a length of at least 16 characters")
	}
	if c.Repository == "" {
		return fmt.Errorf("please configure a REPOSITORY path")
	}
	if _, err := os.Stat(c.Repository); os.IsNotExist(err) {
		return fmt.Errorf("repository path '%s' not found", c.Repository)
	}
	return nil
}

// Load creates a new Config with defaults and loads from environment.
func Load() *Config {
	cfg := Default()
	cfg.LoadFromEnv()
	return cfg
}
