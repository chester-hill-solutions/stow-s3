package stow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validatePrepareOptions(options PrepareOptions) (string, error) {
	if options.Dir == "" {
		return "", fmt.Errorf("stow: prepare requires a workspace Dir")
	}
	if len(options.Inputs) == 0 && len(options.Repositories) == 0 {
		return "", fmt.Errorf("stow: prepare requires at least one input")
	}
	root, err := validatePrepareRoot(options.Dir)
	if err != nil {
		return "", err
	}
	if err := validatePrepareInputsOutsideRoot(root, options.Inputs); err != nil {
		return "", err
	}
	return root, nil
}

func validatePrepareInputsOutsideRoot(root string, inputs []WorkspaceInput) error {
	if len(inputs) == 0 {
		return nil
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		return fmt.Errorf("stow: resolve prepare destination parent for input validation: %w", err)
	}
	for _, input := range inputs {
		if strings.TrimSpace(input.Source) == "" {
			continue
		}
		info, err := os.Stat(input.Source)
		if err != nil || !info.IsDir() {
			continue
		}
		if existingAncestorIsSameDirectory(info, parent) {
			return fmt.Errorf("stow: directory input %q contains prepare destination %s", input.Source, root)
		}
	}
	return nil
}

func existingAncestorIsSameDirectory(sourceInfo os.FileInfo, start string) bool {
	for ancestor := start; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Stat(ancestor)
		if err == nil && info.IsDir() && os.SameFile(sourceInfo, info) {
			return true
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false
		}
	}
}
