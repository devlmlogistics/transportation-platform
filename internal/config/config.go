package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config contains runtime configuration. Secret values are intentionally not
// logged or required during Phase 1A.
type Config struct {
	AppEnv            string
	Port              string
	WebDir            string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	Database          DatabaseConfig
}

type DatabaseConfig struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	SSLMode  string
}

func (c Config) ListenAddr() string {
	return ":" + c.Port
}

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, fmt.Errorf("configuration source is required")
	}

	appEnv := strings.ToLower(valueOrDefault(getenv("APP_ENV"), "development"))
	switch appEnv {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("APP_ENV must be development, test, or production")
	}

	port, err := positivePort(valueOrDefault(getenv("PORT"), "8080"))
	if err != nil {
		return Config{}, err
	}

	config := Config{
		AppEnv:   appEnv,
		Port:     strconv.Itoa(port),
		WebDir:   valueOrDefault(getenv("WEB_DIR"), "web/dist"),
		Database: databaseConfig(getenv),
	}
	for name, target := range map[string]*time.Duration{
		"READ_HEADER_TIMEOUT": &config.ReadHeaderTimeout,
		"READ_TIMEOUT":        &config.ReadTimeout,
		"WRITE_TIMEOUT":       &config.WriteTimeout,
		"IDLE_TIMEOUT":        &config.IdleTimeout,
		"SHUTDOWN_TIMEOUT":    &config.ShutdownTimeout,
	} {
		value := valueOrDefault(getenv(name), defaultDuration(name))
		duration, parseErr := time.ParseDuration(value)
		if parseErr != nil || duration <= 0 {
			return Config{}, fmt.Errorf("%s must be a positive duration", name)
		}
		*target = duration
	}
	if strings.TrimSpace(config.WebDir) == "" {
		return Config{}, fmt.Errorf("WEB_DIR must not be empty")
	}
	return config, nil
}

func databaseConfig(getenv func(string) string) DatabaseConfig {
	return DatabaseConfig{
		Host:     strings.TrimSpace(getenv("DATABASE_HOST")),
		Port:     strings.TrimSpace(getenv("DATABASE_PORT")),
		Name:     strings.TrimSpace(getenv("DATABASE_NAME")),
		User:     strings.TrimSpace(getenv("DATABASE_USER")),
		Password: getenv("DATABASE_PASSWORD"),
		SSLMode:  valueOrDefault(getenv("DATABASE_SSLMODE"), "disable"),
	}
}

func positivePort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	return port, nil
}

func defaultDuration(name string) string {
	switch name {
	case "READ_HEADER_TIMEOUT":
		return "5s"
	case "READ_TIMEOUT":
		return "15s"
	case "WRITE_TIMEOUT":
		return "15s"
	case "IDLE_TIMEOUT":
		return "60s"
	case "SHUTDOWN_TIMEOUT":
		return "10s"
	default:
		return ""
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
