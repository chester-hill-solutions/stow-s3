package resumetransfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrProgress = errors.New("invalid or incompatible transfer progress")
var ErrChanged = errors.New("transfer source, retained prefix or remote changed")
var ErrRemoteLost = errors.New("unfinished remote upload missing; explicit restart required")

const maxStateBytes = 8192
const maxTransferBytes = 1 << 30

type Target struct{ Bucket, Key string }
type Progress struct {
	Version      int    `json:"version"`
	Kind         string `json:"kind"`
	Target       Target `json:"target"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	PartSize     int64  `json:"part_size,omitempty"`
	UploadID     string `json:"upload_id,omitempty"`
	TransferID   string `json:"transfer_id,omitempty"`
	Phase        string `json:"phase"`
	ETag         string `json:"etag,omitempty"`
	Offset       int64  `json:"offset"`
	PrefixSHA256 string `json:"prefix_sha256,omitempty"`
	Integrity    string `json:"integrity"`
}

func digestState(p Progress) string {
	p.Integrity = ""
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(append([]byte("stow-client-transfer-v1\x00"), data...))
	return hex.EncodeToString(sum[:])
}
func loadState(path, kind string) (Progress, error) {
	var p Progress
	file, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return p, err
	}
	if len(data) > maxStateBytes || json.Unmarshal(data, &p) != nil || p.Version != 1 || p.Kind != kind || p.Integrity != digestState(p) {
		return p, ErrProgress
	}
	if p.Target.Bucket == "" || p.Target.Key == "" || p.Size < 0 || p.Size > maxTransferBytes || !validDigest(p.SHA256) || p.Offset < 0 || p.Offset > p.Size {
		return p, ErrProgress
	}
	return p, nil
}
func saveState(path string, p Progress) error {
	p.Integrity = digestState(p)
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(data) > maxStateBytes {
		return ErrProgress
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".resume-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
func hashReader(reader io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, reader)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func sourceIdentity(path string, limit int64) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > limit {
		return "", 0, fmt.Errorf("source exceeds regular-file limit")
	}
	return hashReader(io.LimitReader(f, limit+1))
}
func transferLimit(limit int64) int64 {
	if limit <= 0 || limit > maxTransferBytes {
		return maxTransferBytes
	}
	return limit
}
func requireNewState(path string) error {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("progress already exists; resume or explicitly discard it")
	}
	return err
}
