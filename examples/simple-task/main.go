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
	_ "github.com/lib/pq"

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
	db, err := sql.Open("postgres", *uri)
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
		lockID  = "long-task-lock"
		lockTTL = time.Minute
	)

	lock, err := locker.AcquireLock(ctx, lockID, lockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	log.Println("Start simple task.")
	time.Sleep(time.Second)

	lock, err = locker.RefreshLock(ctx, lock, time.Minute)
	if err != nil {
		return fmt.Errorf("failed to refresh lock: %w", err)
	}

	time.Sleep(time.Second)
	log.Println("Finish simple task.")

	if err = locker.ReleaseLock(ctx, lock); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	return nil
}
