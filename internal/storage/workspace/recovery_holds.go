package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const MaxRecoveryHolds = 256
const MaxRecoveryReferences = 32
const MaxRecoveryHoldBytes = 2 << 20
const MaxRecoveryHoldPeakBytes = 2 * MaxRecoveryHoldBytes

var (
	ErrRecoveryHeld         = errors.New("workspace: retained recovery dependency")
	ErrRecoveryHoldConflict = errors.New("workspace: recovery hold identity conflict")
	ErrRecoveryHoldNotFound = errors.New("workspace: recovery hold not found")
	ErrRecoveryHoldFull     = errors.New("workspace: recovery hold capacity exhausted")
	ErrRecoveryHoldCorrupt  = errors.New("workspace: recovery hold state unreadable")
)

type RecoveryReference struct {
	WorkspaceID  string `json:"workspace_id"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

type RecoveryHoldRequest struct {
	ID         string              `json:"id"`
	Owner      string              `json:"owner"`
	References []RecoveryReference `json:"references"`
	ExpiresAt  time.Time           `json:"expires_at,omitempty"`
}

type RecoveryHold struct {
	RecoveryHoldRequest
	Meaning    string    `json:"meaning"`
	State      string    `json:"state"`
	Created    time.Time `json:"created"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type RecoveryHoldQuery struct {
	Cursor          string
	Limit           int
	WorkspaceID     string
	IncludeFinished bool
}

type RecoveryHoldPage struct {
	Holds             []RecoveryHold `json:"holds"`
	NextCursor        string         `json:"next_cursor,omitempty"`
	ReservedBytes     int64          `json:"reserved_bytes"`
	PeakReservedBytes int64          `json:"peak_reserved_bytes"`
}

func normalizeRecoveryHold(request RecoveryHoldRequest) (RecoveryHoldRequest, string, error) {
	if !ValidWorkspaceID(request.ID) || !ValidWorkspaceID(request.Owner) || len(request.References) == 0 || len(request.References) > MaxRecoveryReferences {
		return request, "", fmt.Errorf("workspace: invalid recovery hold identity or reference bounds")
	}
	request.References = append([]RecoveryReference(nil), request.References...)
	request.ExpiresAt = request.ExpiresAt.UTC()
	sort.Slice(request.References, func(i, j int) bool {
		return recoveryReferenceKey(request.References[i]) < recoveryReferenceKey(request.References[j])
	})
	for index, reference := range request.References {
		if !ValidWorkspaceID(reference.WorkspaceID) || reference.CheckpointID != "" && !validRecoveryCheckpoint(reference.CheckpointID) {
			return request, "", fmt.Errorf("workspace: invalid recovery dependency")
		}
		if index > 0 && reference == request.References[index-1] {
			return request, "", fmt.Errorf("workspace: duplicate recovery dependency")
		}
	}
	data, err := json.Marshal(request)
	if err != nil {
		return request, "", err
	}
	digest := sha256.Sum256(data)
	return request, hex.EncodeToString(digest[:]), nil
}

func recoveryReferenceKey(reference RecoveryReference) string {
	return reference.WorkspaceID + "\x00" + reference.CheckpointID
}

func validRecoveryCheckpoint(id string) bool {
	if len(id) != 27 || id[:3] != "cp_" {
		return false
	}
	decoded, err := hex.DecodeString(id[3:])
	return err == nil && hex.EncodeToString(decoded) == id[3:]
}

func recoveryHoldReserve(hold RecoveryHold) int64 {
	hold.State = "discarded"
	hold.FinishedAt = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	data, _ := json.Marshal(hold)
	return int64(len(data)) + 1
}

func (r *Registry) CheckWorkspaceRecoveryHolds(id string) error {
	return r.checkRecoveryHolds(func(reference RecoveryReference) bool { return reference.WorkspaceID == id })
}

func (r *Registry) CheckCheckpointRecoveryHolds(id string) error {
	return r.checkRecoveryHolds(func(reference RecoveryReference) bool { return reference.CheckpointID == id })
}

func (r *Registry) checkRecoveryHolds(matches func(RecoveryReference) bool) error {
	journal, err := r.readRecoveryHolds()
	if err != nil {
		return err
	}
	for _, hold := range journal.Holds {
		if hold.State != "pending" {
			continue
		}
		for _, reference := range hold.References {
			if matches(reference) {
				return fmt.Errorf("%w: %s", ErrRecoveryHeld, hold.ID)
			}
		}
	}
	return nil
}
