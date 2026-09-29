package stow

import (
	"fmt"
	"path/filepath"
)

type HandoffError struct {
	BundleDir string
	Outcome   string
	Err       error
}

func (e *HandoffError) Error() string {
	return fmt.Sprintf("handoff published at %s; durability outcome %s: %v", e.BundleDir, e.Outcome, e.Err)
}
func (e *HandoffError) Unwrap() error { return e.Err }

func commitHandoff(stage, destination string, syncParent func(string) error) error {
	if err := publishHandoffDirectory(stage, destination); err != nil {
		return err
	}
	if err := syncParent(filepath.Dir(destination)); err != nil {
		return &HandoffError{BundleDir: destination, Outcome: "unknown", Err: err}
	}
	return nil
}
