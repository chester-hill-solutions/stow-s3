package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func encodeWorkspaceFileSaveIdentity(identity workspaceFileSaveIdentity) ([]byte, error) {
	identity.Integrity = workspaceFileSaveIdentityIntegrity(identity)
	return json.Marshal(identity)
}

func decodeWorkspaceFileSaveIdentity(data []byte) (workspaceFileSaveIdentity, error) {
	var identity workspaceFileSaveIdentity
	if len(data) > workspaceFileSaveRecordLimit || !utf8.Valid(data) || decodeWorkspaceFileSaveJSON(data, &identity) != nil ||
		identity.Version != 1 || !ValidWorkspaceID(identity.WorkspaceID) || storage.ValidateBucketName(identity.Bucket) != nil ||
		!workspaceFileSaveDigest(identity.JournalID, 32) || identity.RootDevice == 0 || identity.RootInode == 0 ||
		!workspaceFileSaveDigest(identity.Integrity, 64) || identity.Integrity != workspaceFileSaveIdentityIntegrity(identity) {
		return workspaceFileSaveIdentity{}, storage.ErrSaveRequestUnknown
	}
	return identity, nil
}

func workspaceFileSaveIdentityIntegrity(identity workspaceFileSaveIdentity) string {
	identity.Integrity = ""
	data, _ := json.Marshal(identity)
	digest := sha256.Sum256(append([]byte("stow-workspace-file-save-identity-v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}

func encodeWorkspaceFileSaveRecord(record workspaceFileSaveRecord) ([]byte, error) {
	if err := validateWorkspaceFileSaveRecord(record, false); err != nil {
		return nil, err
	}
	record.Integrity = workspaceFileSaveRecordIntegrity(record)
	data, err := json.Marshal(record)
	if err != nil || len(data) > workspaceFileSaveRecordLimit {
		return nil, storage.ErrSaveRequestFull
	}
	return data, nil
}

func decodeWorkspaceFileSaveRecord(data []byte) (workspaceFileSaveRecord, error) {
	var record workspaceFileSaveRecord
	if len(data) > workspaceFileSaveRecordLimit || !utf8.Valid(data) || decodeWorkspaceFileSaveJSON(data, &record) != nil ||
		!workspaceFileSaveDigest(record.Integrity, 64) || record.Integrity != workspaceFileSaveRecordIntegrity(record) {
		return workspaceFileSaveRecord{}, storage.ErrSaveRequestUnknown
	}
	if err := validateWorkspaceFileSaveRecord(record, false); err != nil {
		return workspaceFileSaveRecord{}, storage.ErrSaveRequestUnknown
	}
	return record, nil
}

func workspaceFileSaveRecordIntegrity(record workspaceFileSaveRecord) string {
	record.Integrity = ""
	data, _ := json.Marshal(record)
	digest := sha256.Sum256(append([]byte("stow-workspace-file-save-record-v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}

func decodeWorkspaceFileSaveJSON[T workspaceFileSaveIdentity | workspaceFileSaveRecord](data []byte, target *T) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}

func validateWorkspaceFileSaveRecord(record workspaceFileSaveRecord, preparedOnly bool) error {
	if preparedOnly && record.Phase != workspaceFileSavePrepared {
		return storage.ErrInvalidSaveRequest
	}
	if err := validateWorkspaceFileSaveRecordIdentity(record); err != nil {
		return err
	}
	if err := validateWorkspaceFileSaveRecordMeaning(record); err != nil {
		return err
	}
	if err := validateWorkspaceFileSaveRecordBasis(record); err != nil {
		return err
	}
	if err := validateWorkspaceFileSaveRecordPlan(record); err != nil {
		return err
	}
	return validateWorkspaceFileSaveRecordPhase(record)
}

func validateWorkspaceFileSaveRecordIdentity(record workspaceFileSaveRecord) error {
	if record.Version != 1 || !ValidWorkspaceID(record.WorkspaceID) || storage.ValidateBucketName(record.Bucket) != nil {
		return storage.ErrInvalidSaveRequest
	}
	if !workspaceFileSaveDigest(record.JournalID, 32) || !validWorkspaceFileSaveRequestKey(record.RequestKey) || storage.ValidateKey(record.Path) != nil || !NewLayout().IsNatural(record.Path) {
		return storage.ErrInvalidSaveRequest
	}
	if !validWorkspaceFileSaveEntryText(record.PreEntry) || !validWorkspaceFileSaveEntryText(&record.PlannedEntry) {
		return storage.ErrInvalidSaveRequest
	}
	return nil
}

func validWorkspaceFileSaveEntryText(entry *ManifestEntry) bool {
	if entry == nil {
		return true
	}
	for _, value := range []string{entry.Digest, entry.ETag, entry.ContentETag, entry.VersionID, entry.ContentType, entry.ChecksumAlgorithm, entry.ChecksumValue} {
		if !utf8.ValidString(value) {
			return false
		}
	}
	for key, value := range entry.Metadata {
		if !utf8.ValidString(key) || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func validateWorkspaceFileSaveRecordMeaning(record workspaceFileSaveRecord) error {
	if !workspaceFileSaveDigest(record.Meaning, 64) || !workspaceFileSaveDigest(record.PayloadDigest, 64) || record.PayloadSize < 0 || record.PayloadSize > 64<<10 {
		return storage.ErrInvalidSaveRequest
	}
	if record.Mode&^uint32(0o777) != 0 {
		return storage.ErrInvalidSaveRequest
	}
	return nil
}

func validateWorkspaceFileSaveRecordBasis(record workspaceFileSaveRecord) error {
	if record.Replace == (record.BasisTokenDigest != "") {
		return storage.ErrInvalidSaveRequest
	}
	if record.BasisTokenDigest != "" && !workspaceFileSaveDigest(record.BasisTokenDigest, 64) {
		return storage.ErrInvalidSaveRequest
	}
	if record.PreEntryIndexed != (record.PreEntry != nil) || record.PreEntry != nil && record.PreEntry.Form != FormNatural {
		return storage.ErrInvalidSaveRequest
	}
	return nil
}

func validateWorkspaceFileSaveRecordPlan(record workspaceFileSaveRecord) error {
	if record.PlannedEntry.Form != FormNatural || !workspaceFileSaveDigest(record.PlannedEntry.VersionID, 32) || record.PlannedEntry.Multipart || record.PlannedEntry.Digest != "" || record.PlannedEntry.ContentETag != "" || !validWorkspaceFileSaveETag(record.PlannedEntry.ETag) {
		return storage.ErrInvalidSaveRequest
	}
	if record.PlannedEntry.Size != record.PayloadSize || record.PlannedEntry.ETag == "" {
		return storage.ErrInvalidSaveRequest
	}
	return nil
}

func validWorkspaceFileSaveETag(value string) bool {
	return len(value) == 34 && value[0] == '"' && value[len(value)-1] == '"' && isLowerHex(value[1:len(value)-1])
}

func validateWorkspaceFileSaveRecordPhase(record workspaceFileSaveRecord) error {
	switch record.Phase {
	case workspaceFileSavePrepared, workspaceFileSavePublishing:
		return validateWorkspaceFileSaveUnpublished(record)
	case workspaceFileSavePublished:
		return validateWorkspaceFileSavePublishedPhase(record)
	case workspaceFileSaveCommitted:
		return validateWorkspaceFileSaveCommitted(record)
	case workspaceFileSaveNotCommitted:
		return validateWorkspaceFileSaveUnpublished(record)
	default:
		return storage.ErrSaveRequestUnknown
	}
}

func validateWorkspaceFileSaveUnpublished(record workspaceFileSaveRecord) error {
	if record.Published != nil || record.Meta != nil || !record.PlannedEntry.Modified.IsZero() {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}

func validateWorkspaceFileSavePublishedPhase(record workspaceFileSaveRecord) error {
	if record.Meta != nil {
		return storage.ErrSaveRequestUnknown
	}
	return validateWorkspaceFileSavePublished(record)
}

func validateWorkspaceFileSaveCommitted(record workspaceFileSaveRecord) error {
	if !workspaceFileSaveMetaMatches(record) {
		return storage.ErrSaveRequestUnknown
	}
	return validateWorkspaceFileSavePublished(record)
}

func validateWorkspaceFileSavePublished(record workspaceFileSaveRecord) error {
	proof := record.Published
	if proof == nil || proof.RootDevice == 0 || proof.RootInode == 0 || proof.ParentDevice == 0 || proof.ParentInode == 0 || proof.FileDevice == 0 || proof.FileInode == 0 ||
		!workspaceFileSaveDigest(proof.Digest, 64) || proof.Digest != record.PayloadDigest || proof.Size != record.PayloadSize || proof.Mode != record.Mode {
		return storage.ErrSaveRequestUnknown
	}
	modified := time.Unix(0, proof.ModifiedNS).UTC()
	if !record.PlannedEntry.Modified.Equal(modified.Truncate(time.Second)) {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}

func workspaceFileSaveMetaMatches(record workspaceFileSaveRecord) bool {
	meta := record.Meta
	entry := record.PlannedEntry
	return meta != nil && meta.Bucket == record.Bucket && meta.Key == record.Path && meta.VersionID == entry.VersionID && meta.Size == entry.Size &&
		meta.ETag == entry.ETag && meta.ContentType == entry.ContentType && meta.ChecksumAlgorithm == entry.ChecksumAlgorithm && meta.ChecksumValue == entry.ChecksumValue &&
		meta.LastModified.Equal(time.Unix(0, record.Published.ModifiedNS).UTC()) && equalWorkspaceFileSaveMetadata(meta.Metadata, entry.Metadata)
}

func equalWorkspaceFileSaveMetadata(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		other, exists := right[key]
		if !exists || other != value {
			return false
		}
	}
	return true
}

func workspaceFileSaveDigest(value string, size int) bool {
	if len(value) != size {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func workspaceFileSaveReservation(record workspaceFileSaveRecord) (int, error) {
	record.Phase, record.Published, record.Meta = workspaceFileSavePrepared, nil, nil
	record.PlannedEntry.Modified = time.Time{}
	if err := validateWorkspaceFileSaveRecord(record, true); err != nil {
		return 0, err
	}
	maximum := 0
	for _, phase := range []string{workspaceFileSavePrepared, workspaceFileSavePublishing, workspaceFileSavePublished, workspaceFileSaveCommitted, workspaceFileSaveNotCommitted} {
		variant := maximumWorkspaceFileSaveRecord(record, phase)
		data, err := encodeWorkspaceFileSaveRecord(variant)
		if err != nil {
			return 0, err
		}
		maximum = max(maximum, len(data))
	}
	if maximum > workspaceFileSaveRecordLimit || maximum > math.MaxInt/2 {
		return 0, storage.ErrSaveRequestFull
	}
	return maximum * 2, nil
}

func maximumWorkspaceFileSaveRecord(record workspaceFileSaveRecord, phase string) workspaceFileSaveRecord {
	record.Phase = phase
	record.Published, record.Meta = nil, nil
	record.PlannedEntry.Modified = time.Time{}
	if phase == workspaceFileSavePublished || phase == workspaceFileSaveCommitted {
		modified := time.Unix(0, math.MinInt64).UTC()
		record.PlannedEntry.Modified = modified.Truncate(time.Second)
		record.Published = &workspaceFileSaveProof{RootDevice: math.MaxUint64, RootInode: math.MaxUint64, ParentDevice: math.MaxUint64, ParentInode: math.MaxUint64,
			FileDevice: math.MaxUint64, FileInode: math.MaxUint64, ModifiedNS: math.MinInt64, Digest: record.PayloadDigest, Size: record.PayloadSize, Mode: record.Mode}
		if phase == workspaceFileSaveCommitted {
			entry := record.PlannedEntry
			record.Meta = &storage.ObjectMeta{Bucket: record.Bucket, Key: record.Path, ETag: entry.ETag, VersionID: entry.VersionID, Size: entry.Size,
				ContentType: entry.ContentType, LastModified: modified, Metadata: storage.CloneMetadata(entry.Metadata), ChecksumAlgorithm: entry.ChecksumAlgorithm, ChecksumValue: entry.ChecksumValue}
		}
	}
	return record
}
