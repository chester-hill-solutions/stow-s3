package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// A handoff is how one workspace's next step is named to another process. There
// are two answers to "on what machine", and version 1 could only express one of
// them.
//
// Version 1 was a same-machine reference: a workspace ID and the registry
// directory holding it. It is still exactly that, and resume still reads it. But
// a reference to a local directory is meaningless to the machine that receives
// it, so handing work to another runner meant the caller exporting an archive by
// hand and telling the receiver to import it — two steps, no binding between the
// reference and the bytes, and nothing to check that the archive is the one the
// reference names.
//
// Version 2 adds an archive to the same document: the checkpoint's bytes, and
// their digest, travel with the reference that names them. The receiver verifies
// the digest, imports the archive (which verifies every file against its own
// manifest), and restores. One document, two integrity checks, one command.
const (
	// handoffLocalVersion is the same-machine reference: an ID and a registry.
	handoffLocalVersion = 1
	// handoffPortableVersion may carry a checkpoint archive.
	handoffPortableVersion = 2
)

// handoffArchive is a checkpoint archive a handoff can name, with the digest that
// binds it to the document.
type handoffArchive struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Files  int64  `json:"files"`
}

type workspaceHandoff struct {
	Version      int             `json:"version"`
	WorkspaceID  string          `json:"workspace_id"`
	CheckpointID string          `json:"checkpoint_id,omitempty"`
	RegistryDir  string          `json:"registry_dir,omitempty"`
	Team         string          `json:"team,omitempty"`
	Archive      *handoffArchive `json:"archive,omitempty"`
}

func handoffWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace handoff", flag.ContinueOnError)
	id := flags.String("id", "", "Workspace ID to hand off")
	checkpointID := flags.String("checkpoint-id", "", "Optional immutable checkpoint to include")
	archive := flags.String("archive", "", "Also write the checkpoint to this portable archive path")
	output := flags.String("output", "", "Write the handoff reference to this file")
	chosen := registryFlag(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("workspace handoff requires --id")
	}
	selection := chosen()
	resolved, err := selection.resolve("")
	if err != nil {
		return err
	}
	// A handoff names a workspace, and naming it does not require holding it. The
	// registry entry is the whole of what is being named, so this looks the
	// workspace up rather than resuming it: an orchestrator hands off a workspace
	// that is still running, and resuming would fail for exactly that reason.
	reference, err := stow.LookupWorkspace(resolved, *id)
	if err != nil {
		return err
	}
	// The reference records the partition as well as the directory it resolved
	// to, so a machine adopting it files the archive under the same team the
	// workspace belonged to rather than into whatever its own root contains.
	document := workspaceHandoff{
		Version: handoffLocalVersion, WorkspaceID: reference.ID,
		CheckpointID: *checkpointID, RegistryDir: resolved, Team: selection.team,
	}
	if *archive != "" {
		if *checkpointID == "" {
			return errors.New("workspace handoff --archive requires --checkpoint-id")
		}
		bound, err := publishHandoffArchive(resolved, *checkpointID, *archive)
		if err != nil {
			return err
		}
		document.Version = handoffPortableVersion
		document.Archive = bound
	} else if *checkpointID != "" {
		if err := requireCheckpointInWorkspace(resolved, *checkpointID, reference.ID); err != nil {
			return err
		}
	}
	return writeWorkspaceJSONTo(*output, document)
}

