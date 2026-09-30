package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// AdmitRecoveryHold requires the registry mutation gate; prepared names a capture being published.
func (r *Registry) AdmitRecoveryHold(request RecoveryHoldRequest, prepared string) (RecoveryHold, error) {
	request, meaning, err := normalizeRecoveryHold(request)
	if err != nil {
		return RecoveryHold{}, err
	}
	journal, err := r.readRecoveryHolds()
	if err != nil {
		return RecoveryHold{}, err
	}
	for _, hold := range journal.Holds {
		if hold.ID != request.ID {
			continue
		}
		if hold.Meaning != meaning {
			return RecoveryHold{}, ErrRecoveryHoldConflict
		}
		return hold, nil
	}
	if len(journal.Holds) >= MaxRecoveryHolds {
		return RecoveryHold{}, ErrRecoveryHoldFull
	}
	for _, reference := range request.References {
		if err := r.validateRecoveryReference(reference, prepared); err != nil {
			return RecoveryHold{}, err
		}
	}
	hold := RecoveryHold{RecoveryHoldRequest: request, Meaning: meaning, State: "pending", Created: time.Now().UTC()}
	journal.Holds = append(journal.Holds, hold)
	sort.Slice(journal.Holds, func(i, j int) bool { return journal.Holds[i].ID < journal.Holds[j].ID })
	if err := r.writeRecoveryHolds(journal); err != nil {
		return RecoveryHold{}, err
	}
	return hold, nil
}

func (r *Registry) validateRecoveryReference(reference RecoveryReference, prepared string) error {
	_, found, err := r.Lookup(reference.WorkspaceID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("workspace: recovery dependency workspace not registered")
	}
	if reference.CheckpointID == "" || reference.CheckpointID == prepared {
		return nil
	}
	path := filepath.Join(r.dir, "checkpoints", reference.CheckpointID, "manifest.json")
	stat, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 4<<20 {
		return fmt.Errorf("workspace: invalid recovery checkpoint reference")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var identity checkpointReference
	if json.Unmarshal(data, &identity) != nil || identity.ID != reference.CheckpointID || identity.WorkspaceID != reference.WorkspaceID {
		return fmt.Errorf("workspace: recovery checkpoint identity mismatch")
	}
	return nil
}

// FinishRecoveryHold requires the mutation gate and retains bounded terminal identity.
func (r *Registry) FinishRecoveryHold(id, owner, state string) (RecoveryHold, error) {
	if !ValidWorkspaceID(id) || !ValidWorkspaceID(owner) || state != "released" && state != "discarded" {
		return RecoveryHold{}, ErrRecoveryHoldConflict
	}
	journal, err := r.readRecoveryHolds()
	if err != nil {
		return RecoveryHold{}, err
	}
	for index, hold := range journal.Holds {
		if hold.ID != id {
			continue
		}
		if hold.Owner != owner {
			return RecoveryHold{}, ErrRecoveryHoldConflict
		}
		if hold.State == state {
			return hold, nil
		}
		if hold.State != "pending" {
			return RecoveryHold{}, ErrRecoveryHoldConflict
		}
		hold.State = state
		hold.FinishedAt = time.Now().UTC()
		if hold.FinishedAt.Before(hold.Created) {
			hold.FinishedAt = hold.Created
		}
		journal.Holds[index] = hold
		if err := r.writeRecoveryHolds(journal); err != nil {
			return RecoveryHold{}, err
		}
		return hold, nil
	}
	return RecoveryHold{}, ErrRecoveryHoldNotFound
}

func (r *Registry) ListRecoveryHolds(query RecoveryHoldQuery) (RecoveryHoldPage, error) {
	if query.Limit == 0 {
		query.Limit = 16
	}
	if query.Limit < 1 || query.Limit > 16 || query.WorkspaceID != "" && !ValidWorkspaceID(query.WorkspaceID) || query.Cursor != "" && !ValidWorkspaceID(query.Cursor) {
		return RecoveryHoldPage{}, fmt.Errorf("workspace: invalid recovery hold page")
	}
	journal, err := r.readRecoveryHolds()
	if err != nil {
		return RecoveryHoldPage{}, err
	}
	reserved := recoveryJournalReserve(journal)
	page := RecoveryHoldPage{Holds: []RecoveryHold{}, ReservedBytes: reserved, PeakReservedBytes: reserved * 2}
	for _, hold := range journal.Holds {
		if hold.ID <= query.Cursor || !query.IncludeFinished && hold.State != "pending" || !holdMatchesWorkspace(hold, query.WorkspaceID) {
			continue
		}
		if len(page.Holds) == query.Limit {
			page.NextCursor = page.Holds[len(page.Holds)-1].ID
			break
		}
		page.Holds = append(page.Holds, hold)
	}
	return page, nil
}

func holdMatchesWorkspace(hold RecoveryHold, id string) bool {
	if id == "" {
		return true
	}
	for _, reference := range hold.References {
		if reference.WorkspaceID == id {
			return true
		}
	}
	return false
}

func (r *Registry) LookupRecoveryHold(id string) (RecoveryHold, bool, error) {
	if !ValidWorkspaceID(id) {
		return RecoveryHold{}, false, ErrRecoveryHoldConflict
	}
	journal, err := r.readRecoveryHolds()
	if err != nil {
		return RecoveryHold{}, false, err
	}
	for _, hold := range journal.Holds {
		if hold.ID == id {
			return hold, true, nil
		}
	}
	return RecoveryHold{}, false, nil
}
