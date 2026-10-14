package recoveryerrors

import "errors"

var (
	NotFound = errors.New("workspace: recovery hold not found")
	Full     = errors.New("workspace: recovery hold capacity exhausted")
)
