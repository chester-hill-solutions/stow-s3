package stow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func decodeCheckpointManifest(id string, data []byte) (CheckpointManifest, error) {
	var manifest CheckpointManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CheckpointManifest{}, fmt.Errorf("stow: decode checkpoint %s: %w", id, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return CheckpointManifest{}, fmt.Errorf("stow: trailing checkpoint manifest data")
	}
	if !supportedCheckpointVersion(manifest.Version) || manifest.ID != id {
		return CheckpointManifest{}, fmt.Errorf("stow: checkpoint %s has an unsupported or mismatched manifest", id)
	}
	if err := validateCheckpointManifest(manifest); err != nil {
		return CheckpointManifest{}, err
	}
	return manifest, nil
}
