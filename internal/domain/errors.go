package domain

import "errors"

// ErrNotFound is returned when a lookup by identifier matches nothing.
var ErrNotFound = errors.New("not found")
