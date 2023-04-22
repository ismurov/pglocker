PG Locker
=============

PG Locker is a Go library for providing a distributed locking mechanism based on PostgreSQL.

## Runnable examples
- [Simple task](./examples/simple-task/main.go)
- [Long task](./examples/long-task/main.go)
- [Custom migrations](./examples/custom-migrations/main.go)

## Usage
Here is an example of using locker for a task with an unknown duration.
```golang
package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	// Registration of SQL drivers.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ismurov/pglocker"
)

func main() {
	ctx := context.Background()

	// Establishing connection to PostgreSQL database server.
	db, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	// Creating locker with applies migrations to the database.
	locker, err := pglocker.NewWithMigrateDB(ctx, db, &pglocker.Config{
		Scheme: pglocker.DefaultScheme, // Database schema used to store lock statuses.
		Table:  pglocker.DefaultTable,  // Database table used to store lock statuses.
	})
	if err != nil {
		log.Fatalf("creating pglocker: %v", err)
	}

	var (
		lockID  = "long-task-lock"
		lockTTL = time.Minute
	)

	// Run task with re-locking process.
	if doErr := locker.Do(ctx, lockID, lockTTL, lockTTL/2, func(ctx context.Context) error {
		//
		// Task performed with a lock.
		//
		return nil
	}); doErr != nil {
		log.Fatalf("task failed: %v", err)
	}
}
```

### Custom Database Migrations
For using package with custom database migrations tools exists constructor
function `pglocker.New` without auto applying migrations from package.
In this case, the database should contain a table with the following schema
(for example):
```sql
CREATE TABLE locks (
    lock_id    TEXT        NOT NULL PRIMARY KEY,
    generation BIGINT      NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
```
Also see the example with [custom migrations](./examples/custom-migrations/main.go) 

## Testing

PostgreSQL server is required to run all project tests.

Example of running PostgreSQL in a Docker container.
```sh
docker run \
	--rm \
	-e POSTGRES_PASSWORD=postgres \
	postgres:14
```

Running integration tests.
```sh
CI_DATABASE_URL=postgres://postgres:postgres@postgres:5432/postgres?sslmode=disable \
	make test-acc
```

Also package contains tools database testing in [pgtesting](./pgtesting).
