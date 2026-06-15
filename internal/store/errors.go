package store

import "errors"

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicate is returned when a unique constraint is violated.
	ErrDuplicate = errors.New("already exists")
	// ErrInvalid is returned when input violates a known data rule.
	ErrInvalid = errors.New("invalid input")
)
