package stow

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

const portableCheckpointVersion = 2

// CheckpointObject describes logical object state, independent of backend paths.
type CheckpointObject struct {
	Bucket            string            `json:"bucket"`
	Key               string            `json:"key"`
	Payload           string            `json:"payload"`
	Size              int64             `json:"size"`
	SHA256            string            `json:"sha256"`
	ContentType       string            `json:"content_type"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ChecksumAlgorithm string            `json:"checksum_algorithm,omitempty"`
	ChecksumValue     string            `json:"checksum_value,omitempty"`
	ChecksumType      string            `json:"checksum_type,omitempty"`
}

type portableCapture struct {
	provenance *CheckpointProvenance
	options    CheckpointOptions
	snapshot   workspace.LogicalSnapshot
	objects    []CheckpointObject
	payloads   []CheckpointFile
	sources    map[string]string
	excluded   []string
}

func scanPortableObjects(ctx context.Context, target captureTarget, options CheckpointOptions, files []CheckpointFile) (portableCapture, error) {
	capture := portableCapture{sources: map[string]string{}}
	snapshot, err := workspace.ReadLogicalSnapshot(ctx, target.dir, workspace.SnapshotOptions{
		MaxBytes: archiveByteLimit(options.MaxBytes), MaxObjects: archiveFileLimit(options.MaxFiles),
		Select: func(bucket, key string) bool {
			if excludedPortableKey(key, options.IncludeSensitiveFiles) {
				capture.excluded = append(capture.excluded, "s3://"+bucket+"/"+key)
				return false
			}
			return true
		},
	})
	if err != nil {
		return capture, err
	}
	if snapshot.WorkspaceID != target.workspaceID {
		return capture, fmt.Errorf("stow: workspace identity changed during capture")
	}
	capture.snapshot = snapshot
	fileIndex := map[string]CheckpointFile{}
	for _, file := range files {
		fileIndex[file.Path] = file
	}
	for _, source := range snapshot.Objects {
		payload := "objects/" + workspace.Digest(source.Bucket+"\x00"+source.Key)
		if file, ok := fileIndex[source.Key]; ok && source.Bucket == snapshot.PrimaryBucket && source.Source == filepath.Join(target.dir, filepath.FromSlash(source.Key)) {
			if file.SHA256 != source.SHA256 || file.Size != source.Size {
				return capture, fmt.Errorf("stow: file and object changed during capture")
			}
			payload = "files/" + source.Key
		} else {
			capture.payloads = append(capture.payloads, CheckpointFile{Path: strings.TrimPrefix(payload, "objects/"), Size: source.Size, SHA256: source.SHA256, Mode: 0o600})
			capture.sources[payload] = source.Source
		}
		object := CheckpointObject{Bucket: source.Bucket, Key: source.Key, Payload: payload, Size: source.Size, SHA256: source.SHA256, ContentType: source.ContentType, Metadata: source.Metadata, ChecksumAlgorithm: source.ChecksumAlgorithm, ChecksumValue: source.ChecksumValue}
		if object.ChecksumAlgorithm != "" {
			object.ChecksumType = "FULL_OBJECT"
		}
		capture.objects = append(capture.objects, object)
	}
	sort.Strings(capture.excluded)
	return capture, nil
}

func excludedPortableKey(key string, includeSensitive bool) bool {
	normalized := strings.ReplaceAll(key, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if strings.EqualFold(part, ".git") || strings.EqualFold(part, ".stow") {
			return true
		}
	}
	return !includeSensitive && sensitiveSeedPath(normalized)
}

func supportedCheckpointVersion(version int) bool {
	return version == checkpointVersion || version == portableCheckpointVersion
}

func checkpointPayloadFiles(manifest CheckpointManifest) map[string]CheckpointFile {
	files := make(map[string]CheckpointFile, len(manifest.Files)+len(manifest.Payloads))
	for _, file := range manifest.Files {
		files["files/"+file.Path] = file
	}
	for _, file := range manifest.Payloads {
		files["objects/"+file.Path] = file
	}
	return files
}

func validatePortableManifest(manifest CheckpointManifest) error {
	if manifest.Version == checkpointVersion {
		return validateFileOnlyManifest(manifest)
	}
	if err := validateCheckpointProvenance(manifest.Provenance); err != nil {
		return err
	}
	if manifest.WorkingDirectory != "." && !validCheckpointPath(manifest.WorkingDirectory) {
		return fmt.Errorf("stow: invalid checkpoint working directory")
	}
	buckets, err := validatePortableBuckets(manifest)
	if err != nil {
		return err
	}
	if err := validatePortablePayloads(manifest.Payloads); err != nil {
		return err
	}
	return validatePortableObjects(manifest, buckets)
}

func validateFileOnlyManifest(manifest CheckpointManifest) error {
	if manifest.PrimaryBucket != "" || len(manifest.Buckets) > 0 || len(manifest.Objects) > 0 || len(manifest.Payloads) > 0 || manifest.WorkingDirectory != "" || manifest.Provenance != nil {
		return fmt.Errorf("stow: file checkpoint cannot contain portable object fields")
	}
	return nil
}

func validatePortableBuckets(manifest CheckpointManifest) (map[string]bool, error) {
	if len(manifest.Objects) > defaultCheckpointArchiveFiles || len(manifest.Buckets) > defaultCheckpointArchiveFiles {
		return nil, fmt.Errorf("stow: checkpoint inventory exceeds format bounds")
	}
	buckets := map[string]bool{}
	for _, bucket := range manifest.Buckets {
		if !storage.ValidBucketName(bucket) || buckets[bucket] {
			return nil, fmt.Errorf("stow: invalid or duplicate checkpoint bucket")
		}
		buckets[bucket] = true
	}
	if !buckets[manifest.PrimaryBucket] {
		return nil, fmt.Errorf("stow: checkpoint primary bucket is missing")
	}
	return buckets, nil
}

func validatePortablePayloads(payloads []CheckpointFile) error {
	seen := map[string]bool{}
	for _, payload := range payloads {
		if len(payload.Path) != 64 || seen[payload.Path] || !validCheckpointFileMetadata(payload) || payload.Mode != 0o600 {
			return fmt.Errorf("stow: invalid checkpoint object payload")
		}
		if _, err := hex.DecodeString(payload.Path); err != nil {
			return err
		}
		seen[payload.Path] = true
	}
	return nil
}

func validatePortableObjects(manifest CheckpointManifest, buckets map[string]bool) error {
	expected := checkpointPayloadFiles(manifest)
	referenced, objects := map[string]bool{}, map[string]bool{}
	for _, object := range manifest.Objects {
		identity := object.Bucket + "\x00" + object.Key
		if !buckets[object.Bucket] || storage.ValidateKey(object.Key) != nil || objects[identity] || excludedPortableKey(object.Key, true) {
			return fmt.Errorf("stow: invalid or duplicate checkpoint object")
		}
		objects[identity] = true
		if err := validateObjectReference(object, expected, manifest.PrimaryBucket); err != nil {
			return err
		}
		if err := validatePortableObjectMetadata(object); err != nil {
			return err
		}
		referenced[object.Payload] = true
	}
	for _, payload := range manifest.Payloads {
		if !referenced["objects/"+payload.Path] {
			return fmt.Errorf("stow: unreferenced checkpoint payload")
		}
	}
	return nil
}

func validateObjectReference(object CheckpointObject, expected map[string]CheckpointFile, primary string) error {
	payload, ok := expected[object.Payload]
	if !ok || payload.Size != object.Size || payload.SHA256 != object.SHA256 {
		return fmt.Errorf("stow: object has invalid payload reference")
	}
	if strings.HasPrefix(object.Payload, "files/") && (object.Bucket != primary || object.Payload != "files/"+object.Key) {
		return fmt.Errorf("stow: object has conflicting file reference")
	}
	return nil
}

func validatePortableObjectMetadata(object CheckpointObject) error {
	metadata, err := storage.NormalizeUserMetadata(object.Metadata)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(metadata, object.Metadata) && !(len(metadata) == 0 && len(object.Metadata) == 0) {
		return fmt.Errorf("stow: portable metadata must use lowercase bare keys")
	}
	size := 0
	for key, value := range metadata {
		size += len(key) + len(value)
	}
	if size > 2048 || len(object.ContentType) > 1024 || strings.ContainsAny(object.ContentType, "\r\n") {
		return fmt.Errorf("stow: checkpoint object metadata exceeds bounds")
	}
	if object.ChecksumAlgorithm == "" && object.ChecksumValue == "" && object.ChecksumType == "" {
		return nil
	}
	if object.ChecksumType != "FULL_OBJECT" || object.ChecksumValue == "" {
		return fmt.Errorf("stow: unsupported checkpoint checksum type")
	}
	if _, err := storage.ComputeChecksum(object.ChecksumAlgorithm, nil); err != nil {
		return err
	}
	return nil
}

func portableWorkingDirectory(target captureTarget) (string, error) {
	reference, err := LookupWorkspace(target.registryDir, target.workspaceID)
	if err != nil {
		return "", err
	}
	directory := reference.WorkingDirectory
	if directory == "" {
		return ".", nil
	}
	relative, err := filepath.Rel(target.dir, directory)
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if relative != "." && !validCheckpointPath(relative) {
		return "", fmt.Errorf("stow: working directory is outside portable workspace")
	}
	return relative, nil
}

func restorePortableObjects(w *Workspace, dir string, manifest CheckpointManifest) error {
	ctx := context.Background()
	for _, bucket := range manifest.Buckets {
		if err := w.store.CreateBucket(ctx, bucket); err != nil {
			return err
		}
	}
	for _, object := range manifest.Objects {
		input, err := os.Open(filepath.Join(dir, filepath.FromSlash(object.Payload)))
		if err != nil {
			return err
		}
		_, err = w.store.PutObject(ctx, object.Bucket, object.Key, input, storage.PutOptions{ContentType: object.ContentType, Metadata: object.Metadata, ChecksumAlgorithm: object.ChecksumAlgorithm, ChecksumValue: object.ChecksumValue})
		input.Close()
		if err != nil {
			return err
		}
	}
	for _, file := range manifest.Files {
		if err := os.Chmod(filepath.Join(w.Dir(), filepath.FromSlash(file.Path)), os.FileMode(file.Mode)&0o777); err != nil {
			return err
		}
	}
	directory, err := prepareWorkingDirectory(w.Dir(), manifest.WorkingDirectory)
	if err != nil {
		return err
	}
	w.workingDirectory = directory
	if err := persistPreparedWorkingDirectory(w); err != nil {
		return err
	}
	if err := persistCheckpointProvenance(w.Dir(), &CheckpointProvenance{Repositories: checkpointRepositories(manifest.Provenance), OriginCheckpointID: manifest.ID}); err != nil {
		return err
	}
	return w.RefreshUsage(ctx)
}

func checkpointRepositories(provenance *CheckpointProvenance) []CheckpointRepository {
	if provenance == nil {
		return nil
	}
	return provenance.Repositories
}
