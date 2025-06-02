package ngrok

import (
	"errors"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
)

// Error is a custom error type that includes a unique ngrok error code.
// All errors that are returned by the ngrok cloud service are of this type.
type Error interface {
	error
	// The unique ngrok error code
	Code() string
}

// errorAdapter implements our Error interface by wrapping ngrok.Error
type errorAdapter struct {
	ngrokErr legacy.Error
}

func (e *errorAdapter) Code() string {
	return e.ngrokErr.ErrorCode()
}

func (e *errorAdapter) Error() string {
	return e.ngrokErr.Error()
}

// wrapError returns the original error or wraps it if it's a ngrok.Error

func wrapError(err error) error {
	if err == nil {
		return nil
	}

	var ngrokErr legacy.Error
	if errors.As(err, &ngrokErr) {
		return &errorAdapter{ngrokErr: ngrokErr}
	}

	return err
}
