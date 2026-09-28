package stow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Staging a declared input: where it may land, and which paths are refused without
// an explicit opt-in. Separate from prepare.go because that file decides whether a
// workspace may be created and this one decides whether a path may be written, and
// the sensitive-path rules have to be reviewable without the quota logic above them.

func safeDestination(root, destination string) (string, error) {
	if destination == "" {
		destination = "."
	}
	if filepath.IsAbs(destination) || strings.Contains(destination, `\`) {
		return "", fmt.Errorf("must be relative")
	}
	for _, segment := range strings.Split(filepath.ToSlash(destination), "/") {
		if segment == ".." {
			return "", fmt.Errorf("must not contain parent-directory traversal")
		}
		if segment != "" && segment != "." && !validPortablePathSegment(segment) {
			return "", fmt.Errorf("contains a path segment that is not portable")
		}
	}
	clean := filepath.Clean(destination)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must not escape the workspace")
	}
	first := strings.Split(clean, string(filepath.Separator))[0]
	if strings.EqualFold(first, ".stow") {
		return "", fmt.Errorf(".stow is reserved")
	}
	resolved := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must not escape the workspace")
	}
	return resolved, nil
}

func validPortableRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == "" || segment == "." || segment == ".." || !validPortablePathSegment(segment) {
			return false
		}
	}
	return true
}

type seedContext struct {
	includeSensitive bool
	bytes            int64
	objects          int64
}

func copySeed(source, destination string, totals *seedContext) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symbolic link inputs are not supported")
	}
	if info.IsDir() {
		return copySeedDirectory(source, destination, totals)
	}
	if !totals.includeSensitive && sensitiveSeedPath(filepath.Base(source)) {
		return fmt.Errorf("sensitive-looking input %s is excluded by default (set include_sensitive_inputs explicitly to include it)", filepath.Base(source))
	}
	return copySeedFile(source, destination, info, totals)
}

func copySeedDirectory(source, destination string, totals *seedContext) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if !validPortableRelativePath(rel) {
			return fmt.Errorf("input path is not portable: %q", rel)
		}
		return copySeedEntry(source, destination, path, entry, totals)
	})
}

func copySeedEntry(sourceRoot, destinationRoot, path string, entry os.DirEntry, totals *seedContext) error {
	relative, err := filepath.Rel(sourceRoot, path)
	if err != nil {
		return err
	}
	destination := filepath.Join(destinationRoot, relative)
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symbolic link input %s is not supported", path)
	}
	if entry.IsDir() {
		return os.MkdirAll(destination, 0o755)
	}
	if !totals.includeSensitive && sensitiveSeedPath(relative) {
		return fmt.Errorf("sensitive-looking input %s is excluded by default (set include_sensitive_inputs explicitly to include it)", relative)
	}
	return copySeedFile(path, destination, info, totals)
}

func sensitiveSeedPath(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(clean)
	return sensitiveSeedBasename(base) || sensitiveSeedLocation(clean)
}

func sensitiveSeedBasename(base string) bool {
	return base == ".env" || strings.HasPrefix(base, ".env.") ||
		base == ".npmrc" || base == ".netrc" || base == "credentials" ||
		base == "credentials.json" || base == "service-account.json" ||
		base == "id_rsa" || strings.HasPrefix(base, "id_rsa.") ||
		base == "id_ed25519" || strings.HasPrefix(base, "id_ed25519.") ||
		strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") ||
		strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx")
}

func sensitiveSeedLocation(clean string) bool {
	return hasPathPrefix(clean, ".aws/credentials") || hasPathSegment(clean, ".aws/credentials") ||
		hasPathPrefix(clean, ".ssh/") || hasPathSegment(clean, ".ssh/") ||
		hasPathPrefix(clean, ".config/gcloud/credentials.db") || hasPathSegment(clean, ".config/gcloud/credentials.db")
}

func hasPathPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix)
}

func hasPathSegment(path, segment string) bool {
	return strings.Contains(path, "/"+segment)
}

func copySeedFile(source, destination string, info os.FileInfo, totals *seedContext) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("only regular files and directories are supported: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()&0o755)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if n > maxInt64-totals.bytes || totals.objects == maxInt64 {
		return fmt.Errorf("seeded totals exceed supported limits")
	}
	totals.bytes += n
	totals.objects++
	return nil
}
