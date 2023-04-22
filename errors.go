package pglocker

import "errors"

var (
	// ErrAlreadyLocked returns if the lock has already been taken.
	ErrAlreadyLocked = errors.New("pglocker: already locked")

	// ErrWrongGeneration returns if the lock generation does not match
	// the generation of the current lock.
	ErrWrongGeneration = errors.New("pglocker: wrong generation")

	// ErrLockNotFound returns if the lock status is not found in the database.
	ErrLockNotFound = errors.New("pglocker: lock not found")

	// ErrInvalidRelockInterval returns if used invalid relock interval.
	// For example, less than the TTL of the lock.
	ErrInvalidRelockInterval = errors.New("pglocker: invalid relock interval")

	// ErrConfigIsNil returns if passed configuration is nil.
	ErrConfigIsNil = errors.New("pglocker: config is nil")
)
