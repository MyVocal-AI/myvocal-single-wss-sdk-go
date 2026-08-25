package myvocalsinglewss

import "fmt"

type ValidationError struct{ Message string }

func (err *ValidationError) Error() string { return err.Message }

type StateError struct{ Message string }

func (err *StateError) Error() string { return err.Message }

type TransportError struct {
	Message string
	Cause   error
}

func (err *TransportError) Error() string {
	if err.Cause != nil {
		return fmt.Sprintf("%s: %v", err.Message, err.Cause)
	}
	return err.Message
}

func (err *TransportError) Unwrap() error { return err.Cause }

type AuthenticationError struct{ Message string }

func (err *AuthenticationError) Error() string { return err.Message }

type ProtocolError struct {
	Code    string
	Message string
}

func (err *ProtocolError) Error() string {
	if err.Message == "" {
		return err.Code
	}
	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}