// adoptHandoffCommand materialises a workspace from a portable handoff on the
// machine that received it.
//
// The order is the design: the document is checked, then the archive's digest is
// checked, then the archive is imported (which verifies every file against the
// manifest inside it), and only then is anything written to the new root. A
// handoff that names bytes it cannot vouch for stops before it creates a
// directory.
func adoptHandoffCommand(args []string) error {
	flags := flag.NewFlagSet("workspace adopt", flag.ContinueOnError)
	handoffPath := flags.String("handoff", "", "Portable handoff reference to adopt")
	root := flags.String("root", "", "New workspace root (must not exist; parent must exist)")
	includeSensitive := flags.Bool("include-sensitive", false, "Allow sensitive-looking paths in the archive")
	maxBytes := flags.Int64("max-bytes", 0, "Uncompressed byte cap (0 uses the 1 GiB default)")
	maxFiles := flags.Int64("max-files", 0, "File count cap (0 uses the 100000-file default)")
	chosen := registryFlag(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *handoffPath == "" || *root == "" {
		return errors.New("workspace adopt requires --handoff and --root")
	}
	if *maxBytes < 0 || *maxFiles < 0 {
		return errors.New("workspace adopt limits must not be negative")
	}
	document, err := readHandoffDocument(*handoffPath)
	if err != nil {
		return err
	}
	selection := chosen()
	if err := checkTeamAgreement(document.Team, selection.team); err != nil {
		return err
	}
	registry, err := selection.resolve(adoptedTeam(document.Team, selection.team))
	if err != nil {
		return err
	}
	imported, err := importHandoffArchive(*handoffPath, registry, document, stow.CheckpointArchiveOptions{
		MaxBytes: *maxBytes, MaxFiles: *maxFiles, IncludeSensitiveFiles: *includeSensitive,
	})
	if err != nil {
		return err
	}
	if err := checkHandoffIdentity(document, imported); err != nil {
		return err
	}
	ws, err := stow.RestoreCheckpoint(registry, imported.ID, stow.WorkspaceOptions{
		Dir: *root, RegistryDir: registry,
	})
	if err != nil {
		return err
	}
	defer ws.Close()
	result := makeWorkspaceResult(ws, imported.Files, imported.Bytes)
	result.CheckpointID = imported.ID
	result.Team = document.Team
	result.RegistryDir = registry
	return writeWorkspaceJSON(result)
}

// adoptedTeam is the partition the adopting machine files the handoff under. The
// flag is how a caller overrides a document written without one, and a document
// that names a team keeps it, because a handoff that arrived with a team is a
// handoff that belongs to one.
func adoptedTeam(documentTeam, flagTeam string) string {
	if flagTeam != "" {
		return flagTeam
	}
	return documentTeam
}

// checkTeamAgreement refuses a flag that contradicts the document. Two teams named
// is two intents, and guessing between them would file somebody's work under a
// partition nobody asked for.
func checkTeamAgreement(documentTeam, flagTeam string) error {
	if documentTeam != "" && flagTeam != "" && documentTeam != flagTeam {
		return fmt.Errorf("handoff names team %q but --team says %q", documentTeam, flagTeam)
	}
	return nil
}

// importHandoffArchive brings the checkpoint the handoff names into this
// machine's registry. The digest is checked first because it is the only check
// available before anything is written, and the import is what verifies every
// file against the manifest inside the archive.
func importHandoffArchive(handoffPath, registryDir string, document workspaceHandoff, options stow.CheckpointArchiveOptions) (stow.CheckpointInfo, error) {
	if document.Archive == nil {
		return stow.CheckpointInfo{}, errors.New("this handoff is a same-machine reference with no archive; export one and hand it off with --archive")
	}
	archivePath, err := resolveHandoffArchive(handoffPath, document.Archive.Path)
	if err != nil {
		return stow.CheckpointInfo{}, err
	}
	if err := verifyHandoffArchive(archivePath, document.Archive); err != nil {
		return stow.CheckpointInfo{}, err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return stow.CheckpointInfo{}, err
	}
	defer archive.Close()
	return stow.ImportCheckpoint(context.Background(), registryDir, archive, options)
}

// checkHandoffIdentity refuses a document whose names disagree with the bytes it
// carries. A handoff pointing at one workspace and holding another is either
// tampered with or assembled wrong, and restoring either would put somebody's
// work in the wrong place.
func checkHandoffIdentity(document workspaceHandoff, imported stow.CheckpointInfo) error {
	if document.CheckpointID != "" && imported.ID != document.CheckpointID {
		return fmt.Errorf("handoff names checkpoint %s but its archive holds %s", document.CheckpointID, imported.ID)
	}
	if document.WorkspaceID != "" && imported.WorkspaceID != document.WorkspaceID {
		return fmt.Errorf("handoff names workspace %s but its archive holds %s", document.WorkspaceID, imported.WorkspaceID)
	}
	return nil
}

// publishHandoffArchive writes the checkpoint archive and returns the reference
// that binds it to the handoff. The digest is of the published file, computed
// after it is in place, because the document has to describe the bytes the
// receiver will actually read.
func publishHandoffArchive(registryDir, checkpointID, archivePath string) (*handoffArchive, error) {
	published, err := publishNewFile(archivePath, "handoff archive", func(file *os.File) error {
		return stow.ExportCheckpoint(context.Background(), registryDir, checkpointID, file, stow.CheckpointArchiveOptions{})
	})
	if err != nil {
		return nil, err
	}
	file, err := os.Open(published)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return nil, fmt.Errorf("digest handoff archive: %w", err)
	}
	manifest, err := stow.LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		return nil, err
	}
	return &handoffArchive{
		Path: published, SHA256: hex.EncodeToString(digest.Sum(nil)),
		Bytes: info.Size(), Files: int64(len(manifest.Files)),
	}, nil
}

