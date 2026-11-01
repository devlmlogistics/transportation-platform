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
	AppEnv              string
	Port                string
	WebDir              string
	ReadHeaderTimeout   time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	ShutdownTimeout     time.Duration
	AuthOnlineEnabled   bool
	AdminHTTPEnabled    bool
	AllowedOrigins      []string
	AuthCookieSecure    bool
	WebAuthnEnabled     bool
	WebAuthnRPID        string
	WebAuthnDisplayName string
	WebAuthnOrigins     []string
	Database            DatabaseConfig
}

type DatabaseConfig struct {
	Host              string
	Port              string
	Name              string
	User              string
	Password          string
	SSLMode           string
	RuntimeUser       string
	RuntimePassword   string
	AuthUser          string
	AuthPassword      string
	BootstrapUser     string
	BootstrapPassword string
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
		AppEnv:              appEnv,
		Port:                strconv.Itoa(port),
		WebDir:              valueOrDefault(getenv("WEB_DIR"), "web/dist"),
		AuthOnlineEnabled:   parseBool(getenv("AUTH_ONLINE_ENABLED")),
		AdminHTTPEnabled:    parseBool(getenv("ADMIN_HTTP_ENABLED")),
		AllowedOrigins:      splitOrigins(getenv("AUTH_ALLOWED_ORIGINS")),
		AuthCookieSecure:    parseBool(getenv("AUTH_COOKIE_SECURE")) || appEnv == "production",
		WebAuthnEnabled:     parseBool(getenv("WEBAUTHN_ENABLED")),
		WebAuthnRPID:        strings.TrimSpace(getenv("WEBAUTHN_RP_ID")),
		WebAuthnDisplayName: valueOrDefault(getenv("WEBAUTHN_RP_DISPLAY_NAME"), "Transportation Platform"),
		WebAuthnOrigins:     splitOrigins(getenv("WEBAUTHN_ORIGINS")),
		Database:            databaseConfig(getenv),
	}
	for _, origin := range config.AllowedOrigins {
		if !isLocalOrigin(origin) {
			config.AuthCookieSecure = true
		}
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
	if config.AdminHTTPEnabled && !config.AuthOnlineEnabled {
		return Config{}, fmt.Errorf("ADMIN_HTTP_ENABLED requires AUTH_ONLINE_ENABLED")
	}
	if config.AuthOnlineEnabled {
		if config.Database.Host == "" || config.Database.Port == "" || config.Database.Name == "" {
			return Config{}, fmt.Errorf("database host, port and name are required when online authentication is enabled")
		}
		if config.Database.RuntimeUser != "tp_runtime" || config.Database.AuthUser != "tp_auth_runtime" {
			return Config{}, fmt.Errorf("online authentication requires DATABASE_RUNTIME_USER=tp_runtime and DATABASE_AUTH_USER=tp_auth_runtime")
		}
		if config.Database.RuntimePassword == "" || config.Database.AuthPassword == "" {
			return Config{}, fmt.Errorf("runtime and authentication database passwords are required when online authentication is enabled")
		}
		if len(config.AllowedOrigins) == 0 {
			return Config{}, fmt.Errorf("AUTH_ALLOWED_ORIGINS must contain at least one origin when online authentication is enabled")
		}
	}
	if config.WebAuthnEnabled {
		if !config.AuthOnlineEnabled {
			return Config{}, fmt.Errorf("WEBAUTHN_ENABLED requires AUTH_ONLINE_ENABLED")
		}
		if config.WebAuthnRPID == "" || len(config.WebAuthnOrigins) == 0 {
			return Config{}, fmt.Errorf("WebAuthn RP id and origins are required when WebAuthn is enabled")
		}
	}
	return config, nil
}

func databaseConfig(getenv func(string) string) DatabaseConfig {
	baseUser := strings.TrimSpace(getenv("DATABASE_USER"))
	runtimeUser := strings.TrimSpace(getenv("DATABASE_RUNTIME_USER"))
	if runtimeUser == "" && baseUser == "tp_runtime" {
		runtimeUser = baseUser
	}
	runtimePassword := getenv("DATABASE_RUNTIME_PASSWORD")
	if runtimePassword == "" && runtimeUser == baseUser && baseUser == "tp_runtime" {
		runtimePassword = getenv("DATABASE_PASSWORD")
	}
	return DatabaseConfig{
		Host:              strings.TrimSpace(getenv("DATABASE_HOST")),
		Port:              strings.TrimSpace(getenv("DATABASE_PORT")),
		Name:              strings.TrimSpace(getenv("DATABASE_NAME")),
		User:              baseUser,
		Password:          getenv("DATABASE_PASSWORD"),
		SSLMode:           valueOrDefault(getenv("DATABASE_SSLMODE"), "disable"),
		RuntimeUser:       runtimeUser,
		RuntimePassword:   runtimePassword,
		AuthUser:          valueOrDefault(getenv("DATABASE_AUTH_USER"), "tp_auth_runtime"),
		AuthPassword:      getenv("DATABASE_AUTH_PASSWORD"),
		BootstrapUser:     valueOrDefault(getenv("DATABASE_BOOTSTRAP_USER"), "tp_bootstrap_runtime"),
		BootstrapPassword: getenv("DATABASE_BOOTSTRAP_PASSWORD"),
	}
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitOrigins(raw string) []string {
	var origins []string
	for _, item := range strings.Split(raw, ",") {
		if origin := strings.TrimSpace(item); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func isLocalOrigin(origin string) bool {
	origin = strings.ToLower(strings.TrimSpace(origin))
	return strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "http://127.0.0.1") ||
		strings.HasPrefix(origin, "http://[::1]")
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
