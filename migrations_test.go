package pglocker_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ismurov/pglocker"
)

func TestMigrateDB_NilConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var db sql.DB // <– This is not valid usage!

	err := pglocker.MigrateDB(ctx, &db, nil)
	if !errors.Is(err, pglocker.ErrConfigIsNil) {
		t.Errorf("got unexpected error: %v", err)
	}
}

func TestMigrateDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := testDatabaseInstance.Open(t,
		testDatabaseInstance.NewDatabase(t).String(),
	)

	cfg := &pglocker.Config{
		Scheme: pglocker.DefaultScheme,
		Table:  pglocker.DefaultTable,
	}

	migrate := func(tb testing.TB, caseName string) {
		tb.Helper()

		if err := pglocker.MigrateDB(ctx, db, cfg); err != nil {
			tb.Fatalf("[%s] pglocker.MigrateDB: %v", caseName, err)
		}
	}

	migrate(t, "first migration")
	migrate(t, "second migration")
	migrate(t, "third migration")

	var exists bool
	if err := db.
		QueryRow(`
			SELECT EXISTS (
				SELECT FROM 
					pg_tables
				WHERE 
					schemaname = $1 AND 
					tablename  = $2
			)
		`, cfg.Scheme, cfg.Table).
		Scan(&exists); err != nil {
		t.Fatalf("failed to check existence of lock statuses table in database: %v", err)
	}

	if !exists {
		t.Errorf(
			"lock statuses table does not exist in database (table: %s.%s)",
			cfg.Scheme, cfg.Table,
		)
	}
}
