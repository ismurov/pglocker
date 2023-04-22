package pgtesting

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testDatabaseFactoryTimeout is timeout for executing database queries.
const testDatabaseFactoryTimeout = 30 * time.Second

// MigrateFunc is a function that apply custom migrations to testing database.
// Function calls with two arguments, with a connection URL and an already open
// connection to the target database.
type MigrateFunc func(context.Context, *url.URL, *sql.DB) error

// TestDatabaseFactory this is a database factory that creates independent
// databases for tests based on a template created during initialization.
type TestDatabaseFactory struct {
	driverName   string
	url          *url.URL
	db           *sql.DB
	baseName     string
	templateName string

	pgMajorVer        int
	isTemplateCreated bool
	cloneCounter      uint32 // <- atomic
}

// NewTestDatabaseFactory creates new instance of TestDatabaseFactory with the
// specified sql driver name. Passing the migrate function is optional.
//
// This should not be used outside of testing, but it is exposed in
// the package so it can be shared with other packages. It should be
// called and instantiated in TestMain.
//
// Under the hood, it creates a new database and calls the migrateFunc to apply
// custom migrations. All future tests will reuse this database as template for
// prevent side effects.
func NewTestDatabaseFactory(ctx context.Context, driverName, connURL string, migrateFunc MigrateFunc) (*TestDatabaseFactory, error) {
	cURL, err := url.Parse(connURL)
	if err != nil {
		return nil, fmt.Errorf("parsing connection url to database: %w", err)
	}

	baseName, err := randomDatabaseName("test_database_")
	if err != nil {
		return nil, fmt.Errorf("generating testing database name: %w", err)
	}
	templateName := baseName + "_template"

	db, err := sql.Open(driverName, cURL.String())
	if err != nil {
		return nil, fmt.Errorf("opening database connection: %w", err)
	}

	f := &TestDatabaseFactory{
		driverName:   driverName,
		url:          cURL,
		db:           db,
		baseName:     baseName,
		templateName: templateName,
	}

	setup := func() error {
		if err := f.definePostgresVersion(ctx); err != nil {
			return fmt.Errorf("define postgres version: %w", err)
		}

		if err := f.createTemplateDatabase(ctx, migrateFunc); err != nil {
			return fmt.Errorf("setup test database: %w", err)
		}

		return nil
	}

	if err := setup(); err != nil {
		if cErr := f.Close(); cErr != nil { //nolint:contextcheck // Internal context is used.
			return nil, fmt.Errorf("close error: %w (original error: %v)", cErr, err) //nolint:errorlint // Wraps only one error.
		}

		return nil, err
	}

	return f, nil
}

// Close closes database connection pool and deletes template database
// if it exists.
func (f *TestDatabaseFactory) Close() (closeErr error) {
	defer func() {
		if err := f.db.Close(); err != nil {
			closeErr = fmt.Errorf("closing database connection: %w", err)
			return
		}
	}()

	if !f.isTemplateCreated {
		return nil
	}

	return f.withTimeout(f.dropTemplateDatabase)
}

// DriverName returns a name of database driver used during initialization.
func (f *TestDatabaseFactory) DriverName() string { return f.driverName }

// TemplateName returns a name of database template.
func (f *TestDatabaseFactory) TemplateName() string { return f.templateName }

// definePostgresVersion detects the Postgres version and sets it to the pgMajorVer variable.
func (f *TestDatabaseFactory) definePostgresVersion(ctx context.Context) error {
	var version string
	if err := f.db.QueryRowContext(ctx, `SHOW SERVER_VERSION;`).Scan(&version); err != nil { //nolint:execinquery // Getting the PostgreSQL version.
		return fmt.Errorf("getting server version: %w", err)
	}

	majorVer, err := postgresMajorVersion(version)
	if err != nil {
		return fmt.Errorf("parsing postgres major version: %w", err)
	}

	f.pgMajorVer = majorVer

	return nil
}

// createTemplateDatabase creates a template database and calls the migration
// function to create a custom database schema.
func (f *TestDatabaseFactory) createTemplateDatabase(ctx context.Context, migrateFunc MigrateFunc) (err error) {
	q := fmt.Sprintf(`CREATE DATABASE %q;`, f.templateName)

	if _, err = f.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("creating template database %q: %w", f.templateName, err)
	}
	f.isTemplateCreated = true

	if migrateFunc == nil {
		return nil
	}

	tmplURL := *f.url
	tmplURL.Path = f.templateName

	tmplDB, err := sql.Open(f.driverName, tmplURL.String())
	if err != nil {
		return fmt.Errorf("opening connection to template database: %w", err)
	}
	defer func() {
		if cErr := tmplDB.Close(); cErr != nil {
			if err != nil {
				err = fmt.Errorf("closing connection to template database: %w (original error: %v)", cErr, err) //nolint:errorlint // Wraps only one error.
				return
			}
			err = fmt.Errorf("closing connection to template database: %w", cErr)
		}
	}()

	if err = migrateFunc(ctx, &tmplURL, tmplDB); err != nil {
		return fmt.Errorf("migrate error: %w", err)
	}

	return nil
}

