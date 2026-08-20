package storage

import "errors"

// ErrNotFound is returned by detail lookups when the ID does not exist.
var ErrNotFound = errors.New("not found")
