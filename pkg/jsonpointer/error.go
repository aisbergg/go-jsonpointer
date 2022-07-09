package jsonpointer

import (
	"fmt"
)

// ErrType represents the type of error.
type ErrType int

const (
	// ErrUnknown indicates an invalid error.
	ErrUnknown ErrType = iota

	// ErrInvalidJSONPointer indicates an error for parsing a JSON pointer.
	ErrInvalidJSONPointer

	// ErrGet indicates an error for getting a value.
	ErrGet

	// ErrSet indicates an error for setting a value.
	ErrSet
)

func (t ErrType) String() string {
	switch t {
	case ErrInvalidJSONPointer:
		return "invalid pointer"
	case ErrGet:
		return "get"
	case ErrSet:
		return "set"
	}
	return "unknown"
}

// Error represents a JSON pointer error.
type Error struct {
	msg     string
	cause   error
	errType ErrType
}

func newError(errType ErrType, format string, args ...interface{}) *Error {
	return &Error{
		msg:     fmt.Sprintf(format, args...),
		errType: errType,
	}
}

func wrapError(err error, errType ErrType, format string, args ...interface{}) *Error {
	return &Error{
		msg:     fmt.Sprintf(format, args...),
		cause:   err,
		errType: errType,
	}
}

// Error returns the formatted error message.
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.errType, e.msg)
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.cause
}
