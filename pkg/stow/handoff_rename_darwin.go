package stow

import "golang.org/x/sys/unix"

func publishHandoffDirectory(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
