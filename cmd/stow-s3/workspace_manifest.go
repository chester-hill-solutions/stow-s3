package main

import "path/filepath"

func resolveManifestPath(base, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}
