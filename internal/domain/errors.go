// Package domain holds the business model of the hub: identifiers, connections, notifications,
// notification targets and the catalog of business errors. It has no dependency on transport or storage.
package domain

import (
	"errors"
	"fmt"
)

// ErrorType classifies a business error; the API maps it to an HTTP status code.
type ErrorType int

const (
	// ErrorTypeFailure is an unexpected or unsupported situation.
	ErrorTypeFailure ErrorType = iota
	// ErrorTypeValidation is an invalid input.
	ErrorTypeValidation
	// ErrorTypeNotFound is a missing resource.
	ErrorTypeNotFound
	// ErrorTypeConflict is a state conflict.
	ErrorTypeConflict
)

// String returns the name of the error type.
func (t ErrorType) String() string {
	switch t {
	case ErrorTypeValidation:
		return "Validation"
	case ErrorTypeNotFound:
		return "NotFound"
	case ErrorTypeConflict:
		return "Conflict"
	default:
		return "Failure"
	}
}

// Error is a business error returned as a value. Fields holds the validation failures grouped by
// field when the error comes from validating a command.
type Error struct {
	Code        string
	Description string
	Type        ErrorType
	Fields      map[string][]string
}

// Error implements the error interface.
func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Description) }

// Is reports whether target is a business error with the same code, so errors.Is works with the catalog.
func (e *Error) Is(target error) bool {
	var other *Error
	return errors.As(target, &other) && other.Code == e.Code
}

// NewFailure creates an error of type Failure.
func NewFailure(code, description string) *Error {
	return &Error{Code: code, Description: description, Type: ErrorTypeFailure}
}

// NewValidation creates an error of type Validation.
func NewValidation(code, description string) *Error {
	return &Error{Code: code, Description: description, Type: ErrorTypeValidation}
}

// NewNotFound creates an error of type NotFound.
func NewNotFound(code, description string) *Error {
	return &Error{Code: code, Description: description, Type: ErrorTypeNotFound}
}

// NewConflict creates an error of type Conflict.
func NewConflict(code, description string) *Error {
	return &Error{Code: code, Description: description, Type: ErrorTypeConflict}
}

// ValidationErrorCode is the code of the error that groups the failures of a command validation.
const ValidationErrorCode = "General.Validation"

// NewValidationErrors creates the error that reports the validation failures of a command, grouped by field.
func NewValidationErrors(fields map[string][]string) *Error {
	return &Error{
		Code:        ValidationErrorCode,
		Description: "One or more validation errors occurred.",
		Type:        ErrorTypeValidation,
		Fields:      fields,
	}
}

// AsError returns the business error wrapped in err, if any.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
