package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// A delta is the difference between two known points, expressed as a versioned
// document that can bring a third point to the second.
//
// The checkpoint diff already computed added/changed/deleted by comparing two
// captures. That is a *report* — it names what differs and hashes both sides, but
// carries no bytes, so it cannot change anything. Promoting it to a document that
// can be applied is what turns "here is what changed" into "here is the change",
// which is the thing that makes exchanging a working set between an agent and a
// device cost what the difference costs rather than what the tree costs.
//
// The conflict rule is the same one run-through propagation uses, deliberately. A
// delta says "this file was A and is now B", so applying it to a target whose copy
// of that file is neither A nor B is applying one writer's intent to a state that
// does not exist. That is a precondition failure, refused rather than merged, and
// it must not acquire a second vocabulary: the mechanism is identical and so is
// the refusal.

// DeltaVersion is the delta document's format version. A receiver refuses a
// version it does not speak rather than guessing at the fields.
const DeltaVersion = 1

// changeSide is what the two ends held for one path, so the comparison can be
// passed as one value rather than as four parameters. It is not a public type.
type changeSide struct {
	old        CheckpointFile
	hadOld     bool
	current    CheckpointFile
	hasCurrent bool
}

// Change kinds, matching what the checkpoint diff already emits. They are strings
// on the wire because a checkpoint manifest is the same shape, and a delta that
// disagreed with the vocabulary it came from would be a second dialect.
const (
	DeltaChangeAdded   = "added"
	DeltaChangeChanged = "changed"
	DeltaChangeDeleted = "deleted"
)

// Default bounds for a delta, the same as the archive path so a delta and an
// archive of the same work are accepted or refused together.
const (
	defaultDeltaBytes = 1 << 30
	defaultDeltaFiles = 100_000
)

// MaxDeltaBytes is the largest content a delta document may declare, on the way
// out and on the way in. It is exported because a caller reading a delta from
// somewhere it does not control has to bound the read itself, and a bound it
// cannot name is a bound it has to guess.
const MaxDeltaBytes int64 = 16 << 20

// ErrDeltaConflict reports that a delta could not be applied because the target
// is not where the delta was written to be applied.
//
// It is a refusal, not a merge, and it is deliberately the same shape as
// runthrough.ErrUpstreamConflict: a precondition that cannot succeed on a retry,
// so the caller has to decide what should win.
var ErrDeltaConflict = errors.New("delta target has diverged from the delta's base")

// ErrDeltaVersionUnsupported reports a delta whose format version this build does
// not speak. It is separate from a conflict because retrying will not help and
// because a version mismatch is a deployment problem rather than a data one.
var ErrDeltaVersionUnsupported = errors.New("delta document version is not supported")

// ErrDeltaTooLarge reports a delta document whose content is over MaxDeltaBytes.
//
// It is separate from a conflict because nothing is wrong with the target, and
// separate from a version refusal because the document is readable: it is simply
// bigger than this format admits.
var ErrDeltaTooLarge = errors.New("delta document is larger than the format allows")

// ErrDeltaDigestMismatch reports that a delta document does not hash to the
// digest the sender published for it.
//
// The per-file digests inside a document cover content bytes only. Nothing in the
// document itself binds the change list — the paths, the kinds, and the from/to
// metadata — to the sender's intent, so a document altered in transit can carry a
// valid content digest for a path the sender never named. Measured: renaming an
// added file from "notes.txt" to "planted.sh", in the change and in the content
// map together, produced a document that applied cleanly.
//
// This is the check the handoff archive path already had and the delta path did
// not: `delta` reports the document's digest, and the receiver compares against it
// before anything is written. It is separate from a conflict because the document
// is self-consistent and the target may well be fine — the disagreement is between
// the document and the sender, and retrying cannot resolve it.
var ErrDeltaDigestMismatch = errors.New("delta document does not match the digest the sender published")

