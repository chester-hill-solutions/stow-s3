package stow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestPrepareRejectsDirectoryInputContainingFutureWorkspaceBeforeMutation(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "source-alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		source    string
		root      string
		rootSetup func(t *testing.T) string
	}{
		{
			name:   "direct nesting",
			source: base,
			root:   filepath.Join(base, "workspace"),
		},
		{
			name:   "source alias via symlink",
			source: alias,
			root:   filepath.Join(base, "workspace"),
		},
		{
			name:   "future root below aliased parent",
			source: base,
			rootSetup: func(t *testing.T) string {
				parentAlias := filepath.Join(t.TempDir(), "parent-alias")
				if err := os.Symlink(base, parentAlias); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(parentAlias, "workspace")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.root
			if tc.rootSetup != nil {
				root = tc.rootSetup(t)
			}
			registry := filepath.Join(t.TempDir(), "registry")
			_, err := stow.PrepareWorkspace(stow.PrepareOptions{
				WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: registry},
				Inputs:           []stow.WorkspaceInput{{Source: tc.source, Destination: "input"}},
			})
			if err == nil {
				t.Fatal("PrepareWorkspace succeeded, want recursive-input refusal")
			}
			if !strings.Contains(err.Error(), "contains prepare destination") {
				t.Fatalf("error = %v, want recursive-input refusal", err)
			}
			if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
				t.Fatalf("workspace root exists after refusal (Lstat error: %v)", statErr)
			}
			if _, statErr := os.Lstat(registry); !os.IsNotExist(statErr) {
				t.Fatalf("registry exists after refusal (Lstat error: %v)", statErr)
			}
		})
	}
}

func TestPrepareAllowsDirectoryInputBesideWorkspace(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(parent, "workspace"), RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Inputs:           []stow.WorkspaceInput{{Source: source, Destination: "input"}},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	defer prepared.Workspace.Close()
	if _, err := os.Stat(filepath.Join(prepared.Workspace.Dir(), "input", "seed.txt")); err != nil {
		t.Fatalf("seeded sibling input missing: %v", err)
	}
}

func TestPrepareRejectsCaseAliasOnCaseInsensitiveFilesystem(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "seedcase")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasedParent := filepath.Join(parent, "SeedCase")
	aliasedInfo, err := os.Stat(aliasedParent)
	if err != nil {
		t.Skip("filesystem is case-sensitive")
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(sourceInfo, aliasedInfo) {
		t.Skip("filesystem is case-sensitive")
	}
	root := filepath.Join(aliasedParent, "workspace")
	registry := filepath.Join(t.TempDir(), "registry")
	_, err = stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: registry},
		Inputs:           []stow.WorkspaceInput{{Source: source, Destination: "input"}},
	})
	if err == nil {
		t.Fatal("PrepareWorkspace succeeded, want recursive-input refusal")
	}
	if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
		t.Fatalf("workspace root exists after refusal (Lstat error: %v)", statErr)
	}
	if _, statErr := os.Lstat(registry); !os.IsNotExist(statErr) {
		t.Fatalf("registry exists after refusal (Lstat error: %v)", statErr)
	}
}

func TestPrepareFingerprintUsesUnambiguousBinaryFileFraming(t *testing.T) {
	type tree struct {
		name  string
		files map[string][]byte
	}
	trees := []tree{
		{
			name: "split delimiter bytes across files",
			files: map[string][]byte{
				"a": []byte("x\x00b\x00y"),
				"c": []byte("z"),
			},
		},
		{
			name: "different file boundaries",
			files: map[string][]byte{
				"a": []byte("x"),
				"b": []byte("y\x00c\x00z"),
			},
		},
		{
			name: "equivalent copy",
			files: map[string][]byte{
				"a": []byte("x\x00b\x00y"),
				"c": []byte("z"),
			},
		},
	}
	identities := make([]string, 0, len(trees))
	for _, tree := range trees {
		t.Run(tree.name, func(t *testing.T) {
			source := t.TempDir()
			for name, content := range tree.files {
				if err := os.WriteFile(filepath.Join(source, name), content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
				WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: filepath.Join(t.TempDir(), "registry")},
				Inputs:           []stow.WorkspaceInput{{Source: source, Destination: "."}},
			})
			if err != nil {
				t.Fatalf("PrepareWorkspace: %v", err)
			}
			identities = append(identities, prepared.BaseIdentity)
			if err := prepared.Workspace.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if identities[0] == identities[1] {
		t.Fatalf("different file trees have the same BaseIdentity: %s", identities[0])
	}
	if identities[0] != identities[2] {
		t.Fatalf("equivalent file trees have different BaseIdentity values: %s and %s", identities[0], identities[2])
	}
}
