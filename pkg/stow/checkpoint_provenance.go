package stow

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CheckpointRepository struct {
	Destination string `json:"destination"`
	Commit      string `json:"commit"`
	SourceLabel string `json:"source_label,omitempty"`
}

type CheckpointProvenance struct {
	Repositories       []CheckpointRepository `json:"repositories,omitempty"`
	OriginCheckpointID string                 `json:"origin_checkpoint_id,omitempty"`
}

func persistPreparedProvenance(root string, repositories []PreparedRepository) error {
	if len(repositories) == 0 {
		return nil
	}
	provenance := CheckpointProvenance{}
	for _, repository := range repositories {
		provenance.Repositories = append(provenance.Repositories, CheckpointRepository{
			Destination: repository.Destination, Commit: repository.Commit, SourceLabel: filepath.Base(repository.Source)})
	}
	return persistCheckpointProvenance(root, &provenance)
}

func persistCheckpointProvenance(root string, provenance *CheckpointProvenance) error {
	if provenance == nil {
		return nil
	}
	if err := validateCheckpointProvenance(provenance); err != nil {
		return err
	}
	data, err := json.Marshal(provenance)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".stow", "provenance.json"), data, 0600)
}

func captureCheckpointProvenance(root string) (*CheckpointProvenance, error) {
	path := filepath.Join(root, ".stow", "provenance.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, fmt.Errorf("stow: invalid workspace provenance file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var provenance CheckpointProvenance
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&provenance); err != nil {
		return nil, err
	}
	if err := validateCheckpointProvenance(&provenance); err != nil {
		return nil, err
	}
	return &provenance, nil
}

func validateCheckpointProvenance(provenance *CheckpointProvenance) error {
	if provenance == nil {
		return nil
	}
	if provenance.OriginCheckpointID != "" && !validCheckpointID(provenance.OriginCheckpointID) {
		return fmt.Errorf("stow: invalid origin checkpoint")
	}
	if len(provenance.Repositories) > 64 {
		return fmt.Errorf("stow: too many provenance repositories")
	}
	seen := make(map[string]bool)
	for _, repository := range provenance.Repositories {
		if !validCheckpointPath(repository.Destination) || seen[repository.Destination] {
			return fmt.Errorf("stow: invalid or duplicate repository destination")
		}
		seen[repository.Destination] = true
		if len(repository.Commit) != 40 && len(repository.Commit) != 64 {
			return fmt.Errorf("stow: invalid Git commit identity")
		}
		if _, err := hex.DecodeString(repository.Commit); err != nil {
			return fmt.Errorf("stow: invalid Git commit identity")
		}
		if len(repository.SourceLabel) > 255 || strings.ContainsAny(repository.SourceLabel, "/\\\x00\n\r") {
			return fmt.Errorf("stow: invalid repository source label")
		}
	}
	return nil
}
