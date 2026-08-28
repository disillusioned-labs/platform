// Package postgres applies goose migrations against a pgx connection pool.
package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrate applies all pending goose migrations against pool using the provided
// embedded migrations filesystem. The caller owns the migration files
// (typically via //go:embed in db/migrations/migrations.go) and passes them in
// so this package stays decoupled from any service's migration set.
//
// goose speaks database/sql, so we open an *sql.DB over the existing pgx pool
// rather than a second connection, and close that handle (not the pool) after.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsFS fs.FS, log *slog.Logger) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	log.Info("database migrations applied")
	return nil
}
