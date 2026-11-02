package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/config"
)

type OnlinePools struct {
	Runtime    *pgxpool.Pool
	Auth       *pgxpool.Pool
	DriverAuth *pgxpool.Pool
}

func (p *OnlinePools) Close() {
	if p == nil {
		return
	}
	if p.Auth != nil {
		p.Auth.Close()
	}
	if p.DriverAuth != nil {
		p.DriverAuth.Close()
	}
	if p.Runtime != nil {
		p.Runtime.Close()
	}
}

func OpenOnlinePools(ctx context.Context, database config.DatabaseConfig) (*OnlinePools, error) {
	if database.Host == "" || database.Port == "" || database.Name == "" {
		return nil, errors.New("database host, port and name are required")
	}
	if database.RuntimeUser != "tp_runtime" || database.AuthUser != "tp_auth_runtime" || database.DriverAuthUser != "tp_driver_auth_runtime" {
		return nil, errors.New("online pools require tp_runtime, tp_auth_runtime and tp_driver_auth_runtime")
	}
	if database.RuntimePassword == "" || database.AuthPassword == "" || database.DriverAuthPassword == "" {
		return nil, errors.New("online pool credentials are required")
	}
	runtime, err := openPool(ctx, database, database.RuntimeUser, database.RuntimePassword)
	if err != nil {
		return nil, fmt.Errorf("open runtime pool: %w", err)
	}
	authPool, err := openPool(ctx, database, database.AuthUser, database.AuthPassword)
	if err != nil {
		runtime.Close()
		return nil, fmt.Errorf("open authentication pool: %w", err)
	}
	driverAuthPool, err := openPool(ctx, database, database.DriverAuthUser, database.DriverAuthPassword)
	if err != nil {
		runtime.Close()
		authPool.Close()
		return nil, fmt.Errorf("open driver authentication pool: %w", err)
	}
	return &OnlinePools{Runtime: runtime, Auth: authPool, DriverAuth: driverAuthPool}, nil
}

// OpenBootstrapPool is intentionally separate from OpenOnlinePools. The
// explicit bootstrap command is the only production-facing caller and uses a
// dedicated database role that is not available to the HTTP server pools.
func OpenBootstrapPool(ctx context.Context, database config.DatabaseConfig) (*pgxpool.Pool, error) {
	if database.Host == "" || database.Port == "" || database.Name == "" {
		return nil, errors.New("database host, port and name are required")
	}
	if database.BootstrapUser != "tp_bootstrap_runtime" {
		return nil, errors.New("bootstrap pool requires tp_bootstrap_runtime")
	}
	if database.BootstrapPassword == "" {
		return nil, errors.New("bootstrap database password is required")
	}
	return openPool(ctx, database, database.BootstrapUser, database.BootstrapPassword)
}

func openPool(ctx context.Context, database config.DatabaseConfig, user, password string) (*pgxpool.Pool, error) {
	dsn := url.URL{
		Scheme:   "postgres",
		Host:     database.Host + ":" + database.Port,
		Path:     "/" + database.Name,
		User:     url.UserPassword(user, password),
		RawQuery: url.Values{"sslmode": []string{database.SSLMode}}.Encode(),
	}
	poolConfig, err := pgxpool.ParseConfig(dsn.String())
	if err != nil {
		return nil, err
	}
	poolConfig.MinConns = 0
	poolConfig.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