// dropTemplateDatabase deletes creates a database of templates.
func (f *TestDatabaseFactory) dropTemplateDatabase(ctx context.Context) error {
	q := fmt.Sprintf(`DROP DATABASE IF EXISTS %q;`, f.templateName)

	if _, err := f.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("dropping template database %q: %w", f.templateName, err)
	}

	return nil
}

// NewDatabase creates a new database suitable for use in testing.
// It returns the connection URL to new database.
func (f *TestDatabaseFactory) NewDatabase(tb testing.TB) *url.URL {
	tb.Helper()

	return f.NewDatabaseWithCleanup(tb, true)
}

// NewDatabaseWithCleanup creates a new database suitable for use in testing.
// It returns the connection URL to new database.
//
// If the cleanup is true, the database will be deleted after testing.
func (f *TestDatabaseFactory) NewDatabaseWithCleanup(tb testing.TB, doCleanup bool) *url.URL {
	tb.Helper()

	dbName := f.generateDatabaseName()

	if err := f.withTimeout(func(cCtx context.Context) error {
		return f.createDatabase(cCtx, dbName)
	}); err != nil {
		tb.Fatalf("failed to create test database: %v", err)
	}

	if doCleanup {
		tb.Cleanup(func() {
			if err := f.withTimeout(func(dCtx context.Context) error {
				return f.dropDatabase(dCtx, dbName)
			}); err != nil {
				tb.Errorf("failed to delete test database: %v", err)
			}
		})
	}

	copyURL := *f.url
	copyURL.Path = dbName

	return &copyURL
}

// createDatabase creates a new database based on a template.
func (f *TestDatabaseFactory) createDatabase(ctx context.Context, name string) error {
	q := fmt.Sprintf(`CREATE DATABASE %q WITH TEMPLATE %q;`, name, f.templateName)

	if _, err := f.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("creating database %q: %w", name, err)
	}

	return nil
}

// dropDatabase deletes database by name.
func (f *TestDatabaseFactory) dropDatabase(ctx context.Context, name string) error {
	if f.pgMajorVer >= 13 { //nolint:gomnd // PostgreSQL version 13.
		return f.dropDatabaseFrom13(ctx, name)
	}
	return f.dropDatabaseBefore13(ctx, name)
}

// dropDatabaseFrom13 drops datadase query for postgres version 13 and above.
func (f *TestDatabaseFactory) dropDatabaseFrom13(ctx context.Context, name string) error {
	// Drop the database to keep the container from running out of resources.
	// `WITH (FORCE)` statement supported since postgres version 13
	q := fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE);`, name)

	if _, err := f.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("dropping database %q: %w", name, err)
	}

	return nil
}

// dropDatabaseFrom13 drops database queries for postgres older than version 13
func (f *TestDatabaseFactory) dropDatabaseBefore13(ctx context.Context, name string) error {
	conn, err := f.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}

	// Disallow new connections to this database.
	q1 := fmt.Sprintf(`UPDATE pg_database SET datallowconn = false WHERE datname = '%s';`, name)

	// Force disconnection of all clients connected to this database.
	q2 := fmt.Sprintf(`
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = '%s'
		AND pid <> pg_backend_pid();
	`, name)

	// Drop the database to keep the container from running out of resources.
	q3 := fmt.Sprintf(`DROP DATABASE IF EXISTS %q;`, name)

	for i, q := range []string{q1, q2, q3} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("dropping database %q (before v13: q=%d):%w", name, i+1, err)
		}
	}

	return nil
}

// generateDatabaseName generates new database name.
func (f *TestDatabaseFactory) generateDatabaseName() string {
	n := atomic.AddUint32(&f.cloneCounter, 1)
	return fmt.Sprintf("%s_%d", f.baseName, n)
}

// withTimeout calls fn with the default timeout passed in the context.
func (f *TestDatabaseFactory) withTimeout(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), testDatabaseFactoryTimeout)
	defer cancel()

	return fn(ctx)
}

// postgresMajorVersion parses major version from string full version information.
// Expected format: "13.3" or "9.6.22"
func postgresMajorVersion(ver string) (int, error) {
	const splitParts = 2

	parts := strings.SplitN(ver, ".", splitParts)
	if len(parts) != splitParts {
		return 0, fmt.Errorf("invalid format of full version information: %q", ver) //nolint:goerr113 // Static error is an overhead.
	}

	majorVer, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid format of major version from full version information (version: %q): %w", ver, err)
	}

	return majorVer, nil
}

// randomDatabaseName returns a random database name.
func randomDatabaseName(prefix string) (string, error) {
	const randomByteLength = 4

	b := make([]byte, randomByteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err //nolint:wrapcheck // Error wrapping is not required.
	}

	return prefix + hex.EncodeToString(b), nil
}
