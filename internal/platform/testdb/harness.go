// Package testdb owns the PostgreSQL container used by destructive integration
// tests. It deliberately does not accept an arbitrary DSN as proof that the
// target is safe to reset.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	postgresImage = "postgres:16-alpine"
	databaseName  = "transportation_platform_test"
	markerTable   = "platform.test_harness_marker"
)

type Harness struct {
	ContainerName string
	ContainerID   string
	Marker        string
	RuntimeDSN    string
	AuthDSN       string
	BootstrapDSN  string
	MigratorDSN   string
	AdminDSN      string
}

func Start(ctx context.Context, repositoryRoot string) (*Harness, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return nil, errors.New("repository root is required")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker is required for PostgreSQL integration tests: %w", err)
	}
	if _, err := run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return nil, fmt.Errorf("docker daemon is unavailable: %w", err)
	}

	marker := "tp-harness-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	containerName := "transportation-platform-test-" + strings.TrimPrefix(marker, "tp-harness-")
	harness := &Harness{ContainerName: containerName, Marker: marker}

	containerID, err := run(ctx, "docker", "run", "--rm", "-d",
		"--name", containerName,
		"--label", "transportation-platform.harness="+marker,
		"--publish", "127.0.0.1::5432",
		"--env", "POSTGRES_DB="+databaseName,
		"--env", "POSTGRES_HOST_AUTH_METHOD=trust",
		postgresImage)
	if err != nil {
		return nil, fmt.Errorf("start dedicated PostgreSQL container: %w", err)
	}
	harness.ContainerID = strings.TrimSpace(containerID)
	if harness.ContainerID == "" {
		return nil, errors.New("docker returned an empty container id")
	}

	cleanupOnFailure := true
	defer func() {
		if cleanupOnFailure {
			// Before the marker exists, Close intentionally refuses removal. The
			// generated container name, exact ID, and private label are still
			// sufficient to prove ownership of a partially initialized harness.
			if err := harness.verifyContainerIdentity(context.Background()); err == nil {
				_, _ = run(context.Background(), "docker", "rm", "--force", "--volumes", harness.ContainerName)
			}
		}
	}()

	if err := waitReady(ctx, containerName); err != nil {
		return nil, err
	}
	port, err := containerPort(ctx, containerName)
	if err != nil {
		return nil, err
	}
	baseDSN := "postgres://%s@127.0.0.1:%s/%s?sslmode=disable"
	harness.RuntimeDSN = fmt.Sprintf(baseDSN, "tp_runtime", port, databaseName)
	harness.AuthDSN = fmt.Sprintf(baseDSN, "tp_auth_runtime", port, databaseName)
	harness.BootstrapDSN = fmt.Sprintf(baseDSN, "tp_bootstrap_runtime", port, databaseName)
	harness.MigratorDSN = fmt.Sprintf(baseDSN, "tp_migrator", port, databaseName)
	harness.AdminDSN = fmt.Sprintf(baseDSN, "postgres", port, databaseName)

	bootstrapPath := filepath.Join(repositoryRoot, "db", "bootstrap", "platform_roles.sql")
	bootstrap, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return nil, fmt.Errorf("read role bootstrap: %w", err)
	}
	if _, err := runWithInput(ctx, bootstrap, "docker", "exec", "-i", containerName,
		"psql", "-U", "postgres", "-d", databaseName,
		"-v", "ON_ERROR_STOP=1",
		"-v", "owner_role=tp_owner",
		"-v", "migrator_role=tp_migrator",
		"-v", "runtime_role=tp_runtime",
		"-v", "auth_runtime_role=tp_auth_runtime",
		"-v", "bootstrap_runtime_role=tp_bootstrap_runtime",
		"-v", "security_writer_role=tp_security_audit_writer",
		"-v", "schema_name=platform", "-f", "-"); err != nil {
		return nil, fmt.Errorf("bootstrap isolated roles: %w", err)
	}
	markerSQL := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    marker text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE %s OWNER TO tp_owner;
INSERT INTO %s (marker) VALUES ('%s');
`, markerTable, markerTable, markerTable, marker)
	if _, err := run(ctx, "docker", "exec", containerName,
		"psql", "-U", "postgres", "-d", databaseName,
		"-v", "ON_ERROR_STOP=1", "-c", markerSQL); err != nil {
		return nil, fmt.Errorf("write harness marker: %w", err)
	}
	if err := harness.Verify(ctx); err != nil {
		return nil, err
	}
	cleanupOnFailure = false
	return harness, nil
}

func (h *Harness) Verify(ctx context.Context) error {
	if h == nil || h.ContainerName == "" || h.ContainerID == "" || h.Marker == "" {
		return errors.New("incomplete test harness identity")
	}
	if err := h.verifyContainerIdentity(ctx); err != nil {
		return err
	}
	marker, err := run(ctx, "docker", "exec", h.ContainerName,
		"psql", "-U", "postgres", "-d", databaseName,
		"-At", "-v", "ON_ERROR_STOP=1", "-c",
		"SELECT marker FROM "+markerTable+" WHERE marker = '"+h.Marker+"'")
	if err != nil || strings.TrimSpace(marker) != h.Marker {
		return fmt.Errorf("test harness database marker mismatch: got %q want %q: %w", strings.TrimSpace(marker), h.Marker, err)
	}
	return nil
}

func (h *Harness) verifyContainerIdentity(ctx context.Context) error {
	if h == nil || h.ContainerName == "" || h.ContainerID == "" || h.Marker == "" {
		return errors.New("incomplete test harness identity")
	}
	id, err := run(ctx, "docker", "inspect", "--format", "{{.Id}}", h.ContainerName)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(id), h.ContainerID) {
		return fmt.Errorf("test harness container identity mismatch: got %q want %q: %w", strings.TrimSpace(id), h.ContainerID, err)
	}
	label, err := run(ctx, "docker", "inspect", "--format", "{{index .Config.Labels \"transportation-platform.harness\"}}", h.ContainerName)
	if err != nil || strings.TrimSpace(label) != h.Marker {
		return fmt.Errorf("test harness label mismatch: got %q want %q: %w", strings.TrimSpace(label), h.Marker, err)
	}
	return nil
}

func (h *Harness) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if err := h.Verify(ctx); err != nil {
		return fmt.Errorf("refusing to remove unverified test harness: %w", err)
	}
	_, err := run(ctx, "docker", "rm", "--force", "--volumes", h.ContainerName)
	return err
}

func waitReady(ctx context.Context, containerName string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		// pg_isready can report success while the entrypoint is still
		// initializing a temporary cluster. A real query against the target
		// database proves that initialization is complete.
		if _, err := run(ctx, "docker", "exec", containerName,
			"psql", "-U", "postgres", "-d", databaseName,
			"-At", "-v", "ON_ERROR_STOP=1", "-c", "SELECT 1"); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.New("timed out waiting for PostgreSQL 16 harness")
}

func containerPort(ctx context.Context, containerName string) (string, error) {
	port, err := run(ctx, "docker", "inspect", "--format", "{{(index (index .NetworkSettings.Ports \"5432/tcp\") 0).HostPort}}", containerName)
	port = strings.TrimSpace(port)
	if err != nil || port == "" {
		return "", fmt.Errorf("resolve harness PostgreSQL port %q: %w", port, err)
	}
	return port, nil
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	return runWithInput(ctx, nil, name, args...)
}

func runWithInput(ctx context.Context, input []byte, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	if input != nil {
		command.Stdin = strings.NewReader(string(input))
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