// DeltaOptions bounds the work a delta may describe. Zero takes the defaults,
// matching CheckpointArchiveOptions: a cap of zero is not "unlimited", because an
// unbounded document is exactly what the archive path refuses to produce.
type DeltaOptions struct {
	MaxBytes int64
	MaxFiles int64
	// IncludeSensitive carries paths the sensitive-name guard would otherwise
	// refuse. It is the same opt-in the archive path requires, because a delta can
	// carry the same bytes an archive can.
	IncludeSensitive bool
	// Encoded is the document exactly as it travelled, and ExpectSHA256 the digest
	// the sender published for it. When both are set, the document is refused
	// unless the bytes hash to that digest, before any precondition is checked and
	// before anything is staged.
	//
	// A *DeltaDocument has already been parsed, so the bytes that produced it are
	// not recoverable: re-encoding a struct would hash a document this build
	// happened to produce rather than the one that arrived, and a digest over that
	// would pass for any alteration that survives a round trip. That is why the
	// bytes are carried alongside rather than recomputed. VerifyDeltaDigest is the
	// same check for a caller that wants it on its own.
	//
	// It is optional because a receiver with no trusted copy of the digest cannot
	// invent one, and demanding it would make every apply impossible rather than
	// safe. A receiver that *can* compare should: the digest is the only thing that
	// covers the change list rather than the content.
	Encoded      []byte
	ExpectSHA256 string
}

// DeltaDocument is the transferable form. It names both ends, so a receiver can
// check it is being applied where it thinks it is, and carries the content for
// every added or changed path.
//
// Content is on the wire as base64 values under its own name. Excluding it
// leaves every delta describable and none applicable: the document a caller wrote
// to a file holds no bytes, and applying it fails on the first changed path. A
// delta's reason to exist is that it can be carried somewhere.
type DeltaDocument struct {
	Version          int               `json:"version"`
	BaseID           string            `json:"base_id"`
	TargetID         string            `json:"target_id"`
	Created          time.Time         `json:"created"`
	Changes          []DeltaChange     `json:"changes"`
	Files            int64             `json:"files"`
	Bytes            int64             `json:"bytes"`
	Content          map[string][]byte `json:"content,omitempty"`
	IncludeSensitive bool              `json:"include_sensitive,omitempty"`
}

// DeltaChange is one path's difference. From is what the base held and To is what
// the target holds, as digests, so the receiver can assert the precondition before
// touching anything.
type DeltaChange struct {
	Path string          `json:"path"`
	Kind string          `json:"kind"`
	From *CheckpointFile `json:"from,omitempty"`
	To   *CheckpointFile `json:"to,omitempty"`
}

