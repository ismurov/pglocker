package main

import (
	"context"
	"database/sql"
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

	locker, err := pglocker.NewWithMigrateDB(ctx, db, &pglocker.Config{
		Scheme: pglocker.DefaultScheme,
		Table:  pglocker.DefaultTable,
	})
	if err != nil {
		return fmt.Errorf("creating pglocker instance: %w", err)
	}

	var (
		lockID         = "long-task-lock"
		lockTTL        = time.Minute
		relockInterval = time.Second
		taskDuration   = 10 * time.Second
	)

	return locker.Do(ctx, lockID, lockTTL, relockInterval, func(ctx context.Context) error {
		log.Println("Start long task.")
		defer log.Println("Finish long task.")

		select {
		case <-ctx.Done():
			return ctx.Err()

		// Emulation of long-running task.
		case <-time.After(taskDuration):
			return nil
		}
	})
}
