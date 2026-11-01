// Command bootstrap is an explicitly invoked local control-plane operation.
// It is not imported by the HTTP server and never runs on startup.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	applicationauth "transportation-platform/internal/application/auth"
	"transportation-platform/internal/config"
	"transportation-platform/internal/platform/postgres"
)

func main() {
	getenv := func(key string) string {
		switch key {
		case "AUTH_ONLINE_ENABLED":
			return "true"
		case "AUTH_ALLOWED_ORIGINS":
			return "cli://bootstrap"
		default:
			return os.Getenv(key)
		}
	}
	cfg, err := config.Load(getenv)
	if err != nil {
		slog.Error("bootstrap_configuration_invalid", "error", err)
		os.Exit(1)
	}
	secret := os.Getenv("BOOTSTRAP_SECRET")
	identifier := os.Getenv("BOOTSTRAP_IDENTIFIER")
	legalName := os.Getenv("BOOTSTRAP_LEGAL_NAME")
	password := os.Getenv("BOOTSTRAP_PASSWORD")
	if secret == "" || identifier == "" || legalName == "" || password == "" {
		slog.Error("bootstrap_inputs_missing")
		os.Exit(1)
	}
	ctx := context.Background()
	bootstrapPool, err := postgres.OpenBootstrapPool(ctx, cfg.Database)
	if err != nil {
		slog.Error("bootstrap_database_unavailable", "error", err)
		os.Exit(1)
	}
	defer bootstrapPool.Close()
	repository, err := postgres.NewBootstrapAuthRepository(bootstrapPool)
	if err != nil {
		slog.Error("bootstrap_repository_unavailable", "error", err)
		os.Exit(1)
	}
	service, err := applicationauth.NewOnlineAuthService(repository)
	if err != nil {
		slog.Error("bootstrap_service_unavailable", "error", err)
		os.Exit(1)
	}
	accountID, err := service.Bootstrap(ctx, secret, identifier, legalName, password)
	if err != nil {
		if errors.Is(err, applicationauth.ErrBootstrapUnavailable) {
			fmt.Fprintln(os.Stderr, "bootstrap was rejected")
		} else {
			fmt.Fprintln(os.Stderr, "bootstrap failed")
		}
		os.Exit(1)
	}
	fmt.Println(accountID)
}