// CreateDelta describes the difference between two checkpoints in one registry.
//
// Both checkpoint IDs are required and must belong to the same workspace.
func CreateDelta(ctx context.Context, registryDir, baseID, targetID string, options DeltaOptions) (*DeltaDocument, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if baseID == "" {
		return nil, errors.New("stow: a delta needs a base checkpoint")
	}
	if targetID == "" {
		return nil, errors.New("stow: a delta needs a target checkpoint")
	}
	if err := validateDeltaOptions(options); err != nil {
		return nil, err
	}

	base, err := LoadCheckpoint(registryDir, baseID)
	if err != nil {
		return nil, err
	}
	target, err := LoadCheckpoint(registryDir, targetID)
	if err != nil {
		return nil, err
	}
	if base.Version != checkpointVersion || target.Version != checkpointVersion {
		return nil, fmt.Errorf("stow: delta transport does not support portable object checkpoints")
	}
	if base.WorkspaceID != target.WorkspaceID {
		return nil, fmt.Errorf("stow: delta crosses workspaces: %q and %q", base.WorkspaceID, target.WorkspaceID)
	}

	targetDir, err := checkpointDirectory(registryDir, targetID)
	if err != nil {
		return nil, err
	}

	lock, err := workspace.AcquireCapture(filepath.Dir(filepath.Dir(targetDir)), base.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	base, err = LoadCheckpoint(registryDir, baseID)
	if err != nil {
		return nil, err
	}
	target, err = LoadCheckpoint(registryDir, targetID)
	if err != nil {
		return nil, err
	}

	delta := &DeltaDocument{
		Version:          DeltaVersion,
		BaseID:           baseID,
		TargetID:         targetID,
		Created:          time.Now().UTC(),
		Content:          map[string][]byte{},
		IncludeSensitive: options.IncludeSensitive,
	}
	if err := recordDeltaChanges(ctx, delta, deltaBuild{targetDir: targetDir, base: base, target: target, options: options}); err != nil {
		return nil, err
	}
	delta.Files = int64(len(delta.Changes))
	return delta, nil
}

type deltaBuild struct {
	targetDir    string
	base, target CheckpointManifest
	options      DeltaOptions
}

func recordDeltaChanges(ctx context.Context, delta *DeltaDocument, build deltaBuild) error {
	baseFiles, targetFiles := indexCheckpointFiles(build.base), indexCheckpointFiles(build.target)
	paths := make(map[string]struct{}, len(baseFiles)+len(targetFiles))
	for path := range baseFiles {
		paths[path] = struct{}{}
	}
	for path := range targetFiles {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := delta.recordChange(build.targetDir, path, changeSide{old: baseFiles[path], hadOld: hasCheckpointFile(baseFiles, path), current: targetFiles[path], hasCurrent: hasCheckpointFile(targetFiles, path)})
		if err != nil {
			return err
		}
		if err := delta.enforceBounds(build.options); err != nil {
			return err
		}
	}
	return delta.enforceBounds(build.options)
}

func hasCheckpointFile(files map[string]CheckpointFile, path string) bool {
	_, ok := files[path]
	return ok
}

// recordChange decides what happened to one path and, when the change carries
// bytes, attaches them. Split out of CreateDelta so the comparison reads as the
// three-way thing it is rather than as a switch buried in a loop.
func (d *DeltaDocument) recordChange(targetDir, path string, side changeSide) error {
	old, hadOld, current, hasCurrent := side.old, side.hadOld, side.current, side.hasCurrent
	switch {
	case !hadOld:
		entry := current
		d.Changes = append(d.Changes, DeltaChange{Path: path, Kind: DeltaChangeAdded, To: &entry})
		return d.attach(targetDir, path, &entry)
	case !hasCurrent:
		entry := old
		d.Changes = append(d.Changes, DeltaChange{Path: path, Kind: DeltaChangeDeleted, From: &entry})
		return nil
	case old.SHA256 != current.SHA256 || old.Mode != current.Mode:
		oldEntry, newEntry := old, current
		d.Changes = append(d.Changes, DeltaChange{Path: path, Kind: DeltaChangeChanged, From: &oldEntry, To: &newEntry})
		return d.attach(targetDir, path, &newEntry)
	}
	return nil
}

// attach reads one path's bytes out of the target checkpoint, verifying the
// digest the manifest claims. A delta whose content does not match its own
// description would apply a different change than the one it names.
func (d *DeltaDocument) attach(checkpointDir, path string, file *CheckpointFile) error {
	if sensitiveSeedPath(path) && !d.IncludeSensitive {
		return fmt.Errorf("stow: delta includes sensitive-looking path %q (explicit opt-in required)", path)
	}
	if file.Size > MaxDeltaBytes-d.Bytes {
		return fmt.Errorf("stow: delta exceeds content limit")
	}
	data, err := readVerifiedCheckpointFile(checkpointDir, *file)
	if err != nil {
		return err
	}
	d.Content[path] = data
	return nil
}

// enforceBounds refuses a delta the receiver could not accept, rather than
// producing one and letting it fail on the other side.
func (d *DeltaDocument) enforceBounds(options DeltaOptions) error {
	maxBytes, maxFiles := options.MaxBytes, options.MaxFiles
	if maxBytes == 0 {
		maxBytes = defaultDeltaBytes
	}
	if maxFiles == 0 {
		maxFiles = defaultDeltaFiles
	}
	if int64(len(d.Changes)) > maxFiles {
		return fmt.Errorf("stow: delta describes %d files, over the cap of %d", len(d.Changes), maxFiles)
	}
	d.Bytes = 0
	for _, data := range d.Content {
		d.Bytes += int64(len(data))
	}
	if d.Bytes > maxBytes {
		return fmt.Errorf("stow: delta carries %d bytes, over the cap of %d", d.Bytes, maxBytes)
	}
	if d.Bytes > MaxDeltaBytes {
		return fmt.Errorf("stow: delta carries %d bytes, over the %d a document may declare", d.Bytes, MaxDeltaBytes)
	}
	return nil
}

func validateDeltaOptions(options DeltaOptions) error {
	if options.MaxBytes < 0 {
		return errors.New("stow: delta MaxBytes must not be negative")
	}
	if options.MaxFiles < 0 {
		return errors.New("stow: delta MaxFiles must not be negative")
	}
	return nil
}

func indexCheckpointFiles(manifest CheckpointManifest) map[string]CheckpointFile {
	out := make(map[string]CheckpointFile, len(manifest.Files))
	for _, file := range manifest.Files {
		out[file.Path] = file
	}
	return out
}

// EncodeDelta serializes a delta, including its content, for transport.
func EncodeDelta(delta *DeltaDocument) ([]byte, error) {
	if err := validateDeltaDocument(delta, delta != nil && delta.IncludeSensitive); err != nil {
		return nil, err
	}
	return json.Marshal(delta)
}

// DeltaDocumentDigest is the digest a sender publishes for a document and a
// receiver checks it against. It is the digest of the encoded bytes, so it covers
// the whole document: the change list, the paths, the kinds, the from/to metadata
// and the content together.
func DeltaDocumentDigest(encoded []byte) string {
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// VerifyDeltaDigest refuses a document that does not hash to what the sender said
// it hashes to.
//
// This is the same check the handoff archive path has had all along, applied to
// the delta path where it was missing. Without it a document altered in transit
// can keep a valid per-file content digest while naming a different destination
// than the sender chose, because the content digest covers bytes and not the name
// those bytes are filed under.
//
// An empty expected digest is not a check, so it returns nil rather than refusing:
// a receiver with no trusted copy of the digest has nothing to compare against, and
// failing closed there would make the option mandatory in every call and honest in
// none.
func VerifyDeltaDigest(encoded []byte, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	actual := DeltaDocumentDigest(encoded)
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("%w: the document hashes to %s, the sender published %s",
			ErrDeltaDigestMismatch, actual, strings.TrimSpace(expected))
	}
	return nil
}

// DecodeDelta parses a transported delta.
//
// The bound is applied here rather than at the point of application because this
// is the only place a document from outside this process exists, and a decoder
// that accepts an unbounded document has already paid for the memory by the time
// anyone gets to refuse it.
func DecodeDelta(raw []byte) (*DeltaDocument, error) {
	if len(raw) > 32<<20 {
		return nil, errors.New("stow: encoded delta exceeds limit")
	}
	var delta DeltaDocument
	if err := json.Unmarshal(raw, &delta); err != nil {
		return nil, fmt.Errorf("stow: parse delta: %w", err)
	}
	if err := validateDeltaDocument(&delta, delta.IncludeSensitive); err != nil {
		return nil, err
	}
	if err := checkDeltaSize(delta); err != nil {
		return nil, err
	}
	return &delta, nil
}

// checkDeltaSize refuses a document over the format's limit, counting the content
// it actually carries rather than the Bytes field it declares. A declaration is a
// claim; the map is the evidence.
func checkDeltaSize(delta DeltaDocument) error {
	if delta.Bytes > MaxDeltaBytes {
		return fmt.Errorf("%w: declares %d bytes, over %d", ErrDeltaTooLarge, delta.Bytes, MaxDeltaBytes)
	}
	var total int64
	for _, data := range delta.Content {
		total += int64(len(data))
	}
	if total > MaxDeltaBytes {
		return fmt.Errorf("%w: carries %d bytes, over %d", ErrDeltaTooLarge, total, MaxDeltaBytes)
	}
	return nil
}
