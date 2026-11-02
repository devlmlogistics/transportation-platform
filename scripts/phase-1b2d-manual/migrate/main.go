// Command migrate is a local-only helper for the isolated 1B.2-D manual test
// database. It never runs from the platform server process.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"transportation-platform/internal/platform/migrations"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "version") {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/phase-1b2d-manual/migrate (up|version)")
		os.Exit(2)
	}
	dsn := os.Getenv("PHASE_1B2D_MIGRATOR_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "PHASE_1B2D_MIGRATOR_DSN is required")
		os.Exit(2)
	}
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	db, err := migrations.Open(context.Background(), dsn)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	directory := filepath.Join(root, "db", "migrations")
	switch os.Args[1] {
	case "up":
		if err := migrations.Up(context.Background(), db, directory); err != nil {
			fail(err)
		}
	case "version":
		version, err := migrations.Version(context.Background(), db)
		if err != nil {
			fail(err)
		}
		fmt.Println(version)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
