package config

import "testing"

func TestLoadDefaultsWithoutDatabase(t *testing.T) {
	config, err := Load(func(key string) string {
		return map[string]string{`APP_ENV`: `test`}[key]
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Port != "8080" || config.WebDir != "web/dist" {
		t.Fatalf("defaults = %#v", config)
	}
	if config.Database.Host != "" {
		t.Fatalf("database should be optional in Phase 1A")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	_, err := Load(func(key string) string {
		if key == "PORT" {
			return "not-a-port"
		}
		return ""
	})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid port error")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	_, err := Load(func(key string) string {
		if key == "READ_TIMEOUT" {
			return "0s"
		}
		return ""
	})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid duration error")
	}
}
