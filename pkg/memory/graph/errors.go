package graph

import "errors"

// ErrInvalidInput is returned when a tool request is missing required fields.
var ErrInvalidInput = errors.New("invalid input: required fields missing")
