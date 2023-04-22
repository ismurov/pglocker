package pglocker_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	// Registration of SQL driver.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ismurov/pglocker"
	"github.com/ismurov/pglocker/pgtesting"
)

var testDatabaseInstance *pgtesting.TestInstance

func TestMain(m *testing.M) {
	var code int
	defer func() { os.Exit(code) }()

	// Initializing a database instance for all package tests.
	testDatabaseInstance = pgtesting.MustTestInstance(&pgtesting.TestInstanceConfig{
		DriverName:  "pgx",
		DatabaseURL: os.Getenv("CI_DATABASE_URL"),
	})
	defer testDatabaseInstance.MustClose()

	// Run package tests.
	code = m.Run()
}

// newLocker creates new locker for testing.
func newLocker(tb testing.TB) *pglocker.Locker {
	tb.Helper()

	ctx := context.Background()
	db := testDatabaseInstance.Open(tb,
		testDatabaseInstance.NewDatabase(tb).String(),
	)

	cfg := &pglocker.Config{
		Scheme: pglocker.DefaultScheme,
		Table:  pglocker.DefaultTable,
	}

	l, err := pglocker.NewWithMigrateDB(ctx, db, cfg)
	if err != nil {
		tb.Fatalf("pglocker.NewWithMigrateDB: %v", err)
	}

	return l
}

func TestNew_NilConfig(t *testing.T) {
	t.Parallel()

	var db sql.DB // <– This is not valid usage!

	_, err := pglocker.New(&db, nil)
	if !errors.Is(err, pglocker.ErrConfigIsNil) {
		t.Errorf("got unexpected error: %v", err)
	}
}

func TestNewWithMigrate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config pglocker.Config
	}{
		{
			name: "default config",
		},
		{
			name: "custom config",
			config: pglocker.Config{
				Scheme: "test_shema",
				Table:  "test_lock_table",
			},
		},
		{
			name: "custom config with upper letters",
			config: pglocker.Config{
				Scheme: "TestShema-99",
				Table:  "TestLockTable_103",
			},
		},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db := testDatabaseInstance.Open(t,
				testDatabaseInstance.NewDatabase(t).String(),
			)

			l, err := pglocker.NewWithMigrateDB(ctx, db, &tc.config)
			if err != nil {
				t.Fatalf("pglocker.NewWithMigrateDB: %v", err)
			}

			lock, err := l.AcquireLock(ctx, "lock-99", time.Minute)
			if err != nil {
				t.Fatalf("AcquireLock: %v", err)
			}

			if err = l.ReleaseLock(ctx, lock); err != nil {
				t.Fatalf("ReleaseLock: %v", err)
			}
		})
	}
}

func TestLocker_CreateLock(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-create"

	lock1, err := l.CreateLock(ctx, lockID)
	if err != nil {
		t.Fatalf("[lock 1] CreateLock: %v", err)
	}

	// If the lock already exists, it's a noop.
	lock2, err := l.CreateLock(ctx, lockID)
	if err != nil {
		t.Fatalf("[lock 2] CreateLock: %v", err)
	}

	if diff := cmp.Diff(lock1, lock2); diff != "" {
		t.Errorf("mismatch (-lock1, +lock2):\n%s", diff)
	}
}

func TestLocker_GetLockStatus(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-get"

	// The lock has not been created yet.
	_, err := l.GetLockStatus(ctx, lockID)
	if got, want := err, pglocker.ErrLockNotFound; !errors.Is(got, want) {
		t.Fatalf("got unexpected error:\ngot:  %v\nwant: %v", got, want)
	}

	lock1, err := l.CreateLock(ctx, lockID)
	if err != nil {
		t.Fatalf("[lock 1] CreateLock: %v", err)
	}

	lock2, err := l.GetLockStatus(ctx, lockID)
	if err != nil {
		t.Fatalf("[lock 2] GetLockStatus: %v", err)
	}

	if diff := cmp.Diff(lock1, lock2); diff != "" {
		t.Errorf("mismatch (-lock1, +lock2):\n%s", diff)
	}
}

func TestLocker_AcquireLock(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-acquire"
	lockTTL := 100 * time.Millisecond

	lock1, err := l.AcquireLock(ctx, lockID, lockTTL)
	if err != nil {
		t.Fatalf("[lock 1] failed accurate lock: %v", err)
	}
	if got, want := lock1.Generation, int64(2); got != want {
		t.Errorf("[lock 1] wrong generation:\ngot:  %v\nwant: %v", got, want)
	}

	lock2, err := l.AcquireLock(ctx, lockID, lockTTL)
	if got, want := err, pglocker.ErrAlreadyLocked; !errors.Is(got, want) {
		t.Errorf("[lock 2] got unexpected error:\ngot:  %v\nwant: %v\nlock: %v", got, want, lock2)
	}

	// Waiting for the lock TTL to expire.
	time.Sleep(lockTTL)

	lock3, err := l.AcquireLock(ctx, lockID, lockTTL)
	if err != nil {
		t.Fatalf("[lock 3] failed accurate lock: %v", err)
	}
	if got, want := lock3.Generation, int64(3); got != want {
		t.Errorf("[lock 3] wrong generation:\ngot:  %v\nwant: %v", got, want)
	}
}

