package pgtesting_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"testing"

	// Registration of SQL drivers.
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"

	"github.com/ismurov/pglocker/pgtesting"
)

// getDatabaseURL returns database connection URL from the `CI_DATABASE_URL`
// environment variable or skip test if tests running with flag `-short`.
func getDatabaseURL(tb testing.TB) string {
	tb.Helper()

	// Querying for -short requires flags to be parsed.
	if !flag.Parsed() {
		flag.Parse()
	}

	if testing.Short() {
		tb.Skip("🚧 Skipping database tests (-short flag provided)!")
	}

	connURL := os.Getenv("CI_DATABASE_URL")
	if connURL == "" {
		tb.Error("environment variable CI_DATABASE_URL is not set")
	}

	return connURL
}

func testMigrateFunc(_ context.Context, u *url.URL, db *sql.DB) error {
	var dbName string
	if err := db.
		QueryRow("SELECT current_database();").
		Scan(&dbName); err != nil {
		return fmt.Errorf("getting database name: %w", err)
	}

	if dbName != u.Path {
		return fmt.Errorf("database name mismatch: got %q (want: %q)", u.Path, dbName)
	}

	return nil
}

func TestTestInstance(t *testing.T) { //nolint:funlen // Test table is used.
	t.Parallel()

	databaseURL := getDatabaseURL(t)

	tests := []struct {
		name string
		cfg  *pgtesting.TestInstanceConfig
	}{
		{
			name: "lib/pq driver",
			cfg: &pgtesting.TestInstanceConfig{
				DriverName:  "postgres",
				DatabaseURL: databaseURL,
			},
		},
		{
			name: "lib/pq driver with migrations",
			cfg: &pgtesting.TestInstanceConfig{
				DriverName:  "postgres",
				DatabaseURL: databaseURL,
				MigrateFunc: testMigrateFunc,
			},
		},
		{
			name: "pgx driver",
			cfg: &pgtesting.TestInstanceConfig{
				DriverName:  "pgx",
				DatabaseURL: databaseURL,
			},
		},
		{
			name: "pgx driver with migrations",
			cfg: &pgtesting.TestInstanceConfig{
				DriverName:  "pgx",
				DatabaseURL: databaseURL,
				MigrateFunc: testMigrateFunc,
			},
		},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			i, err := pgtesting.NewTestInstance(tc.cfg)
			if err != nil {
				t.Fatalf("pgtesting.NewTestInstance: %v", err)
			}
			t.Cleanup(func() {
				if err := i.Close(); err != nil {
					t.Errorf("TestInstance.Close: %v", err)
				}
			})

			uri := i.NewDatabase(t)
			conn := i.Open(t, uri.String())

			var dbName string
			if err := conn.
				QueryRow("SELECT current_database();").
				Scan(&dbName); err != nil {
				t.Fatalf("getting database name: %v", err)
			}

			if dbName != uri.Path {
				t.Errorf("database name mismatch:\ngot:  %s\nwant: %s", uri.Path, dbName)
			}
		})
	}
}
