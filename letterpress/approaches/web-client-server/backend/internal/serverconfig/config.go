// Package serverconfig loads and validates the production runtime contract.
package serverconfig

import (
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

// Config contains environment-controlled server settings.
type Config struct {
	Address         string
	PublicBaseURL   string
	DatabasePath    string
	StaticDirectory string
	InsecureCookies bool
	LogLevel        slog.Level
}

// Load reads configuration using getenv, applying local-development defaults.
func Load(getenv func(string) string) (Config, error) {
	config := Config{
		Address:         valueOrDefault(getenv("LETTERPRESS_ADDR"), ":8080"),
		PublicBaseURL:   strings.TrimSpace(getenv("LETTERPRESS_PUBLIC_BASE_URL")),
		DatabasePath:    valueOrDefault(getenv("LETTERPRESS_DB_PATH"), "letterpress.db"),
		StaticDirectory: strings.TrimSpace(getenv("LETTERPRESS_STATIC_DIR")),
		LogLevel:        slog.LevelInfo,
	}
	if raw := strings.TrimSpace(getenv("LETTERPRESS_INSECURE_COOKIES")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("LETTERPRESS_INSECURE_COOKIES: %w", err)
		}
		config.InsecureCookies = value
	}
	if raw := strings.TrimSpace(getenv("LETTERPRESS_LOG_LEVEL")); raw != "" {
		if err := config.LogLevel.UnmarshalText([]byte(raw)); err != nil {
			return Config{}, fmt.Errorf("LETTERPRESS_LOG_LEVEL: %w", err)
		}
	}
	if err := Validate(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate checks settings after environment and command-line overrides.
func Validate(config Config) error {
	if strings.TrimSpace(config.Address) == "" {
		return fmt.Errorf("listen address is required")
	}
	if strings.TrimSpace(config.DatabasePath) == "" {
		return fmt.Errorf("database path is required")
	}
	if config.PublicBaseURL == "" {
		return nil
	}
	parsed, err := url.Parse(config.PublicBaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("public base URL must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("public base URL must contain only scheme and host")
	}
	if parsed.Scheme != "https" && !config.InsecureCookies {
		return fmt.Errorf("secure cookies require an HTTPS public base URL")
	}
	return nil
}

// AllowedOrigin returns the canonical public origin, or empty for host-based
// local-development validation.
func (config Config) AllowedOrigin() string {
	if config.PublicBaseURL == "" {
		return ""
	}
	parsed, err := url.Parse(config.PublicBaseURL)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
