package stow

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestPortableHostEditDropsStaleChecksumAndKeepsDeclaredMetadata(t *testing.T) {
	w, registry := portableFixture(t)
	ctx := context.Background()
	_, err := w.store.PutObject(ctx, "primary", "normal.txt", strings.NewReader("hello world"), storage.PutOptions{ContentType: "application/custom", Metadata: map[string]string{"project": "stow"}, ChecksumAlgorithm: "CRC64NVME", ChecksumValue: "jSnVw/bqjr4="})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(w.Dir(), "normal.txt")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("HELLO WORLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := w.CreateCheckpoint(ctx, CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCheckpoint(registry, checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range manifest.Objects {
		if object.Bucket != "primary" || object.Key != "normal.txt" {
			continue
		}
		if object.ChecksumAlgorithm != "" || object.Metadata["project"] != "stow" || object.ContentType != "application/custom" {
			t.Fatalf("host edit = %+v", object)
		}
		return
	}
	t.Fatal("edited object missing")
}

func TestPortableSensitiveObjectsRequireSenderAndReceiverConsent(t *testing.T) {
	w, registry := portableFixture(t)
	ctx := context.Background()
	checkpoint, err := w.CreateCheckpoint(ctx, CheckpointOptions{PortableObjects: true, IncludeSensitiveFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportCheckpoint(ctx, registry, checkpoint.ID, &archive, CheckpointArchiveOptions{}); err == nil {
		t.Fatal("export accepted sensitive object")
	}
	if err := ExportCheckpoint(ctx, registry, checkpoint.ID, &archive, CheckpointArchiveOptions{IncludeSensitiveFiles: true}); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCheckpointArchive(ctx, bytes.NewReader(archive.Bytes()), CheckpointArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.SensitivePaths) != 1 || preview.SensitivePaths[0] != "s3://secondary/../.env" {
		t.Fatalf("preview=%+v", preview)
	}
	remote := filepath.Join(t.TempDir(), "registry")
	if _, err := ImportCheckpoint(ctx, remote, bytes.NewReader(archive.Bytes()), CheckpointArchiveOptions{}); err == nil {
		t.Fatal("import accepted sensitive object")
	}
	if _, err := ImportCheckpoint(ctx, remote, bytes.NewReader(archive.Bytes()), CheckpointArchiveOptions{IncludeSensitiveFiles: true}); err != nil {
		t.Fatal(err)
	}
}

func TestPortableManifestRefusesInvalidInventory(t *testing.T) {
	w, registry := portableFixture(t)
	checkpoint, err := w.CreateCheckpoint(context.Background(), CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCheckpoint(registry, checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*CheckpointManifest){
		"v1 with objects":    func(m *CheckpointManifest) { m.Version = 1 },
		"missing primary":    func(m *CheckpointManifest) { m.PrimaryBucket = "missing" },
		"duplicate bucket":   func(m *CheckpointManifest) { m.Buckets = append(m.Buckets, m.Buckets[0]) },
		"duplicate object":   func(m *CheckpointManifest) { m.Objects = append(m.Objects, m.Objects[0]) },
		"missing payload":    func(m *CheckpointManifest) { m.Objects[0].Payload = "objects/absent" },
		"payload traversal":  func(m *CheckpointManifest) { m.Payloads[0].Path = "../escape" },
		"wrong payload hash": func(m *CheckpointManifest) { m.Objects[0].SHA256 = strings.Repeat("0", 64) },
		"metadata prefix":    func(m *CheckpointManifest) { m.Objects[0].Metadata = map[string]string{"X-Amz-Meta-Project": "stow"} },
		"private key":        func(m *CheckpointManifest) { m.Objects[0].Key = ".stow/credentials" },
		"composite checksum": func(m *CheckpointManifest) {
			m.Objects[0].ChecksumAlgorithm = "CRC64NVME"
			m.Objects[0].ChecksumValue = "AAAAAAAAAAA="
			m.Objects[0].ChecksumType = "COMPOSITE"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var altered CheckpointManifest
			if err := json.Unmarshal(encoded, &altered); err != nil {
				t.Fatal(err)
			}
			change(&altered)
			if err := validateCheckpointManifest(altered); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}

func TestPortableRestoreRefusesInvalidChecksumBeforeCreatingDestination(t *testing.T) {
	w, registry := portableFixture(t)
	checkpoint, err := w.CreateCheckpoint(context.Background(), CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCheckpoint(registry, checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Objects[0].ChecksumAlgorithm = "CRC64NVME"
	manifest.Objects[0].ChecksumValue = "AAAAAAAAAAA="
	manifest.Objects[0].ChecksumType = "FULL_OBJECT"
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "checkpoints", checkpoint.ID, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := RestoreCheckpoint(registry, checkpoint.ID, WorkspaceOptions{Dir: destination, RegistryDir: registry}); err == nil {
		t.Fatal("invalid checksum accepted")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination created: %v", err)
	}
}