func TestLocker_RefreshLock_noExist(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-nope"
	lockTTL := time.Minute

	lock, err := l.RefreshLock(ctx, &pglocker.LockStatus{
		LockID: lockID,
	}, lockTTL)
	if got, want := err, pglocker.ErrLockNotFound; !errors.Is(got, want) {
		t.Errorf("got unexpected error:\ngot:  %v\nwant: %v\nlock: %v", got, want, lock)
	}
}

func TestLocker_RefreshLock_wrongGeneration(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-dirty"
	lockTTL := time.Minute

	if _, err := l.CreateLock(ctx, lockID); err != nil {
		t.Fatalf("CreateLock: %v", err)
	}

	lock, err := l.RefreshLock(ctx, &pglocker.LockStatus{
		LockID:     lockID,
		Generation: 2,
	}, lockTTL)
	if got, want := err, pglocker.ErrWrongGeneration; !errors.Is(got, want) {
		t.Errorf("got unexpected error:\ngot:  %v\nwant: %v\nlock: %v", got, want, lock)
	}
}

func TestLocker_RefreshLock_exists(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-success"
	lockTTL := time.Minute

	if _, err := l.CreateLock(ctx, lockID); err != nil {
		t.Fatalf("CreateLock: %v", err)
	}

	lock, err := l.RefreshLock(ctx, &pglocker.LockStatus{
		LockID:     lockID,
		Generation: 1,
	}, lockTTL)
	if err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}

	if got, want := lock.Generation, int64(2); got != want {
		t.Errorf("wrong generation:\ngot:  %v\nwant: %v", got, want)
	}

	if got, now := lock.ExpiresAt, time.Now().UTC(); got.Before(now) {
		t.Errorf("wrong expires at: expected %q to be after %q", got, now)
	}
}

func TestLocker_ReleaseLock_noExist(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-nope"

	err := l.ReleaseLock(ctx, &pglocker.LockStatus{LockID: lockID})
	if got, want := err, pglocker.ErrLockNotFound; !errors.Is(got, want) {
		t.Errorf("got unexpected error:\ngot:  %v\nwant: %v", got, want)
	}
}

func TestLocker_ReleaseLock_wrongGeneration(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-dirty"

	if _, err := l.CreateLock(ctx, lockID); err != nil {
		t.Fatalf("CreateLock: %v", err)
	}

	err := l.ReleaseLock(ctx, &pglocker.LockStatus{
		LockID:     lockID,
		Generation: 2,
	})
	if got, want := err, pglocker.ErrWrongGeneration; !errors.Is(got, want) {
		t.Errorf("got unexpected error:\ngot:  %v\nwant: %v", got, want)
	}
}

func TestLocker_ReleaseLock_exists(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	lockID := "lock-success"
	lockTTL := time.Minute

	lock1, err := l.AcquireLock(ctx, lockID, lockTTL)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	if err := l.ReleaseLock(ctx, lock1); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	lock2, err := l.GetLockStatus(ctx, lockID)
	if err != nil {
		t.Fatalf("GetLockStatus: %v", err)
	}

	if got, want := lock2.Generation, int64(3); got != want {
		t.Errorf("wrong generation:\ngot:  %v\nwant: %v", got, want)
	}

	if got, now := lock2.ExpiresAt, time.Now().UTC(); got.After(now) {
		t.Errorf("wrong expires at: expected %q to be before %q", got, now)
	}
}

func TestLocker_Do_simple(t *testing.T) {
	t.Parallel()

	l := newLocker(t)
	ctx := context.Background()

	var (
		lockID         = "lock-do"
		lockTTL        = time.Minute
		relockInterval = 10 * time.Millisecond
		taskDuration   = 100 * time.Millisecond
	)

	if doErr := l.Do(ctx, lockID, lockTTL, relockInterval, func(doCtx context.Context) error {
		select {
		case <-doCtx.Done():
			return doCtx.Err() //nolint:wrapcheck // Returns original error.
		case <-time.After(taskDuration):
			return nil
		}
	}); doErr != nil {
		t.Fatalf("got unexpected error: %v", doErr)
	}
}
