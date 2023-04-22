package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	// Registration of SQL drivers.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ismurov/pglocker"
)

var uri = flag.String("url", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable", "connection url")

func main() {
	flag.Parse()

	if err := realMain(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func realMain(ctx context.Context) error {
	db, err := sql.Open("pgx", *uri)
	if err != nil {
		return fmt.Errorf("cannot connect to test database server: %w", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS locks (
			lock_id    TEXT        NOT NULL PRIMARY KEY,
			generation BIGINT      NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("cannot apply locker migrations: %w", err)
	}

	locker, err := pglocker.New(db, &pglocker.Config{
		Scheme: "public",
		Table:  "locks",
	})
	if err != nil {
		return fmt.Errorf("creating pglocker instance: %w", err)
	}

	const (
		lockID  = "custom-migrations-lock"
		lockTTL = time.Minute
	)

	switch _, err := locker.AcquireLock(ctx, lockID, lockTTL); {
	case err == nil:
		log.Printf("Lock %q is locked for %v.", lockID, lockTTL)

	case errors.Is(err, pglocker.ErrAlreadyLocked):
		log.Printf("Lock %q is already locked.", lockID)

	default:
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	return nil
}
