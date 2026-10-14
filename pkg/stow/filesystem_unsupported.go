//go:build !linux && !darwin

package stow

func OpenFilesystem(FilesystemOptions) (*Runtime, error) {
	return nil, ErrUnsupportedBackend
}
