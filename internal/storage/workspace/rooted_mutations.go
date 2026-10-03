package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

func (s *Store) writeAtomic(path string, data []byte) error {
	root, err := s.readRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		return err
	}
	return root.WriteAtomic(relative, data, 0644)
}

func (s *Store) removeConfined(path string) error {
	root, err := s.readRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		return err
	}
	return root.Remove(relative)
}

func writeDocumentAtomic(path string, data []byte, expected os.FileInfo) error {
	root, err := rooted.Open(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return err
	}
	defer root.Close()
	if expected != nil && !os.SameFile(expected, root.Info()) {
		return fmt.Errorf("workspace: root identity changed")
	}
	relative, err := filepath.Rel(filepath.Dir(filepath.Dir(path)), path)
	if err != nil {
		return err
	}
	return root.WriteAtomic(relative, data, 0644)
}
