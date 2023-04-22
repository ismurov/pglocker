package pglocker

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateDB applies the necessary migrations to the database.
func MigrateDB(ctx context.Context, db *sql.DB, cfg *Config) error {
	if cfg == nil {
		return ErrConfigIsNil
	}

	cfg.SetDefault()

	return migrateDB(ctx, db, cfg)
}

// migrateDB applies the necessary migrations to the database.
func migrateDB(ctx context.Context, db *sql.DB, cfg *Config) error {
	return inTx(ctx, db, sql.LevelReadCommitted, func(tx *sql.Tx) error {
		queries := []struct {
			name  string
			query string
			skip  bool
		}{
			{
				name: "creating database schema",
				query: fmt.Sprintf(`
					CREATE SCHEMA IF NOT EXISTS %q;
				`, cfg.Scheme),
				skip: cfg.Scheme == PublicScheme,
			},
			{
				name: "creating database table",
				query: fmt.Sprintf(`
					CREATE TABLE IF NOT EXISTS %q.%q (
						lock_id    TEXT        NOT NULL PRIMARY KEY,
						generation BIGINT      NOT NULL,
						expires_at TIMESTAMPTZ NOT NULL
					);
				`, cfg.Scheme, cfg.Table),
			},
		}

		for _, q := range queries {
			if q.skip {
				continue
			}

			if _, err := tx.ExecContext(ctx, q.query); err != nil {
				return fmt.Errorf("%s: %w", q.name, err)
			}
		}

		return nil
	})
}
