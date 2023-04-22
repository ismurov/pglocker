// Package pgtesting provides tools for integration testing with PostgeSQL database.
//
// One of most useful feature of package is test database instance that provides
// functionality for creating independent databases suitable for use in testing.
// This approach can be useful to prevent side effects when the database remains
// in a dirty state after the tests are completed. Also independent databases
// allow running test in parallel.
//
// Usage examples:
//
//	package pglocker_test
//
//	import (
//		"context"
//		"database/sql"
//		"net/url"
//		"os"
//		"testing"
//
//		// Registration of SQL driver.
//		_ "github.com/jackc/pgx/v5/stdlib"
//
//		"github.com/ismurov/pglocker"
//	)
//
//	var testDatabaseInstance *pgtesting.TestInstance
//
//	func TestMain(m *testing.M) {
//		var code int
//		defer func() { os.Exit(code) }()
//
//		// Initializing a database instance for all package tests.
//		testDatabaseInstance = pgtesting.MustTestInstance(&pgtesting.TestInstanceConfig{
//			DriverName:  "pgx",
//			DatabaseURL: os.Getenv("CI_DATABASE_URL"),
//			MigrateFunc: func(context.Context, *url.URL, *sql.DB) error {
//				// Custome database migration logic here.
//				return nil
//			},
//			SkipTests: os.Getenv("CI_DATABASE_URL") == "",
//		})
//		defer testDatabaseInstance.MustClose()
//
//		// Run package tests.
//		code = m.Run()
//	}
//
//	func TestWithDatabase(t *testing.T) {
//		// Creating a new database suitable for current test.
//		uri := testDatabaseInstance.NewDatabase(t)
//
//		// Opening a connection to the created database.
//		conn := testDatabaseInstance.Open(t, uri.String())
//
//		if _, err = conn.Exec("SELECT 1"); err != nil {
//			t.Fatalf("executing query: %v", err)
//		}
//	}
package pgtesting

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"testing"
)

// TestInstanceConfig is a configuration for create and initialize
// test database instance.
type TestInstanceConfig struct {
	// DriverName is a name of database driver registered by `sql.Register`.
	//
	// (required).
	DriverName string

	// DatabaseURL is a connection URI to testing database. The connection
	// to the target database is only used to create a new test database
	// that will be deleted with closing TestInstance.
	//
	// (required).
	DatabaseURL string

	// MigrateFunc is a function that apply custom migrations to testing
	// database. The migration is applied once to the database, which will
	// be used as a template for cloning in future tests.
	MigrateFunc MigrateFunc

	// SkipTests is a flag for skipping database tests.
	SkipTests bool
}

// TestInstance is a wrapper around the database instance.
type TestInstance struct {
	dbFactory *TestDatabaseFactory

	skipReason string
}

// MustTestInstance is NewTestInstance, except it prints errors to stderr and
// calls os.Exit when finished. Callers can call Close or MustClose().
func MustTestInstance(cfg *TestInstanceConfig) *TestInstance {
	testDatabaseInstance, err := NewTestInstance(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[pgtesting] NewTestInstance:", err.Error())
		os.Exit(1)
	}
	return testDatabaseInstance
}

// NewTestInstance creates a new URI-based database instance. It also
// creates an initial database, runs the migrations (by cfg.MigrateFunc),
// and sets that database as a template to be cloned by future tests.
//
// This should not be used outside of testing, but it is exposed in
// the package so it can be shared with other packages. It should be
// called and instantiated in TestMain.
//
// All database tests can be skipped by running `go test -short` or
// explicitly by configuration.
func NewTestInstance(cfg *TestInstanceConfig) (*TestInstance, error) {
	if cfg == nil {
		return nil, errors.New("config is nil") //nolint:goerr113 // Static error is an overhead.
	}

	// Querying for -short requires flags to be parsed.
	if !flag.Parsed() {
		flag.Parse()
	}

	// Do not create an instance in -short mode.
	if testing.Short() {
		return &TestInstance{
			skipReason: "🚧 Skipping database tests (-short flag provided)!",
		}, nil
	}

	// Do not create an instance if database tests are explicitly skipped.
	if cfg.SkipTests {
		return &TestInstance{
			skipReason: "🚧 Skipping database tests (SkipTests provided by config)!",
		}, nil
	}

	if cfg.DriverName == "" {
		return nil, errors.New("config field DriverName is required") //nolint:goerr113 // Static error is an overhead.
	}
	if cfg.DatabaseURL == "" {
		return nil, errors.New("config field DatabaseURL is required") //nolint:goerr113 // Static error is an overhead.
	}

	ctx := context.Background()

	// Initialize new database factory.
	dbFactory, err := NewTestDatabaseFactory(ctx, cfg.DriverName, cfg.DatabaseURL, cfg.MigrateFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to create database factory: %w", err)
	}

	return &TestInstance{
		dbFactory: dbFactory,
	}, nil
}

// MustClose is like Close except it prints the error to stderr and calls os.Exit.
func (i *TestInstance) MustClose() {
	if err := i.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "[pgtesting] TestInstance.Close:", err.Error())
		os.Exit(1)
	}
}

// Close terminates the test database instance, cleaning up any resources.
func (i *TestInstance) Close() error {
	// Do not attempt to close things when there's nothing to close.
	if i.skipReason != "" {
		return nil
	}

	if err := i.dbFactory.Close(); err != nil {
		return fmt.Errorf("failed to close database factory: %w", err)
	}

	return nil
}

// TemplateName returns name of database template.
func (i *TestInstance) TemplateName() string {
	return i.dbFactory.TemplateName()
}

// NewDatabase creates a new database suitable for use in testing.
// It returns the connection URL to created database.
func (i *TestInstance) NewDatabase(tb testing.TB) *url.URL {
	tb.Helper()

	// Ensure we should actually create the database.
	if i.skipReason != "" {
		tb.Skip(i.skipReason)
	}

	return i.dbFactory.NewDatabase(tb)
}

// NewDatabaseWithCleanup creates a new database suitable for use in testing.
// It returns the connection URL to created database.
//
// If the cleanup is true, the database will be deleted after testing.
func (i *TestInstance) NewDatabaseWithCleanup(tb testing.TB, doCleanup bool) *url.URL {
	tb.Helper()

	// Ensure we should actually create the database.
	if i.skipReason != "" {
		tb.Skip(i.skipReason)
	}

	return i.dbFactory.NewDatabaseWithCleanup(tb, doCleanup)
}

// Open opens a database connection using connection URL and database driver
// specified during initialization. Also its closing opened database connection
// with testing cleanup.
func (i *TestInstance) Open(tb testing.TB, connURL string) *sql.DB {
	tb.Helper()

	db, err := sql.Open(i.dbFactory.DriverName(), connURL)
	if err != nil {
		tb.Fatalf("failed to open database connection: %v", err)
	}
	tb.Cleanup(func() {
		if err := db.Close(); err != nil {
			tb.Errorf("failed to close database connection: %v", err)
		}
	})

	return db
}
