// Package pglocker provides a simple utility for using PostgreSQL
// for managing distributed locks.
package pglocker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// LockStatus represents a distributed lock that spaces operations out.
// These are only self expring locks (ExpiresAt) and are not explicitly
// released.
type LockStatus struct {
	LockID     string
	Generation int64
	ExpiresAt  time.Time
}

// Locker is a service that implements a distributed locking mechanism.
type Locker struct {
	tableName string
	db        *sql.DB
}

// New creates a new distributed locking service.
//
// Database migrations are not applied during initialization.
// For applying them use MigrateDB or NewWithMigrateDB functions.
func New(db *sql.DB, cfg *Config) (*Locker, error) {
	if cfg == nil {
		return nil, ErrConfigIsNil
	}

	cfg.SetDefault()

	return &Locker{
		tableName: fmt.Sprintf("%q.%q", cfg.Scheme, cfg.Table),
		db:        db,
	}, nil
}

// NewWithMigrateDB creates a new distributed locking service
// and applies the necessary migrations to the database.
func NewWithMigrateDB(ctx context.Context, db *sql.DB, cfg *Config) (*Locker, error) {
	l, err := New(db, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating new locker: %w", err)
	}

	if err = MigrateDB(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return l, nil
}

// CreateLock creates new lock status in database or fetches existing.
func (l *Locker) CreateLock(ctx context.Context, lockID string) (*LockStatus, error) {
	return l.createLock(ctx, lockID)
}

// GetLockStatus returns a lock status from the database by the lockID.
// If the lock status is not found in the database, error ErrLockNotFound
// will be returned.
func (l *Locker) GetLockStatus(ctx context.Context, lockID string) (*LockStatus, error) {
	return l.fetchLock(ctx, lockID)
}

// AcquireLock attempts to grab the lock with the given lockID and TTL lock
// expiration. If the lock does not exist in the database, it will be created.
// If the lock is already locked, ErrAlreadyLocked error will be returned.
func (l *Locker) AcquireLock(ctx context.Context, lockID string, lockTTL time.Duration) (*LockStatus, error) {
	status, err := l.createLock(ctx, lockID)
	if err != nil {
		return nil, fmt.Errorf("creating %s lock: %w", lockID, err)
	}

	if status.ExpiresAt.After(time.Now().UTC()) {
		return nil, ErrAlreadyLocked
	}

	// An attempt to move the generation forward.
	status, err = l.claimLock(ctx, status, lockTTL)
	if err != nil {
		if errors.Is(err, ErrWrongGeneration) {
			return nil, ErrAlreadyLocked
		}
		return nil, fmt.Errorf("claim %s lock: %w", lockID, err)
	}

	return status, nil
}

// RefreshLock extends the lock lifetime by a time interval of duration equal
// to lockTTL. If the lock generation does not match the generation of the
// current lock, ErrWrongGeneration error will be returned.
func (l *Locker) RefreshLock(ctx context.Context, lock *LockStatus, lockTTL time.Duration) (*LockStatus, error) {
	status, err := l.claimLock(ctx, lock, lockTTL)
	if err != nil {
		return nil, fmt.Errorf("claim %s lock: %w", lock.LockID, err)
	}

	return status, nil
}

// ReleaseLock releases the lock explicitly. If the lock generation does
// not match the generation of the current lock, ErrWrongGeneration error
// will be returned.
func (l *Locker) ReleaseLock(ctx context.Context, lock *LockStatus) error {
	if _, err := l.claimLock(ctx, lock, 0); err != nil {
		return fmt.Errorf("unclaim %s lock: %w", lock.LockID, err)
	}

	return nil
}

// claimLock extends the lock lifetime by a time interval of duration equal
// to lockTTL. If the lock generation does not match the generation of the
// current lock, ErrWrongGeneration error will be returned.
func (l *Locker) claimLock(ctx context.Context, current *LockStatus, lockTTL time.Duration) (*LockStatus, error) {
	var status LockStatus

	if txErr := inTx(ctx, l.db, sql.LevelReadCommitted, func(tx *sql.Tx) error {
		if err := l.fetchLockForUpdateTx(ctx, tx, current.LockID, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrLockNotFound
			}

			return fmt.Errorf("fetching lock status: %w", err)
		}

		if status.Generation != current.Generation {
			return ErrWrongGeneration
		}

		status.Generation++
		status.ExpiresAt = time.Now().UTC().Add(lockTTL)

		if err := l.updateLockInTx(ctx, tx, &status); err != nil {
			return fmt.Errorf("updating lock status: %w", err)
		}

		return nil
	}); txErr != nil {
		return nil, txErr
	}

	return &status, nil
}

// createLock creates new lock status in database or fetches existing.
func (l *Locker) createLock(ctx context.Context, lockID string) (*LockStatus, error) {
	var status LockStatus

	query := `
		INSERT INTO
			` + l.tableName + ` (lock_id, generation, expires_at)
		VALUES
			($1, $2, $3)
		ON CONFLICT
			(lock_id)
		DO UPDATE SET
			lock_id = EXCLUDED.lock_id
		RETURNING
			*
	`

	if err := scanOneLockStatus(
		l.db.QueryRowContext(ctx, query, lockID, 1, time.Now().UTC()), //nolint:execinquery // Uses RETURNING statement.
		&status,
	); err != nil {
		return nil, fmt.Errorf("fetching lock status: %w", err)
	}

	return &status, nil
}

