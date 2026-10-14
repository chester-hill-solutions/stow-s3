package fs

import "context"

func (s *FilesystemStore) UsageRevision(ctx context.Context) (uint64, error) {
	if ctx != nil && ctx.Err() != nil {
		return 0, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.saves.pending != nil
	if err := s.settleSaveLocked(); err != nil {
		return s.usageRevision, err
	}
	if pending {
		s.usageRevision++
	}
	return s.usageRevision, nil
}
