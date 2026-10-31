package migrations

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Open opens a SQL database handle only for migration tooling/tests. The
// application runtime uses pgxpool directly and never calls this package at
// startup.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func Up(ctx context.Context, db *sql.DB, directory string) error {
	if err := configure(); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, directory)
}

func DownTo(ctx context.Context, db *sql.DB, directory string, version int64) error {
	if err := configure(); err != nil {
		return err
	}
	return goose.DownToContext(ctx, db, directory, version)
}

func Version(ctx context.Context, db *sql.DB) (int64, error) {
	if err := configure(); err != nil {
		return 0, err
	}
	return goose.GetDBVersionContext(ctx, db)
}

func configure() error {
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	// Keep migration bookkeeping inside the isolated platform schema. The
	// bootstrap creates that schema before the migrator connects.
	goose.SetTableName("platform.goose_db_version")
	return nil
}
