package stow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func RestoreCheckpoint(registryDir, id string, options WorkspaceOptions) (*Workspace, error) {
	manifest, err := LoadCheckpoint(registryDir, id)
	if err != nil {
		return nil, err
	}
	directory, err := checkpointDirectory(registryDir, id)
	if err != nil {
		return nil, err
	}
	lock, err := workspace.AcquireCapture(filepath.Dir(filepath.Dir(directory)), manifest.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	manifest, err = LoadCheckpoint(registryDir, id)
	if err != nil {
		return nil, err
	}
	if _, err := verifyCheckpointReceiptPayload(context.Background(), directory, manifest); err != nil {
		return nil, err
	}
	if manifest.Version == portableCheckpointVersion {
		if options.Bucket != "" && options.Bucket != manifest.PrimaryBucket {
			return nil, fmt.Errorf("stow: portable restore cannot rename the primary bucket")
		}
		options.Bucket = manifest.PrimaryBucket
	}
	restored, err := restoreCheckpointFiles(directory, manifest, options)
	if err != nil {
		return nil, err
	}
	if manifest.Version == portableCheckpointVersion {
		if err := checkSeedLimits(restored, checkpointManifestBytes(manifest), int64(len(manifest.Objects))); err != nil {
			_ = restored.Destroy(context.Background())
			return nil, err
		}
		err = restorePortableObjects(restored, directory, manifest)
	} else {
		err = restoreCheckpointModes(restored, manifest)
	}
	if err != nil {
		_ = restored.Destroy(context.Background())
		return nil, err
	}
	return restored, nil
}

func restoreCheckpointModes(w *Workspace, manifest CheckpointManifest) error {
	for _, file := range manifest.Files {
		if err := os.Chmod(filepath.Join(w.Dir(), filepath.FromSlash(file.Path)), os.FileMode(file.Mode)&0o777); err != nil {
			return err
		}
	}
	return nil
}

func restoreCheckpointFiles(directory string, manifest CheckpointManifest, options WorkspaceOptions) (*Workspace, error) {
	inputs := make([]WorkspaceInput, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, err := safeDestination(options.Dir, file.Path); err != nil {
			return nil, err
		}
		inputs = append(inputs, WorkspaceInput{Source: filepath.Join(directory, "files", filepath.FromSlash(file.Path)), Destination: file.Path})
	}
	if len(inputs) == 0 {
		root, err := validatePrepareRoot(options.Dir)
		if err != nil {
			return nil, err
		}
		if options.Authority == nil {
			grant := ReadWrite()
			options.Authority = &grant
		}
		return openPreparedWorkspace(options, root)
	}
	prepared, err := PrepareWorkspace(PrepareOptions{WorkspaceOptions: options, Inputs: inputs, IncludeSensitiveInputs: true})
	if err != nil {
		return nil, err
	}
	return prepared.Workspace, nil
}
