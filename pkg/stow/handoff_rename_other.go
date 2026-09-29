//go:build !linux && !darwin

package stow

import "errors"

func publishHandoffDirectory(source, destination string) error {
	return errors.New("stow: atomic handoff directory publication is unsupported on this platform")
}
