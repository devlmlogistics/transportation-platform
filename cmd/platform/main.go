package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"transportation-platform/internal/config"
	"transportation-platform/internal/httpapi"
	"transportation-platform/internal/httpserver"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("configuration_invalid", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.AppEnv)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server_stopped", "error", err)
		os.Exit(1)
	}
}

func newLogger(appEnv string) *slog.Logger {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	handlerOptions := &slog.HandlerOptions{Level: level}
	if appEnv == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, handlerOptions))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, handlerOptions))
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	handler, err := httpapi.NewRouter(cfg, logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	logger.Info("http_server_listening", "addr", server.Addr, "app_env", cfg.AppEnv)

	return httpserver.Run(ctx, server, listener, func(server *http.Server, listener net.Listener) error {
		return server.Serve(listener)
	}, cfg.ShutdownTimeout)
}