// verifyHandoffArchive is the check the receiver can make before it creates
// anything: does the file still hash to what the handoff said it hashes to.
func verifyHandoffArchive(path string, bound *handoffArchive) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read handoff archive: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return fmt.Errorf("digest handoff archive: %w", err)
	}
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != bound.SHA256 {
		return fmt.Errorf("handoff archive %s has digest %s, the handoff names %s", path, actual, bound.SHA256)
	}
	if bound.Bytes > 0 && size != bound.Bytes {
		return fmt.Errorf("handoff archive %s is %d bytes, the handoff names %d", path, size, bound.Bytes)
	}
	return nil
}

// resolveHandoffArchive reads the archive path relative to the handoff document,
// so a handoff and the archive it names stay together when the pair is moved or
// copied. An absolute path in the document is used as written.
func resolveHandoffArchive(handoffPath, archive string) (string, error) {
	if archive == "" {
		return "", errors.New("handoff archive path is empty")
	}
	if filepath.IsAbs(archive) {
		return archive, nil
	}
	base, err := filepath.Abs(filepath.Dir(handoffPath))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, archive), nil
}

// readHandoffDocument parses a handoff reference under the same strictness as a
// task manifest: unknown fields and trailing values are refused, because a
// document that carries a field nobody implements is a document nobody is
// checking.
func readHandoffDocument(path string) (workspaceHandoff, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return workspaceHandoff{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document workspaceHandoff
	if err := decoder.Decode(&document); err != nil {
		return document, fmt.Errorf("decode handoff reference: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return document, errors.New("handoff reference must contain exactly one JSON value")
	}
	if err := validateHandoff(document); err != nil {
		return document, err
	}
	return document, nil
}

func validateHandoff(document workspaceHandoff) error {
	if document.Version != handoffLocalVersion && document.Version != handoffPortableVersion {
		return fmt.Errorf("unsupported handoff reference version %d", document.Version)
	}
	if document.WorkspaceID == "" {
		return errors.New("invalid handoff reference: no workspace id")
	}
	if document.Version == handoffLocalVersion && document.RegistryDir == "" {
		return errors.New("invalid handoff reference: no registry directory")
	}
	if document.Archive != nil && document.Archive.SHA256 == "" {
		return errors.New("invalid handoff reference: the archive has no digest")
	}
	return nil
}

func requireCheckpointInWorkspace(registryDir, checkpointID, workspaceID string) error {
	manifest, err := stow.LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		return err
	}
	if manifest.WorkspaceID != workspaceID {
		return errors.New("checkpoint belongs to a different workspace")
	}
	return nil
}
