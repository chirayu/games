package serverconfig

import (
	"log/slog"
	"testing"
)

func TestLoadProductionConfiguration(t *testing.T) {
	values := map[string]string{
		"LETTERPRESS_ADDR":             ":9000",
		"LETTERPRESS_PUBLIC_BASE_URL":  "https://play.example.com",
		"LETTERPRESS_DB_PATH":          "/data/letterpress.db",
		"LETTERPRESS_STATIC_DIR":       "/app/frontend",
		"LETTERPRESS_INSECURE_COOKIES": "false",
		"LETTERPRESS_LOG_LEVEL":        "warn",
	}
	config, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Address != ":9000" || config.DatabasePath != "/data/letterpress.db" || config.StaticDirectory != "/app/frontend" {
		t.Fatalf("config = %#v", config)
	}
	if config.AllowedOrigin() != "https://play.example.com" || config.InsecureCookies || config.LogLevel != slog.LevelWarn {
		t.Fatalf("security/log config = %#v", config)
	}
}

func TestLoadRejectsInsecurePublicURLWithSecureCookies(t *testing.T) {
	_, err := Load(func(key string) string {
		if key == "LETTERPRESS_PUBLIC_BASE_URL" {
			return "http://play.example.com"
		}
		return ""
	})
	if err == nil {
		t.Fatal("Load accepted HTTP public URL with secure cookies")
	}
}

func TestLoadDefaultsSupportLocalDevelopment(t *testing.T) {
	config, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if config.Address != ":8080" || config.DatabasePath != "letterpress.db" || config.LogLevel != slog.LevelInfo {
		t.Fatalf("defaults = %#v", config)
	}
}
