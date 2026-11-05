package auth

import "fmt"

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func authError(code, format string, args ...any) error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}
