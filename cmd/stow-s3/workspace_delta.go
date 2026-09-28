package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// deltaDocumentBytes bounds the read of a transported delta document. The format
// bounds its own content at stow.MaxDeltaBytes; this bounds the file, which is
// that content as base64 plus JSON, so it is the same limit with room for the
// encoding. A delta arrives from another machine, and the file is read before
// anything in it can be checked.
const deltaDocumentBytes = stow.MaxDeltaBytes*2 + (1 << 20)

type deltaResult struct {
	Version    int    `json:"version"`
	BaseID     string `json:"base_id"`
	TargetID   string `json:"target_id"`
	Document   string `json:"document"`
	SHA256     string `json:"sha256"`
	Files      int64  `json:"files"`
	Bytes      int64  `json:"bytes"`
	Checkpoint string `json:"checkpoint_id,omitempty"`
}

// deltaWorkspaceCommand writes the difference between two checkpoints to a file
// the other side can apply.
func deltaWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace delta", flag.ContinueOnError)
	from := flags.String("from", "", "Checkpoint the delta is measured from")
	to := flags.String("to", "", "Checkpoint the delta brings the target to")
	output := flags.String("output", "", "New delta document path")
	chosen := registryFlag(flags)
	maxBytes := flags.Int64("max-bytes", 0, "Delta content byte cap (0 uses the default)")
	maxFiles := flags.Int64("max-files", 0, "Delta file cap (0 uses the default)")
	includeSensitive := flags.Bool("include-sensitive", false, "Include common credential-looking filenames")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" || *output == "" {
		return errors.New("workspace delta requires --from, --to, and --output")
	}
	if *maxBytes < 0 || *maxFiles < 0 {
		return errors.New("workspace delta limits must not be negative")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	delta, err := stow.CreateDelta(context.Background(), registry, *from, *to, stow.DeltaOptions{
		MaxBytes: *maxBytes, MaxFiles: *maxFiles, IncludeSensitive: *includeSensitive,
	})
	if err != nil {
		return err
	}
	encoded, err := stow.EncodeDelta(delta)
	if err != nil {
		return err
	}
	path, err := publishNewFile(*output, "delta document", func(file *os.File) error {
		_, writeErr := file.Write(encoded)
		return writeErr
	})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	return writeWorkspaceJSON(deltaResult{
		Version:  stow.DeltaVersion,
		BaseID:   delta.BaseID,
		TargetID: delta.TargetID,
		Document: path, SHA256: hex.EncodeToString(digest[:]),
		Files: delta.Files, Bytes: delta.Bytes,
	})
}

// applyDeltaCommand brings a base checkpoint to the state a delta describes. The
// base is explicit because the conflict rule is stated against it: a delta says
// what a path was and is now, and a target that is neither cannot be reconciled
// without deciding which writer wins, which is a decision this command refuses to
// make on the caller's behalf.
func applyDeltaCommand(args []string) error {
	flags := flag.NewFlagSet("workspace apply", flag.ContinueOnError)
	path := flags.String("delta", "", "Delta document to apply")
	base := flags.String("base", "", "Checkpoint the delta applies to")
	chosen := registryFlag(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || *base == "" {
		return errors.New("workspace apply requires --delta and --base")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	delta, err := readDeltaDocument(*path)
	if err != nil {
		return err
	}
	info, err := stow.ApplyDelta(context.Background(), registry, *base, delta)
	if err != nil {
		return describeDeltaFailure(err)
	}
	return writeWorkspaceJSON(checkpointResult{
		Version: 1, ID: info.ID, WorkspaceID: info.WorkspaceID,
		ParentID: info.ParentID, Created: info.Created.Format(time.RFC3339Nano),
		Files: info.Files, Bytes: info.Bytes, Excluded: info.Excluded,
	})
}

func readDeltaDocument(path string) (*stow.DeltaDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, deltaDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read delta document: %w", err)
	}
	if int64(len(raw)) > deltaDocumentBytes {
		return nil, fmt.Errorf("%w: file is over the %d byte read limit", stow.ErrDeltaTooLarge, deltaDocumentBytes)
	}
	return stow.DecodeDelta(raw)
}

// describeDeltaFailure names the two refusals an operator can act on and leaves
// everything else alone. A conflict is not a crash: the base is a real point in a
// real history and the caller has to choose, so the message says which two.
func describeDeltaFailure(err error) error {
	switch {
	case errors.Is(err, stow.ErrDeltaConflict):
		return fmt.Errorf("delta refused: the base has diverged from the delta (%w)", err)
	case errors.Is(err, stow.ErrDeltaVersionUnsupported):
		return fmt.Errorf("delta refused: this build does not speak the document's version (%w)", err)
	case errors.Is(err, stow.ErrDeltaTooLarge):
		return fmt.Errorf("delta refused: %w", err)
	default:
		return err
	}
}
