package pglocker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
)

// errInTxSentinel this is the sentinel's error of InTx function
// for unexpected corner cases.
var errInTxSentinel = errors.New("pglocker: InTx sentinel error")

// InTx runs the given function f within a transaction with isolation level isoLevel.
func inTx(ctx context.Context, db *sql.DB, isoLevel sql.IsolationLevel, f func(tx *sql.Tx) error) (txErr error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{
		Isolation: isoLevel,
	})
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}

	var txDone bool
	defer func() {
		// Make sure to rollback when panic.
		if txDone || txErr != nil {
			return
		}

		// Make sure that the transaction caused by the panic
		// does not return a nil error.
		txErr = errInTxSentinel

		if err := tx.Rollback(); err != nil {
			log.Printf("pglocker.inTx: failed to rollback transaction correctly: %v", err)
		}
	}()

	if err := f(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rolling back transaction: %w (original error: %v)", rbErr, err) //nolint:errorlint // Wraps only one error.
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	// Mark that the transaction was completed successfully.
	txDone = true

	return nil
}
