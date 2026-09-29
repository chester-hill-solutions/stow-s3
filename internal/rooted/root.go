package rooted

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Root pins a directory identity and refuses symbolic links in paths it reads.
type Root struct {
	path string
	root *os.Root
	info os.FileInfo
}

func Open(path string) (*Root, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("rooted: root must be a real directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		root.Close()
		return nil, fmt.Errorf("rooted: root changed while opening")
	}
	return &Root{path: path, root: root, info: info}, nil
}

func (r *Root) Close() error      { return r.root.Close() }
func (r *Root) FS() fs.FS         { return r.root.FS() }
func (r *Root) Info() os.FileInfo { return r.info }

func (r *Root) Check() error {
	actual, err := os.Lstat(r.path)
	if err != nil {
		return err
	}
	if !actual.IsDir() || !os.SameFile(r.info, actual) {
		return fmt.Errorf("rooted: directory identity changed")
	}
	return nil
}

func (r *Root) OpenFile(relative string) (*os.File, error) {
	return r.openFile(relative, false)
}

func (r *Root) OpenRegularFile(relative string) (*os.File, error) {
	return r.openFile(relative, true)
}

func (r *Root) openFile(relative string, regular bool) (*os.File, error) {
	if err := r.Check(); err != nil {
		return nil, err
	}
	if !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("rooted: path is not local")
	}
	info, err := r.statWithoutLinks(relative)
	if err != nil {
		return nil, err
	}
	if regular && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("rooted: file must be regular")
	}
	file, err := r.open(relative, regular)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		file.Close()
		return nil, fmt.Errorf("rooted: file changed while opening")
	}
	return file, nil
}

func (r *Root) open(relative string, regular bool) (*os.File, error) {
	if regular {
		return openRegularFile(r.root, relative)
	}
	return r.root.Open(relative)
}

func (r *Root) statWithoutLinks(relative string) (os.FileInfo, error) {
	var info os.FileInfo
	current := "."
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "" || part == ".." {
			return nil, fmt.Errorf("rooted: unsafe path component")
		}
		current = filepath.Join(current, part)
		var err error
		info, err = r.root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("rooted: symbolic links are not readable")
		}
	}
	return info, nil
}