// fetchLock fetches a lock status from the database by the lockID. If the lock
// status is not found in the database, error ErrLockNotFound will be returned.
func (l *Locker) fetchLock(ctx context.Context, lockID string) (*LockStatus, error) {
	var status LockStatus

	row := l.db.QueryRowContext(ctx, `
		SELECT
			lock_id, generation, expires_at
		FROM
			`+l.tableName+`
		WHERE
			lock_id = $1
	`, lockID)

	if err := scanOneLockStatus(row, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLockNotFound
		}
		return nil, fmt.Errorf("fetching lock status: %w", err)
	}

	return &status, nil
}

// fetchLockForUpdateTx fetches a lock status from the database by the lockID.
// The received data is filled in out.
func (l *Locker) fetchLockForUpdateTx(ctx context.Context, tx *sql.Tx, lockID string, out *LockStatus) error {
	row := tx.QueryRowContext(ctx, `
		SELECT
			lock_id, generation, expires_at
		FROM
			`+l.tableName+`
		WHERE
			lock_id = $1
		FOR UPDATE
	`, lockID)

	return scanOneLockStatus(row, out)
}

// errLockStatusNotUpdated is an internal error that returns when a lock status
// has not been updated.
var errLockStatusNotUpdated = errors.New("pglocker: lock status has not been updated")

// updateLockInTx updates the lock status in the transaction.
func (l *Locker) updateLockInTx(ctx context.Context, tx *sql.Tx, m *LockStatus) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE
			`+l.tableName+`
		SET
			generation = $2,
			expires_at = $3
		WHERE
			lock_id = $1
	`, m.LockID, m.Generation, m.ExpiresAt)
	if err != nil {
		return fmt.Errorf("executing an update query: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected count: %w", err)
	}

	switch count {
	case 0:
		return errLockStatusNotUpdated
	case 1:
		return nil
	default:
		return errors.New("more than one entry has been updated: " + strconv.FormatInt(count, 10)) //nolint:goerr113 // This is internal corner case error.
	}
}

// scanOneLockStatus scans one lock status record from database
func scanOneLockStatus(row *sql.Row, m *LockStatus) error {
	//nolint:wrapcheck // Error wrapping is not required.
	return row.Scan(
		&m.LockID,
		&m.Generation,
		&m.ExpiresAt,
	)
}

// Do executes fn while holding the lock for the lockID. When the lock loss is
// detected in the re-locking process, it is going to cancel the context passed
// on to fn. If it ends normally, it releases the lock. If the lock is already
// locked, ErrAlreadyLocked error will be returned.
func (l *Locker) Do(ctx context.Context, lockID string, lockTTL, relockInterval time.Duration, fn func(context.Context) error) (doErr error) {
	if relockInterval <= 0 || relockInterval >= lockTTL {
		return ErrInvalidRelockInterval
	}

	lock, err := l.AcquireLock(ctx, lockID, lockTTL)
	if err != nil {
		if !errors.Is(err, ErrAlreadyLocked) {
			err = fmt.Errorf("acquiring lock: %w", err)
		}
		return err
	}

	errChan := make(chan error, 1)
	defer func() {
		bgErr := <-errChan
		if bgErr == nil || errors.Is(bgErr, context.Canceled) {
			return
		}

		if doErr == nil || errors.Is(doErr, context.Canceled) {
			doErr = fmt.Errorf("relocking process: %w", bgErr)
			return
		}

		doErr = fmt.Errorf("relocking process: %w (original error: %v)", bgErr, doErr) //nolint:errorlint // Wraps only one error.
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Starting background re-locking process.
	go func() {
		defer cancel()

		errChan <- panicSafeWrapper(func() error {
			return l.StartRelocker(ctx, lock, lockTTL, relockInterval)
		})
	}()

	return fn(ctx)
}

// StartRelocker starts the re-locking process and blocks until the provided
// context is closed. At the end of the process, the lock is explicitly released.
func (l *Locker) StartRelocker(ctx context.Context, lock *LockStatus, lockTTL, relockInterval time.Duration) (err error) {
	if relockInterval <= 0 || relockInterval >= lockTTL {
		return ErrInvalidRelockInterval
	}

	const execTimeout = 30 * time.Second

	defer func() {
		if err != nil {
			// Relock is failed, skip unlock.
			return
		}

		withTimeout(execTimeout, func(rCtx context.Context) {
			if rErr := l.ReleaseLock(rCtx, lock); rErr != nil {
				err = fmt.Errorf("unlock: %w", rErr)
			}
		})
	}()

	ticker := time.NewTicker(relockInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			withTimeout(execTimeout, func(rCtx context.Context) { //nolint:contextcheck // Used detached context for atomic execution.
				lock, err = l.RefreshLock(rCtx, lock, lockTTL)
			})
			if err != nil {
				return fmt.Errorf("relock: %w", err)
			}
		}
	}
}
